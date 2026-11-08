package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopUpdaterIDStoredPerAccount(t *testing.T) {
	store := NewStore(t.TempDir())
	expiry := time.Now().Add(time.Hour)
	first, err := store.Upsert(testToken(t, "first@example.com", "subject-1", "refresh-1", expiry), "first", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Upsert(testToken(t, "second@example.com", "subject-2", "refresh-2", expiry), "second", false)
	if err != nil {
		t.Fatal(err)
	}
	const firstID = "11111111-1111-4111-8111-111111111111"
	const secondID = "22222222-2222-4222-8222-222222222222"
	if err := store.SaveDesktopUpdaterID(first.Account.ID, firstID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDesktopUpdaterID(first.Account.ID, firstID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDesktopUpdaterID(second.Account.ID, secondID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDesktopUpdaterID(second.Account.ID, firstID); err == nil {
		t.Fatal("expected duplicate desktop updater id to be rejected")
	}
	got, err := store.LoadDesktopUpdaterID(first.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != firstID {
		t.Fatalf("first id = %s", got)
	}
	owner, err := store.DesktopUpdaterOwner(secondID, "")
	if err != nil || owner != second.Account.ID {
		t.Fatalf("owner = %s, err = %v", owner, err)
	}
	owner, err = store.DesktopUpdaterOwner(secondID, second.Account.ID)
	if err != nil || owner != "" {
		t.Fatalf("owner excluding self = %s, err = %v", owner, err)
	}
	info, err := os.Stat(filepath.Join(store.DesktopDir, first.Account.ID, ".updaterId"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
	if err := store.DeleteToken(first.Account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadDesktopUpdaterID(first.Account.ID); !os.IsNotExist(err) {
		t.Fatalf("deleted updater id err = %v", err)
	}
}

func TestDesktopUpdaterOwnerReadError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	store.DesktopDir = blocker
	if _, err := store.DesktopUpdaterOwner("11111111-1111-4111-8111-111111111111", ""); err == nil {
		t.Fatal("expected a read error")
	}
}
