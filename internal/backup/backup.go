package backup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const Retention = 5

type Kind string

const (
	Active   Kind = "active"
	Registry Kind = "registry"
)

var managedPattern = regexp.MustCompile(`^(active|registry)-\d{8}T\d{6}\.\d{9}Z(-\d+)?\.json$`)

// Store manages timestamped credential and registry snapshots. Only names that
// match the managed pattern are ever pruned or removed.
type Store struct {
	Dir string
	Now func() time.Time
}

func New(dir string) *Store {
	return &Store{Dir: dir}
}

func (s *Store) Snapshot(kind Kind, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := s.ensureDir(); err != nil {
		return err
	}
	existing, err := s.Managed(kind)
	if err != nil {
		return err
	}
	for _, path := range existing {
		data, err := os.ReadFile(path)
		if err == nil && bytes.Equal(data, raw) {
			return nil
		}
	}

	stamp := s.now().UTC().Format("20060102T150405.000000000Z")
	base := fmt.Sprintf("%s-%s", kind, stamp)
	path := filepath.Join(s.Dir, base+".json")
	for i := 1; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
		path = filepath.Join(s.Dir, fmt.Sprintf("%s-%d.json", base, i))
	}
	if err := writeFileAtomic(path, raw, 0o600); err != nil {
		return err
	}
	_, err = s.Prune(kind, Retention, false)
	return err
}

// Managed returns managed backup paths for a kind, newest first.
func (s *Store) Managed(kind Kind) ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := string(kind) + "-"
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !managedPattern.MatchString(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(s.Dir, entry.Name()))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	return paths, nil
}

// Prune removes managed backups beyond keep, newest first. It never touches
// unknown files and returns the basenames it removed (or would remove).
func (s *Store) Prune(kind Kind, keep int, dryRun bool) ([]string, error) {
	if keep < 1 {
		keep = Retention
	}
	paths, err := s.Managed(kind)
	if err != nil {
		return nil, err
	}
	if len(paths) <= keep {
		return nil, nil
	}
	removed := make([]string, 0, len(paths)-keep)
	var errs []error
	for _, path := range paths[keep:] {
		removed = append(removed, filepath.Base(path))
		if dryRun {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}

func (s *Store) PruneAll(keep int, dryRun bool) ([]string, error) {
	var removed []string
	var errs []error
	for _, kind := range []Kind{Active, Registry} {
		names, err := s.Prune(kind, keep, dryRun)
		removed = append(removed, names...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	sort.Strings(removed)
	return removed, errors.Join(errs...)
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.Dir, 0o700)
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return os.Chmod(path, mode)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".backup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
