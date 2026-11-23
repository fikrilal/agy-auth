package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

	updaterOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(updaterOnly, UpdaterIDFile), []byte("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if HasProfile(updaterOnly) {
		t.Fatal("updater id alone should not count as a profile")
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
	const updater = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := os.WriteFile(filepath.Join(configDir, UpdaterIDFile), []byte(updater), 0o600); err != nil {
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
	snapUpdater, err := os.ReadFile(filepath.Join(accountSnap, UpdaterIDFile))
	if err != nil || string(snapUpdater) != updater {
		t.Fatalf("unexpected snapshot updater id: %v, %s", err, string(snapUpdater))
	}

	// Modify config dir to simulate a different session
	if err := os.WriteFile(filepath.Join(configDir, "Cookies"), []byte("new-cookies-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	const replaced = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if err := os.WriteFile(filepath.Join(configDir, UpdaterIDFile), []byte(replaced), 0o600); err != nil {
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
	restoredUpdater, err := os.ReadFile(filepath.Join(configDir, UpdaterIDFile))
	if err != nil || string(restoredUpdater) != updater {
		t.Fatalf("unexpected restored updater id: %v, %s", err, string(restoredUpdater))
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
	backedUpdater, err := os.ReadFile(filepath.Join(desktopBackups, entries[0].Name(), UpdaterIDFile))
	if err != nil || string(backedUpdater) != replaced {
		t.Fatalf("expected backup updater id %s, got %v, %s", replaced, err, string(backedUpdater))
	}
}

func TestRestoreKeepsUpdaterIDWhenSnapshotOmitsIt(t *testing.T) {
	configDir := t.TempDir()
	snapDir := t.TempDir()
	const live = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := os.WriteFile(filepath.Join(configDir, "Cookies"), []byte("live-cookies"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, UpdaterIDFile), []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "Cookies"), []byte("snap-cookies"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(configDir, "", "")
	if err := mgr.Restore(snapDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(configDir, UpdaterIDFile))
	if err != nil || string(got) != live {
		t.Fatalf("live updater id = %q, err = %v", got, err)
	}
	restoredCookies, err := os.ReadFile(filepath.Join(configDir, "Cookies"))
	if err != nil || string(restoredCookies) != "snap-cookies" {
		t.Fatalf("cookies = %q, err = %v", restoredCookies, err)
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

func TestParseDarwinProcesses(t *testing.T) {
	psOutput := `
  101 /Users/gray/.local/bin/agy
  202 /bin/zsh -c grep /Applications/Antigravity.app/Contents/MacOS/Antigravity
  303 /usr/bin/vim /Applications/Antigravity.app/Contents/MacOS/Antigravity
15025 /Applications/Antigravity.app/Contents/Resources/bin/language_server --standalone --override_ide_name antigravity
14375 /Applications/Antigravity.app/Contents/Frameworks/Antigravity Helper.app/Contents/MacOS/Antigravity Helper --type=gpu-process
14373 /Applications/Antigravity.app/Contents/MacOS/Antigravity
36099 /Applications/Antigravity.app/Contents/Frameworks/Antigravity Helper (Renderer).app/Contents/MacOS/Antigravity Helper (Renderer) --type=renderer
40000 /Users/John Doe/Applications/Antigravity.app/Contents/MacOS/Antigravity --flag
`
	got := parseDarwinProcesses(psOutput, "/Applications/Antigravity.app/Contents/MacOS/Antigravity", 36099)
	want := []int{14373, 40000, 15025, 14375}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDarwinProcesses = %v, want %v", got, want)
	}
}

func TestParseWindowsTasklist(t *testing.T) {
	output := "\"Antigravity.exe\",\"4321\",\"Console\",\"1\",\"120,000 K\"\r\n\"Antigravity.exe\",\"8765\",\"Console\",\"1\",\"64,000 K\"\r\n"
	got := parseWindowsTasklist(output, "Antigravity.exe", 8765)
	want := []int{4321}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWindowsTasklist = %v, want %v", got, want)
	}

	none := "INFO: No tasks are running which match the specified criteria.\r\n"
	if got := parseWindowsTasklist(none, "Antigravity.exe", 1); len(got) != 0 {
		t.Fatalf("expected empty pids, got %v", got)
	}
}

func TestParseUnixProcesses(t *testing.T) {
	output := `
 100 /usr/local/bin/agy
 200 /usr/local/bin/agy-auth switch work --relaunch
 300 /opt/antigravity/resources/bin/language_server --standalone
 400 /opt/antigravity/antigravity --type=renderer
 500 /opt/antigravity/antigravity
`
	got := parseUnixProcesses(output, "/opt/antigravity/antigravity", 200)
	want := []int{500, 300, 400}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUnixProcesses = %v, want %v", got, want)
	}
}

func TestMacOSAppBundle(t *testing.T) {
	if got := macOSAppBundle("/Applications/Antigravity.app/Contents/MacOS/Antigravity"); got != "/Applications/Antigravity.app" {
		t.Fatalf("macOSAppBundle = %q", got)
	}
	if got := macOSAppBundle("/usr/local/bin/antigravity"); got != "" {
		t.Fatalf("expected empty bundle for plain binary, got %q", got)
	}
}

func TestScopeFileAndSyncAppStorage(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "Antigravity")
	lowerScopeDir := filepath.Join(root, "antigravity")
	scopeFile := filepath.Join(lowerScopeDir, ScopeFileName)
	snapDir := filepath.Join(root, "snap")

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lowerScopeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "app_storage.json"), []byte(`{"agy.onboarding.lastLoginUsername":"old@example.com","agy.onboarding.lastLoginIsGcpTos":"false","jetski.fontSize":14}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scopeFile, []byte("scope-user-1"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(configDir, "", "")
	mgr.ScopeFile = scopeFile

	if got := LastLoginEmail(configDir); got != "old@example.com" {
		t.Fatalf("LastLoginEmail = %q", got)
	}

	if err := mgr.Snapshot(snapDir); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	snapScope, err := os.ReadFile(filepath.Join(snapDir, ScopeFileName))
	if err != nil || string(snapScope) != "scope-user-1" {
		t.Fatalf("snapshot scope = %q, err = %v", string(snapScope), err)
	}

	if err := mgr.ClearUserScope(); err != nil {
		t.Fatalf("ClearUserScope: %v", err)
	}
	if _, err := os.Stat(scopeFile); !os.IsNotExist(err) {
		t.Fatalf("expected scope file removed, err = %v", err)
	}

	if err := mgr.SyncAppStorage("new@example.com", true); err != nil {
		t.Fatalf("SyncAppStorage: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(configDir, "app_storage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["agy.onboarding.lastLoginUsername"] != "new@example.com" {
		t.Errorf("lastLoginUsername = %v", doc["agy.onboarding.lastLoginUsername"])
	}
	if doc["agy.onboarding.lastLoginIsGcpTos"] != "true" {
		t.Errorf("lastLoginIsGcpTos = %#v, want string \"true\"", doc["agy.onboarding.lastLoginIsGcpTos"])
	}
	if doc["jetski.fontSize"] != float64(14) {
		t.Errorf("unrelated setting clobbered: %#v", doc["jetski.fontSize"])
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(configDir, "app_storage.json"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("app_storage.json mode = %v, err = %v", info.Mode().Perm(), err)
		}
	}

	// Restoring snapshot restores credential-user-scope to both ConfigDir and ScopeFile.
	if err := mgr.Restore(snapDir); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restoredScope, err := os.ReadFile(scopeFile)
	if err != nil || string(restoredScope) != "scope-user-1" {
		t.Fatalf("restored scope = %q, err = %v", string(restoredScope), err)
	}
}
