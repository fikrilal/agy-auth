package agy

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/fikrilal/agy-auth/internal/backup"
	keyring "github.com/zalando/go-keyring"
)

const (
	KeyringService = "gemini"
	KeyringUser    = "antigravity"

	keyringTimeout = 5 * time.Second
)

var (
	ErrNoCredentials = errors.New("no Antigravity credentials found")
	ErrBackup        = errors.New("credential backup failed")
)

// Storage reads and writes the credential document agy itself uses.
//
// agy prefers the OS keyring and falls back to a plain file when the keyring is
// unavailable. agy-auth mirrors writes to both locations so switching works in
// desktop and headless environments.
type Storage struct {
	Service   string
	User      string
	FilePath  string
	NoKeyring bool
	Backup    *backup.Store
}

func DefaultStorage() *Storage {
	return &Storage{
		Service:   KeyringService,
		User:      KeyringUser,
		FilePath:  defaultTokenFilePath(),
		NoKeyring: os.Getenv("AGY_AUTH_STORAGE") == "file",
	}
}

func defaultTokenFilePath() string {
	if override := os.Getenv("AGY_AUTH_AGY_TOKEN_FILE"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "antigravity-cli", "antigravity-oauth-token")
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
}

type Source string

const (
	SourceKeyring Source = "keyring"
	SourceFile    Source = "file"
)

type ReadResult struct {
	Raw    []byte
	Source Source
}

func (s *Storage) Read() (ReadResult, error) {
	keyringRaw, keyringErr := s.readKeyring()
	fileRaw, fileErr := s.readFile()

	if keyringErr == nil {
		if _, err := ParseTokenFile(keyringRaw); err == nil {
			return ReadResult{Raw: keyringRaw, Source: SourceKeyring}, nil
		} else {
			keyringErr = fmt.Errorf("keyring credential is invalid: %w", err)
		}
	}
	if fileErr == nil {
		if _, err := ParseTokenFile(fileRaw); err == nil {
			return ReadResult{Raw: fileRaw, Source: SourceFile}, nil
		} else {
			fileErr = fmt.Errorf("file credential is invalid: %w", err)
		}
	}
	if errors.Is(keyringErr, ErrNoCredentials) && errors.Is(fileErr, ErrNoCredentials) {
		return ReadResult{}, ErrNoCredentials
	}
	if keyringErr == nil || fileErr == nil {
		return ReadResult{}, fmt.Errorf("%w: credential stores are unreadable", ErrNoCredentials)
	}
	return ReadResult{}, fmt.Errorf("read credentials: keyring: %v; file: %v", keyringErr, fileErr)
}

// ReadBoth returns both stores without choosing one. It is used by status to
// detect drift between the keyring and the file mirror.
func (s *Storage) ReadBoth() (keyringRaw, fileRaw []byte, keyringErr, fileErr error) {
	keyringRaw, keyringErr = s.readKeyring()
	fileRaw, fileErr = s.readFile()
	return
}

type WriteReport struct {
	Written []Source
	Errors  []error
}

func (r WriteReport) OK() bool { return len(r.Written) > 0 }

func (r WriteReport) Error() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return errors.Join(r.Errors...)
}

func (s *Storage) Write(raw []byte) WriteReport {
	if _, err := ParseTokenFile(raw); err != nil {
		return WriteReport{Errors: []error{err}}
	}
	if err := s.snapshotCurrentExcept(raw); err != nil {
		return WriteReport{Errors: []error{fmt.Errorf("%w: %v", ErrBackup, err)}}
	}
	var report WriteReport
	if s.KeyringAvailable() {
		if err := s.writeKeyring(raw); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("keyring: %w", err))
		} else {
			report.Written = append(report.Written, SourceKeyring)
		}
	}
	if err := writeFileAtomic(s.FilePath, raw, 0o600); err != nil {
		report.Errors = append(report.Errors, fmt.Errorf("file: %w", err))
	} else {
		report.Written = append(report.Written, SourceFile)
	}
	return report
}

func (s *Storage) SnapshotCurrent() error {
	return s.snapshotCurrentExcept(nil)
}

func (s *Storage) snapshotCurrentExcept(newRaw []byte) error {
	if s.Backup == nil {
		return nil
	}
	keyringRaw, fileRaw, _, _ := s.ReadBoth()
	for _, current := range [][]byte{keyringRaw, fileRaw} {
		if len(current) == 0 || (newRaw != nil && bytes.Equal(current, newRaw)) {
			continue
		}
		if _, err := ParseTokenFile(current); err != nil {
			continue
		}
		if err := s.Backup.Snapshot(backup.Active, current); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) Delete() error {
	if err := s.snapshotCurrentExcept(nil); err != nil {
		return fmt.Errorf("%w: %v", ErrBackup, err)
	}
	var errs []error
	if err := s.deleteKeyring(); err != nil && !errors.Is(err, ErrNoCredentials) {
		errs = append(errs, fmt.Errorf("keyring: %w", err))
	}
	if err := os.Remove(s.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("file: %w", err))
	}
	return errors.Join(errs...)
}

func (s *Storage) KeyringAvailable() bool {
	if s.NoKeyring {
		return false
	}
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		return false
	}
	return true
}

func (s *Storage) readKeyring() ([]byte, error) {
	if !s.KeyringAvailable() {
		return nil, ErrNoCredentials
	}
	value, err := keyringGet(s.Service, s.User)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (s *Storage) writeKeyring(raw []byte) error {
	if !s.KeyringAvailable() {
		return errors.New("OS keyring unavailable")
	}
	return keyringSet(s.Service, s.User, string(raw))
}

func (s *Storage) deleteKeyring() error {
	if !s.KeyringAvailable() {
		return ErrNoCredentials
	}
	err := keyringDelete(s.Service, s.User)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNoCredentials
	}
	return err
}

func (s *Storage) readFile() ([]byte, error) {
	raw, err := os.ReadFile(s.FilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoCredentials
	}
	return raw, err
}

type keyringResult struct {
	value string
	err   error
}

func keyringGet(service, user string) (string, error) {
	ch := make(chan keyringResult, 1)
	go func() {
		value, err := keyring.Get(service, user)
		ch <- keyringResult{value: value, err: err}
	}()
	select {
	case result := <-ch:
		return result.value, result.err
	case <-time.After(keyringTimeout):
		return "", errors.New("OS keyring timed out")
	}
}

func keyringSet(service, user, secret string) error {
	ch := make(chan error, 1)
	go func() { ch <- keyring.Set(service, user, secret) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(keyringTimeout):
		return errors.New("OS keyring timed out")
	}
}

func keyringDelete(service, user string) error {
	ch := make(chan error, 1)
	go func() { ch <- keyring.Delete(service, user) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(keyringTimeout):
		return errors.New("OS keyring timed out")
	}
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
	tmp, err := os.CreateTemp(dir, ".antigravity-oauth-token-*")
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
