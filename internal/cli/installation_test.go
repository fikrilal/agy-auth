package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func TestAddKeepsExistingInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live id after add = %s", got)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(reg.Accounts))
	}
	first := reg.Accounts[0].ID
	stored, err := store.LoadInstallationID(first)
	if err != nil {
		t.Fatal(err)
	}
	if stored != original {
		t.Fatalf("stored first id = %s", stored)
	}
}

func TestSwitchMintsAndRestoresInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	first := reg.Accounts[0].ID

	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second"}); code != 0 {
		t.Fatalf("switch second exit = %d, stderr = %s", code, errOut)
	}
	switched := readTestFile(t, livePath)
	if switched == original || agy.NormalizeInstallationID(switched) == "" {
		t.Fatalf("live id after switch = %s", switched)
	}
	if got, err := store.LoadInstallationID(first); err != nil || got != original {
		t.Fatalf("first id after switch = %s, %v", got, err)
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var second string
	for _, account := range reg.Accounts {
		if account.ID != first {
			second = account.ID
		}
	}
	if second == "" {
		t.Fatal("second account missing")
	}
	if got, err := store.LoadInstallationID(second); err != nil || got != switched {
		t.Fatalf("stored second id = %s, live = %s, err = %v", got, switched, err)
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "first"}); code != 0 {
		t.Fatalf("switch first exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live id after switch back = %s", got)
	}
	info, err := os.Stat(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("live installation id mode = %o", info.Mode().Perm())
	}
}

func TestAddReplacesInvalidInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte("not-a-uuid"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	got := readTestFile(t, livePath)
	if agy.NormalizeInstallationID(got) != got {
		t.Fatalf("live id = %q", got)
	}
	if strings.Contains(errOut.String(), "warning:") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestAddDesktopTargetDoesNotChangeInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--target", "desktop", "--alias", "desk"}); code == 0 {
		t.Fatal("desktop add succeeded without a desktop profile")
	}
	if got := readTestFile(t, livePath); got != original {
		t.Fatalf("live id = %s, stderr = %s", got, errOut)
	}
	entries, err := os.ReadDir(filepath.Join(env.storeDir, "accounts"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".installation_id") {
			t.Fatalf("desktop add wrote %s", entry.Name())
		}
	}
}

func TestImportActivateKeepsUnsavedAccountInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(env.dir, "other.json")
	if err := os.WriteFile(other, makeCLIToken(t, "other@example.com", "subject-other", "refresh-other"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", other, "--activate", "--alias", "other"}); code != 0 {
		t.Fatalf("import exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Saved current account") {
		t.Fatalf("stdout = %s", out)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Fatalf("accounts = %d", len(reg.Accounts))
	}
	live := readTestFile(t, livePath)
	if live == original || agy.NormalizeInstallationID(live) == "" {
		t.Fatalf("live id = %s", live)
	}
	var sawOriginal, sawLive bool
	for _, account := range reg.Accounts {
		stored, err := store.LoadInstallationID(account.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch stored {
		case original:
			sawOriginal = true
			if account.Email == "other@example.com" {
				t.Fatal("imported account kept the previous installation id")
			}
		case live:
			sawLive = true
			if account.Email != "other@example.com" {
				t.Fatalf("live id stored on %s", account.Email)
			}
		default:
			t.Fatalf("account %s id = %s", account.Email, stored)
		}
	}
	if !sawOriginal || !sawLive {
		t.Fatalf("original stored = %v, live stored = %v", sawOriginal, sawLive)
	}
}

func TestSwitchDoesNotClaimUnboundInstallationID(t *testing.T) {
	env := newTestEnv(t, true)
	const original = "11111111-1111-4111-8111-111111111111"
	livePath := filepath.Join(filepath.Dir(env.tokenPath), agy.InstallationFileName)
	if err := os.WriteFile(livePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(store.AccountsDir, reg.Accounts[0].ID+".installation_id")
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.tokenPath, []byte("not-a-credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut = env.app("")
	if claim := app.retainLiveInstallationID(); claim {
		t.Fatal("retain claimed an installation id it could not bind")
	}
	if !strings.Contains(errOut.String(), "could not keep the current installation id") {
		t.Fatalf("stderr = %s", errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	if got := readTestFile(t, livePath); got == original {
		t.Fatal("switch claimed the unbound installation id")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
