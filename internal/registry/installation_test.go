package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallationIDStoredPerAccount(t *testing.T) {
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
	if err := store.SaveInstallationID(first.Account.ID, firstID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInstallationID(second.Account.ID, secondID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInstallationID(second.Account.ID, firstID); err == nil {
		t.Fatal("expected duplicate installation id to be rejected")
	}
	got, err := store.LoadInstallationID(first.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != firstID {
		t.Fatalf("first id = %s", got)
	}
	owner, err := store.InstallationOwner(secondID)
	if err != nil || owner != second.Account.ID {
		t.Fatalf("owner = %s, err = %v", owner, err)
	}
	info, err := os.Stat(filepath.Join(store.AccountsDir, first.Account.ID+".installation_id"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}

	if err := store.DeleteToken(first.Account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadInstallationID(first.Account.ID); !os.IsNotExist(err) {
		t.Fatalf("deleted id err = %v", err)
	}

	orphan := filepath.Join(store.AccountsDir, "acct_orphan.installation_id")
	if err := os.WriteFile(orphan, []byte(firstID), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(store.AccountsDir, second.Account.ID+".installation_id")
	report, err := store.Clean(false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range report.Snapshots {
		if name == "acct_orphan.installation_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clean snapshots = %v", report.Snapshots)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan sidecar err = %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("kept sidecar: %v", err)
	}
}

func TestInstallationOwnerReadError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	store.AccountsDir = blocker
	if _, err := store.InstallationOwner("11111111-1111-4111-8111-111111111111"); err == nil {
		t.Fatal("expected a read error")
	}
}
