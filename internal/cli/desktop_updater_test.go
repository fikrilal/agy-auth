package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/desktop"
	"github.com/fikrilal/agy-auth/internal/oauth"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func TestAddClaimsDesktopUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	const original = "11111111-1111-4111-8111-111111111111"
	writeDesktopCookies(t, app.Desktop.ConfigDir)
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live updater id = %s", got)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadDesktopUpdaterID(reg.Accounts[0].ID)
	if err != nil || stored != original {
		t.Fatalf("stored updater id = %s, err = %v", stored, err)
	}
	info, err := os.Stat(filepath.Join(store.DesktopProfilePath(reg.Accounts[0].ID), desktop.UpdaterIDFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
}

func TestAddCLITargetDoesNotChangeDesktopUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	const original = "11111111-1111-4111-8111-111111111111"
	writeDesktopCookies(t, app.Desktop.ConfigDir)
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"add", "--target=cli", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live updater id = %s", got)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.DesktopProfilePath(reg.Accounts[0].ID), desktop.UpdaterIDFile)); !os.IsNotExist(err) {
		t.Fatalf("cli add wrote a desktop updater id: %v", err)
	}
}

func TestSwitchMintsAndRestoresDesktopUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	first := reg.Active
	var second string
	for _, account := range reg.Accounts {
		if account.ID != first {
			second = account.ID
		}
	}
	if first == "" || second == "" {
		t.Fatal("expected two accounts")
	}

	app, _, _ = env.app("")
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	installPath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	installBefore := ""
	if raw, err := os.ReadFile(installPath); err == nil {
		installBefore = string(raw)
	}
	writeDesktopCookies(t, store.DesktopProfilePath(first))
	writeDesktopCookies(t, store.DesktopProfilePath(second))

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second", "--target=desktop"}); code != 0 {
		t.Fatalf("switch second exit = %d, stderr = %s", code, errOut)
	}
	if got, err := store.LoadDesktopUpdaterID(first); err != nil || got != original {
		t.Fatalf("first updater id = %s, err = %v", got, err)
	}
	switched := readTestFile(t, livePath)
	if switched == original || agy.NormalizeInstallationID(switched) == "" {
		t.Fatalf("live updater id after switch = %s", switched)
	}
	if got, err := store.LoadDesktopUpdaterID(second); err != nil || got != switched {
		t.Fatalf("second updater id = %s, live = %s, err = %v", got, switched, err)
	}
	if installBefore != "" && readTestFile(t, installPath) != installBefore {
		t.Fatal("desktop switch changed the CLI installation id")
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "first", "--target=desktop"}); code != 0 {
		t.Fatalf("switch first exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("restored updater id = %s", got)
	}
}

func TestSwitchDoesNotClaimDifferentDesktopUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	first := reg.Active
	second := ""
	for _, account := range reg.Accounts {
		if account.ID != first {
			second = account.ID
		}
	}
	if first == "" || second == "" {
		t.Fatal("expected two accounts")
	}
	const stored = "11111111-1111-4111-8111-111111111111"
	const liveID = "22222222-2222-4222-8222-222222222222"
	if err := store.SaveDesktopUpdaterID(first, stored); err != nil {
		t.Fatal(err)
	}
	writeDesktopCookies(t, store.DesktopProfilePath(first))
	writeDesktopCookies(t, store.DesktopProfilePath(second))
	app, _, _ = env.app("")
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(liveID), 0o600); err != nil {
		t.Fatal(err)
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second", "--target=desktop"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "could not keep the current desktop updater id") {
		t.Fatalf("stderr = %s", errOut)
	}
	if got, err := store.LoadDesktopUpdaterID(first); err != nil || got != stored {
		t.Fatalf("first updater id = %s, err = %v", got, err)
	}
	got := readTestFile(t, livePath)
	if got == stored || got == liveID || agy.NormalizeInstallationID(got) == "" {
		t.Fatalf("live updater id = %s", got)
	}
}

func TestSwitchCLITargetDoesNotChangeDesktopUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"add", "--target=cli", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second", "--target=cli"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live updater id = %s", got)
	}
}

func TestSwitchRunningDesktopDoesNotChangeUpdaterID(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	first := reg.Active
	second := ""
	for _, account := range reg.Accounts {
		if account.ID != first {
			second = account.ID
		}
	}
	if first == "" || second == "" {
		t.Fatal("expected two accounts")
	}
	writeDesktopCookies(t, store.DesktopProfilePath(first))
	writeDesktopCookies(t, store.DesktopProfilePath(second))
	app, _, errOut = env.app("")
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := desktop.UpdaterIDPath(app.Desktop.ConfigDir)
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	app.Desktop.Ops = &desktop.ProcessOps{
		FindProcesses: func() ([]int, error) { return []int{9999}, nil },
	}
	if code := app.Run([]string{"switch", "second", "--target=desktop"}); code == 0 {
		t.Fatal("expected switch to fail while Desktop is running")
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live updater id = %s", got)
	}
	if _, err := os.Stat(filepath.Join(store.DesktopProfilePath(first), desktop.UpdaterIDFile)); !os.IsNotExist(err) {
		t.Fatalf("running desktop still stored an updater id: %v", err)
	}
}

func writeDesktopCookies(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cookies"), []byte("cookies"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchUnifiedDesktopSyncWithoutProfileSnapshot(t *testing.T) {
	env := newTestEnv(t, true)
	deskDataDir := filepath.Join(env.dir, "desktop_data")
	deskTokenPath := filepath.Join(env.dir, "jetski-standalone-oauth-token")

	const firstUpdater = "11111111-1111-4111-8111-111111111111"
	const firstDeskInstall = "33333333-3333-4333-8333-333333333333"

	app, _, errOut := env.app("")
	app.Desktop.DataDir = deskDataDir
	app.Storage.DesktopFilePath = deskTokenPath
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deskDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Desktop.ConfigDir, "app_storage.json"), []byte(`{"agy.onboarding.lastLoginUsername":"user@example.com","agy.onboarding.lastLoginIsGcpTos":"false","jetski.fontSize":13}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Desktop.ConfigDir, desktop.ScopeFileName), []byte("scope-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop.UpdaterIDPath(app.Desktop.ConfigDir), []byte(firstUpdater), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop.InstallationIDPath(deskDataDir), []byte(firstDeskInstall), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add first exit = %d, stderr = %s", code, errOut)
	}
	// Second account has NO stored desktop profile snapshot (like one added via `login`).
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	killCalled := false
	startCalled := false
	app, out, errOut := env.app("")
	app.Desktop.DataDir = deskDataDir
	app.Storage.DesktopFilePath = deskTokenPath
	app.Desktop.Ops = &desktop.ProcessOps{
		FindProcesses: func() ([]int, error) {
			if !killCalled {
				return []int{14373}, nil
			}
			return nil, nil
		},
		KillProcess: func(pid int) error {
			if pid == 14373 {
				killCalled = true
			}
			return nil
		},
		StartProcess: func(bin string) error {
			startCalled = true
			return nil
		},
	}

	if code := app.Run([]string{"switch", "second", "--relaunch"}); code != 0 {
		t.Fatalf("switch second exit = %d, stderr = %s", code, errOut)
	}
	if !killCalled || !startCalled {
		t.Fatalf("expected Desktop kill and relaunch, got kill=%v start=%v", killCalled, startCalled)
	}
	if !strings.Contains(out.String(), "Updated Antigravity Desktop state") {
		t.Errorf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), "Relaunched Antigravity Desktop") {
		t.Errorf("output missing relaunch notice = %q", out.String())
	}

	// Desktop fallback token file matches second account.
	deskTokRaw, err := os.ReadFile(deskTokenPath)
	if err != nil {
		t.Fatalf("desktop fallback token missing: %v", err)
	}
	tf, err := agy.ParseTokenFile(deskTokRaw)
	if err != nil || tf.Email() != "second@example.com" {
		t.Fatalf("desktop fallback token email = %q, err = %v", tf.Email(), err)
	}

	// Stale credential-user-scope was cleared and app_storage.json updated.
	if _, err := os.Stat(filepath.Join(app.Desktop.ConfigDir, desktop.ScopeFileName)); !os.IsNotExist(err) {
		t.Fatalf("stale credential-user-scope still exists: %v", err)
	}
	if got := desktop.LastLoginEmail(app.Desktop.ConfigDir); got != "second@example.com" {
		t.Fatalf("desktop last login = %q, want second@example.com", got)
	}

	// Both .updaterId and Desktop installation_id rotated to new UUIDs for second account.
	secondUpdater := readTestFile(t, desktop.UpdaterIDPath(app.Desktop.ConfigDir))
	if secondUpdater == firstUpdater || agy.NormalizeInstallationID(secondUpdater) == "" {
		t.Fatalf("second updater id = %q", secondUpdater)
	}
	secondDeskInstall := readTestFile(t, desktop.InstallationIDPath(deskDataDir))
	if secondDeskInstall == firstDeskInstall || agy.NormalizeInstallationID(secondDeskInstall) == "" {
		t.Fatalf("second desktop installation id = %q", secondDeskInstall)
	}

	// Switching back to first restores first's snapshot, scope, updater id, and desktop installation id.
	app, _, errOut = env.app("")
	app.Desktop.DataDir = deskDataDir
	app.Storage.DesktopFilePath = deskTokenPath
	if code := app.Run([]string{"switch", "first"}); code != 0 {
		t.Fatalf("switch back to first exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, desktop.UpdaterIDPath(app.Desktop.ConfigDir)); got != firstUpdater {
		t.Fatalf("restored updater id = %q, want %q", got, firstUpdater)
	}
	if got := readTestFile(t, desktop.InstallationIDPath(deskDataDir)); got != firstDeskInstall {
		t.Fatalf("restored desktop installation id = %q, want %q", got, firstDeskInstall)
	}
	if got := readTestFile(t, filepath.Join(app.Desktop.ConfigDir, desktop.ScopeFileName)); got != "scope-first" {
		t.Fatalf("restored scope = %q, want scope-first", got)
	}
}

func TestAddSkipsMismatchedDesktopEmail(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Desktop.ConfigDir, "app_storage.json"), []byte(`{"agy.onboarding.lastLoginUsername":"other@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"add", "--alias", "cliuser"}); code != 0 {
		t.Fatalf("add target=all exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "skipped Desktop profile capture") {
		t.Fatalf("expected mismatch warning, got %q", errOut.String())
	}
	reg, err := app.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Accounts[0].HasDesktop {
		t.Fatal("mismatched desktop profile should not be captured")
	}

	// Explicit --target=desktop fails with state_conflict.
	app, _, errOut = env.app("")
	if code := app.Run([]string{"add", "--target=desktop"}); code == 0 {
		t.Fatal("expected add --target=desktop to fail on email mismatch")
	}
	if !strings.Contains(errOut.String(), "other@example.com") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestLiveSwitchForwardsRelaunchAndTarget(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	reg, _ := app.Store.Load()
	second := reg.Resolve("second")[0]
	writeDesktopCookies(t, app.Store.DesktopProfilePath(second.ID))

	killCalled := false
	startCalled := false
	app, _, _ = env.app("")
	app.Desktop.Ops = &desktop.ProcessOps{
		FindProcesses: func() ([]int, error) {
			if !killCalled {
				return []int{4242}, nil
			}
			return nil, nil
		},
		KillProcess: func(pid int) error {
			if pid == 4242 {
				killCalled = true
			}
			return nil
		},
		StartProcess: func(bin string) error {
			startCalled = true
			return nil
		},
	}

	action := app.liveSwitchWithOpts(second.ID, second.DisplayName(), switchOptions{
		Target:   "all",
		Relaunch: true,
	})
	if !strings.HasPrefix(action, "Switched to") {
		t.Fatalf("liveSwitchWithOpts = %q", action)
	}
	if !killCalled || !startCalled {
		t.Fatalf("expected liveSwitchWithOpts to kill and relaunch Desktop, got kill=%v start=%v", killCalled, startCalled)
	}
}

func TestRemoveLogoutDeletesDesktopFallbackToken(t *testing.T) {
	env := newTestEnv(t, true)
	deskTokenPath := filepath.Join(env.dir, "jetski-standalone-oauth-token")
	if err := os.WriteFile(deskTokenPath, env.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	app.Storage.DesktopFilePath = deskTokenPath
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	app, _, errOut = env.app("")
	app.Storage.DesktopFilePath = deskTokenPath
	if code := app.Run([]string{"remove", "first", "--logout", "--yes"}); code != 0 {
		t.Fatalf("remove exit = %d, stderr = %s", code, errOut)
	}
	if _, err := os.Stat(deskTokenPath); !os.IsNotExist(err) {
		t.Fatalf("expected desktop fallback token deleted on logout, err = %v", err)
	}
}

func TestLoginSyncsAndRelaunchesDesktop(t *testing.T) {
	setTestOAuthClient(t)
	env := newTestEnv(t, true)
	deskDataDir := filepath.Join(env.dir, "desktop_data")
	deskTokenPath := filepath.Join(env.dir, "jetski-standalone-oauth-token")

	const prevUpdater = "11111111-1111-4111-8111-111111111111"
	const prevDeskInstall = "44444444-4444-4444-8444-444444444444"

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "login-access",
			"expires_in":    3600,
			"refresh_token": "login-refresh",
			"token_type":    "Bearer",
			"id_token":      makeCLIIDToken(t, "fresh@example.com", "subject-fresh"),
		})
	}))
	defer server.Close()
	oldEndpoint, oldClient := oauth.TokenEndpoint, oauth.HTTPClient
	oauth.TokenEndpoint = server.URL
	oauth.HTTPClient = server.Client()
	t.Cleanup(func() {
		oauth.TokenEndpoint = oldEndpoint
		oauth.HTTPClient = oldClient
	})

	// Seed live Desktop files belonging to the currently active CLI user.
	app, _, _ := env.app("")
	if err := os.MkdirAll(app.Desktop.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deskDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Desktop.ConfigDir, "app_storage.json"), []byte(`{"agy.onboarding.lastLoginUsername":"user@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Desktop.ConfigDir, desktop.ScopeFileName), []byte("scope-prev"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop.UpdaterIDPath(app.Desktop.ConfigDir), []byte(prevUpdater), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop.InstallationIDPath(deskDataDir), []byte(prevDeskInstall), 0o600); err != nil {
		t.Fatal(err)
	}

	killCalled := false
	startCalled := false
	app, out, errOut := env.app("auth-code\n")
	app.Desktop.DataDir = deskDataDir
	app.Storage.DesktopFilePath = deskTokenPath
	app.Desktop.Ops = &desktop.ProcessOps{
		FindProcesses: func() ([]int, error) {
			if !killCalled {
				return []int{14373}, nil
			}
			return nil, nil
		},
		KillProcess: func(pid int) error {
			if pid == 14373 {
				killCalled = true
			}
			return nil
		},
		StartProcess: func(bin string) error {
			startCalled = true
			return nil
		},
	}

	if code := app.Run([]string{"login", "--no-browser", "--alias", "fresh", "--relaunch"}); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, errOut)
	}
	if !killCalled || !startCalled {
		t.Fatalf("expected Desktop kill and relaunch on login, got kill=%v start=%v", killCalled, startCalled)
	}
	if !strings.Contains(out.String(), "Relaunched Antigravity Desktop") {
		t.Errorf("login output = %q", out.String())
	}

	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(reg.Accounts))
	}
	prevAcct := reg.Resolve("user@example.com")[0]
	freshAcct := reg.Resolve("fresh")[0]

	if got, err := store.LoadDesktopUpdaterID(prevAcct.ID); err != nil || got != prevUpdater {
		t.Fatalf("previous account updater id = %q, err = %v", got, err)
	}
	if got, err := store.LoadDesktopInstallationID(prevAcct.ID); err != nil || got != prevDeskInstall {
		t.Fatalf("previous account desktop installation id = %q, err = %v", got, err)
	}
	newUpdater := readTestFile(t, desktop.UpdaterIDPath(app.Desktop.ConfigDir))
	if newUpdater == prevUpdater || agy.NormalizeInstallationID(newUpdater) == "" {
		t.Fatalf("new updater id = %q", newUpdater)
	}
	newDeskInstall := readTestFile(t, desktop.InstallationIDPath(deskDataDir))
	if newDeskInstall == prevDeskInstall || agy.NormalizeInstallationID(newDeskInstall) == "" {
		t.Fatalf("new desktop installation id = %q", newDeskInstall)
	}
	if got, err := store.LoadDesktopUpdaterID(freshAcct.ID); err != nil || got != newUpdater {
		t.Fatalf("fresh account stored updater id = %q, live = %q, err = %v", got, newUpdater, err)
	}
	if _, err := os.Stat(filepath.Join(app.Desktop.ConfigDir, desktop.ScopeFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected stale scope removed on login, err = %v", err)
	}
	if got := desktop.LastLoginEmail(app.Desktop.ConfigDir); got != "fresh@example.com" {
		t.Fatalf("desktop last login = %q, want fresh@example.com", got)
	}
	deskTokRaw, err := os.ReadFile(deskTokenPath)
	if err != nil {
		t.Fatalf("desktop fallback token missing: %v", err)
	}
	tf, err := agy.ParseTokenFile(deskTokRaw)
	if err != nil || tf.Email() != "fresh@example.com" {
		t.Fatalf("desktop fallback token email = %q, err = %v", tf.Email(), err)
	}
}

func TestIsIsolatedCLIOverride(t *testing.T) {
	t.Setenv("AGY_AUTH_AGY_TOKEN_FILE", "/tmp/isolated-token")
	t.Setenv("AGY_DESKTOP_HOME", "")
	t.Setenv("AGY_DESKTOP_CONFIG_DIR", "")
	t.Setenv("AGY_DESKTOP_DATA_DIR", "")
	if !isIsolatedCLIOverride() {
		t.Fatal("expected isolated CLI override when AGY_AUTH_AGY_TOKEN_FILE is set without desktop overrides")
	}

	t.Setenv("AGY_DESKTOP_HOME", "/tmp/isolated-desktop")
	if isIsolatedCLIOverride() {
		t.Fatal("expected desktop enabled when AGY_DESKTOP_HOME is explicitly set")
	}
}
