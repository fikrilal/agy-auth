package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfigDir(t *testing.T) {
	t.Setenv("AGY_DESKTOP_HOME", "/custom/desktop/path")
	if got := DefaultConfigDir(); got != "/custom/desktop/path" {
		t.Fatalf("expected /custom/desktop/path, got %s", got)
	}

	t.Setenv("AGY_DESKTOP_HOME", "")
	t.Setenv("AGY_DESKTOP_CONFIG_DIR", "/custom/alt/path")
	if got := DefaultConfigDir(); got != "/custom/alt/path" {
		t.Fatalf("expected /custom/alt/path, got %s", got)
	}
}

func TestDefaultBinaryPath(t *testing.T) {
	t.Setenv("AGY_DESKTOP_BIN", "/custom/bin/antigravity")
	if got := DefaultBinaryPath(); got != "/custom/bin/antigravity" {
		t.Fatalf("expected /custom/bin/antigravity, got %s", got)
	}
}

func TestHasProfile(t *testing.T) {
	tmp := t.TempDir()
	if HasProfile(tmp) {
		t.Fatal("empty directory should not have a profile")
	}

	cookiePath := filepath.Join(tmp, "Cookies")
	if err := os.WriteFile(cookiePath, []byte("cookies-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !HasProfile(tmp) {
		t.Fatal("directory with Cookies should have a profile")
	}
}

func TestSnapshotRestoreAndBackup(t *testing.T) {
	configDir := t.TempDir()
	snapDir := t.TempDir()
	backupDir := t.TempDir()

	// Populate config dir with mock session files
	if err := os.WriteFile(filepath.Join(configDir, "Cookies"), []byte("sqlite-cookies-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "app_storage.json"), []byte(`{"lastLoginUsername":"user@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lsDir := filepath.Join(configDir, "Local Storage", "leveldb")
	if err := os.MkdirAll(lsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lsDir, "000001.log"), []byte("leveldb-log"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(configDir, "", backupDir)

	// Snapshot
	accountSnap := filepath.Join(snapDir, "acct_test")
	if err := mgr.Snapshot(accountSnap); err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Verify snapshot contents
	if !HasProfile(accountSnap) {
		t.Fatal("snapshot should be a valid profile")
	}
	snapCookies, err := os.ReadFile(filepath.Join(accountSnap, "Cookies"))
	if err != nil || string(snapCookies) != "sqlite-cookies-content" {
		t.Fatalf("unexpected snapshot cookies: %v, %s", err, string(snapCookies))
	}

	// Modify config dir to simulate a different session
	if err := os.WriteFile(filepath.Join(configDir, "Cookies"), []byte("new-cookies-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Restore snapshot
	if err := mgr.Restore(accountSnap); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored cookies
	restoredCookies, err := os.ReadFile(filepath.Join(configDir, "Cookies"))
	if err != nil || string(restoredCookies) != "sqlite-cookies-content" {
		t.Fatalf("unexpected restored cookies: %v, %s", err, string(restoredCookies))
	}

	// Verify backup was created
	desktopBackups := filepath.Join(backupDir, "desktop")
	entries, err := os.ReadDir(desktopBackups)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 backup, got %v (err: %v)", len(entries), err)
	}
	backedUpCookies, err := os.ReadFile(filepath.Join(desktopBackups, entries[0].Name(), "Cookies"))
	if err != nil || string(backedUpCookies) != "new-cookies-content" {
		t.Fatalf("expected backup to have previous live cookies, got %v, %s", err, string(backedUpCookies))
	}
}

func TestProcessOps(t *testing.T) {
	findCalled := false
	killCalled := false
	startCalled := false

	ops := &ProcessOps{
		FindProcesses: func() ([]int, error) {
			findCalled = true
			return []int{12345}, nil
		},
		KillProcess: func(pid int) error {
			if pid == 12345 {
				killCalled = true
			}
			return nil
		},
		StartProcess: func(bin string) error {
			if bin == "/test/bin" {
				startCalled = true
			}
			return nil
		},
	}

	mgr := NewManager(t.TempDir(), "/test/bin", "")
	mgr.Ops = ops

	running, pid, err := mgr.IsRunning()
	if err != nil || !running || pid != 12345 || !findCalled {
		t.Fatalf("expected running pid 12345, got %v, %d, %v", running, pid, err)
	}

	if err := mgr.Terminate(pid, 100*time.Millisecond); err != nil || !killCalled {
		t.Fatalf("Terminate failed or did not call kill: %v", err)
	}

	if err := mgr.Relaunch(); err != nil || !startCalled {
		t.Fatalf("Relaunch failed or did not call start: %v", err)
	}
}
