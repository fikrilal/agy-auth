package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/desktop"
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
