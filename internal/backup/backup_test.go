package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store := New(filepath.Join(t.TempDir(), "backups"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	return store
}

func TestSnapshotRetentionAndPermissions(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 6; i++ {
		if err := store.Snapshot(Active, []byte(fmt.Sprintf("payload-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	managed, err := store.Managed(Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != Retention {
		t.Fatalf("managed = %d, want %d", len(managed), Retention)
	}
	newest, err := os.ReadFile(managed[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(newest) != "payload-5" {
		t.Errorf("newest backup = %q", newest)
	}
	info, err := os.Stat(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("backup dir permissions = %o", info.Mode().Perm())
	}
	for _, path := range managed {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions = %o", filepath.Base(path), info.Mode().Perm())
		}
	}
}

func TestSnapshotDedupesIdentical(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 2; i++ {
		if err := store.Snapshot(Active, []byte("same")); err != nil {
			t.Fatal(err)
		}
	}
	managed, err := store.Managed(Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 1 {
		t.Errorf("managed = %d, want 1", len(managed))
	}
}

func addOldBackup(t *testing.T, store *Store, kind Kind, payload string) {
	t.Helper()
	path := filepath.Join(store.Dir, fmt.Sprintf("%s-20250101T000000.000000000Z.json", kind))
	if err := writeFileAtomic(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPruneDryRun(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < Retention; i++ {
		if err := store.Snapshot(Active, []byte(fmt.Sprintf("payload-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	addOldBackup(t, store, Active, "old")
	removed, err := store.Prune(Active, Retention, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Errorf("removed = %d, want 1", len(removed))
	}
	managed, err := store.Managed(Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != Retention+1 {
		t.Errorf("dry run removed files: managed = %d", len(managed))
	}
}

func TestPruneAllKeepsKindsSeparate(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < Retention; i++ {
		if err := store.Snapshot(Active, []byte(fmt.Sprintf("active-%d", i))); err != nil {
			t.Fatal(err)
		}
		if err := store.Snapshot(Registry, []byte(fmt.Sprintf("registry-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	addOldBackup(t, store, Active, "old-active")
	addOldBackup(t, store, Registry, "old-registry")
	removed, err := store.PruneAll(Retention, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Errorf("removed = %d, want 2", len(removed))
	}
	for _, kind := range []Kind{Active, Registry} {
		managed, err := store.Managed(kind)
		if err != nil {
			t.Fatal(err)
		}
		if len(managed) != Retention {
			t.Errorf("%s managed = %d, want %d", kind, len(managed), Retention)
		}
	}
}

func TestPruneKeepsUnknownFiles(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 6; i++ {
		if err := store.Snapshot(Active, []byte(fmt.Sprintf("payload-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(store.Dir, "notes.txt")
	if err := os.WriteFile(unknown, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prune(Active, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Errorf("unknown file was removed: %v", err)
	}
}

func TestSnapshotRehardensDirectory(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Snapshot(Active, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("backup dir permissions = %o", info.Mode().Perm())
	}
}
