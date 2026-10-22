package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/backup"
	"github.com/fikrilal/agy-auth/internal/oauth"
	"github.com/fikrilal/agy-auth/internal/registry"
)

var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func makeCLIToken(t *testing.T, email, subject, refresh string) []byte {
	t.Helper()
	document := map[string]any{
		"token": map[string]any{
			"access_token":  "access-" + refresh,
			"token_type":    "Bearer",
			"refresh_token": refresh,
			"expiry":        testNow.Add(time.Hour).Format(time.RFC3339Nano),
		},
		"auth_method": "consumer",
		"id_token":    makeCLIIDToken(t, email, subject),
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func makeCLIIDToken(t *testing.T, email, subject string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"email": email, "sub": subject})
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

type testEnv struct {
	dir       string
	tokenPath string
	storeDir  string
	raw       []byte
}

func newTestEnv(t *testing.T, withToken bool) *testEnv {
	t.Helper()
	dir := t.TempDir()
	env := &testEnv{
		dir:       dir,
		tokenPath: filepath.Join(dir, "agy", "antigravity-oauth-token"),
		storeDir:  filepath.Join(dir, "config"),
	}
	if withToken {
		env.raw = makeCLIToken(t, "user@example.com", "subject-1", "refresh-1")
		if err := os.MkdirAll(filepath.Dir(env.tokenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(env.tokenPath, env.raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

func (e *testEnv) app(input string) (*App, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &App{
		In:      strings.NewReader(input),
		Out:     out,
		Err:     errOut,
		Store:   registry.NewStore(e.storeDir),
		Storage: &agy.Storage{Service: "test", User: "test", FilePath: e.tokenPath, NoKeyring: true, Backup: backup.New(filepath.Join(e.storeDir, "backups"))},
		Now:     func() time.Time { return testNow },
		TTY:     false,
		OutTTY:  false,
	}, out, errOut
}

func addSecondAccount(t *testing.T, env *testEnv, email, subject, refresh, alias string) {
	t.Helper()
	path := filepath.Join(env.dir, subject+".json")
	if err := os.WriteFile(path, makeCLIToken(t, email, subject, refresh), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, errOut := env.app("")
	if code := app.Run([]string{"import", path, "--alias", alias}); code != 0 {
		t.Fatalf("import %s exit = %d, stderr = %s", alias, code, errOut)
	}
}

func TestCLILifecycle(t *testing.T) {
	env := newTestEnv(t, true)

	app, out, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "personal"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Added account") {
		t.Errorf("add output = %q", out.String())
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"list", "--json"}); code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.SchemaVersion != 1 || envelope.Command != "list" || envelope.Error != nil {
		t.Fatalf("unexpected list envelope: %+v", envelope)
	}
	var listPayload struct {
		Accounts []accountRow `json:"accounts"`
		Current  *accountRow  `json:"current"`
	}
	if err := json.Unmarshal(envelope.Data, &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(listPayload.Accounts))
	}
	if !listPayload.Accounts[0].Active || listPayload.Accounts[0].Alias != "personal" {
		t.Errorf("unexpected account: %+v", listPayload.Accounts[0])
	}
	if listPayload.Current == nil || !listPayload.Current.Stored {
		t.Errorf("unexpected current: %+v", listPayload.Current)
	}

	app, _, _ = env.app("")
	if code := app.Run([]string{"switch", "1"}); code != 0 {
		t.Fatalf("switch exit = %d", code)
	}
	info, err := os.Stat(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token permissions = %o", info.Mode().Perm())
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"alias", "set", "1", "work"}); code != 0 {
		t.Fatalf("alias exit = %d, stderr = %s", code, errOut)
	}

	exportDir := filepath.Join(env.dir, "export")
	app, _, errOut = env.app("")
	if code := app.Run([]string{"export", "--dir", exportDir}); code != 0 {
		t.Fatalf("export exit = %d, stderr = %s", code, errOut)
	}
	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("exported files = %d", len(entries))
	}
	exported := filepath.Join(exportDir, entries[0].Name())

	// Flags after the positional argument must still parse.
	app, _, errOut = env.app("")
	if code := app.Run([]string{"import", exported, "--alias", "imported"}); code != 0 {
		t.Fatalf("import exit = %d, stderr = %s", code, errOut)
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"--json", "list"}); code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	envelope = decodeTestEnvelope(t, out)
	if envelope.Command != "list" {
		t.Fatalf("command = %q", envelope.Command)
	}
	listPayload.Accounts = nil
	if err := json.Unmarshal(envelope.Data, &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Accounts) != 1 || listPayload.Accounts[0].Alias != "imported" {
		t.Errorf("unexpected accounts after import: %+v", listPayload.Accounts)
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"remove", "--all", "--yes", "--logout"}); code != 0 {
		t.Fatalf("remove exit = %d, stderr = %s", code, errOut)
	}
	if _, err := os.Stat(env.tokenPath); !os.IsNotExist(err) {
		t.Error("token file should have been removed")
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"list"}); code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if !strings.Contains(out.String(), "No accounts yet") {
		t.Errorf("list output = %q", out.String())
	}
}

func TestSwitchPrevious(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	store := registry.NewStore(env.storeDir)
	app, out, errOut := env.app("")
	if code := app.Run([]string{"switch", "second", "--json"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var data struct {
		Account registry.Account `json:"account"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Account.Email != "second@example.com" {
		t.Errorf("switched account = %+v", data.Account)
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != data.Account.ID || reg.Previous == "" {
		t.Fatalf("active = %q, previous = %q", reg.Active, reg.Previous)
	}
	previousID := reg.Previous

	app, out, errOut = env.app("")
	if code := app.Run([]string{"switch", "-", "--json"}); code != 0 {
		t.Fatalf("switch - exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Account.ID != previousID || data.Account.Email != "user@example.com" {
		t.Errorf("previous switch = %+v, want %q", data.Account, previousID)
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != previousID {
		t.Errorf("active = %q, want %q", reg.Active, previousID)
	}
}

func TestSwitchPreviousMissing(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	app, out, _ := env.app("")
	if code := app.Run([]string{"switch", "-", "--json"}); code != 1 {
		t.Fatalf("switch - exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "previous_not_found" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
}

func TestSwitchAmbiguousPicker(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, out, errOut := env.app("2\n")
	app.TTY = true
	if code := app.Run([]string{"switch", "example.com"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Select an account to activate") {
		t.Errorf("missing picker prompt: %q", out.String())
	}
	if !strings.Contains(out.String(), "Switched to second") {
		t.Errorf("missing switch result: %q", out.String())
	}
}

func TestSwitchAmbiguousNonTTY(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, out, _ := env.app("")
	if code := app.Run([]string{"switch", "example.com", "--json"}); code != 1 {
		t.Fatalf("switch exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "ambiguous_query" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if len(envelope.Error.Candidates) != 2 {
		t.Errorf("candidates = %d", len(envelope.Error.Candidates))
	}
}

func TestRemoveClearsPrevious(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second", "--json"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	store := registry.NewStore(env.storeDir)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Previous == "" {
		t.Fatal("previous account not recorded")
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"remove", "first", "--yes", "--json"}); code != 0 {
		t.Fatalf("remove exit = %d, stderr = %s", code, errOut)
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Previous != "" {
		t.Errorf("previous = %q after removing it", reg.Previous)
	}

	app, out, _ := env.app("")
	if code := app.Run([]string{"switch", "-", "--json"}); code != 1 {
		t.Fatalf("switch - exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "previous_not_found" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
}

func TestCLIWithoutCredentials(t *testing.T) {
	env := newTestEnv(t, false)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"current"}); code != 1 {
		t.Fatalf("current exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "no agy credentials") {
		t.Errorf("stderr = %q", errOut.String())
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch"}); code != 1 {
		t.Fatalf("switch exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "no stored accounts") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestCLIHelpAndUnknown(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	if code := app.Run([]string{"help"}); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(out.String(), "switch") {
		t.Errorf("help output missing commands")
	}

	app, _, _ = env.app("")
	if code := app.Run([]string{"unknown-command"}); code != 2 {
		t.Errorf("unknown command exit = %d", code)
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"help", "switch"}); code != 0 {
		t.Fatalf("help switch exit = %d", code)
	}
	if !strings.Contains(out.String(), "interactive picker") {
		t.Errorf("switch help output = %q", out.String())
	}
}

func TestGeneratedHelpListsEverySpec(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, errOut := env.app("")
	if code := app.Run([]string{"help"}); code != 0 {
		t.Fatalf("help exit = %d, stderr = %s", code, errOut)
	}
	for _, spec := range commandSpecs {
		if !strings.Contains(out.String(), spec.Synopsis) {
			t.Errorf("help output missing %q", spec.Synopsis)
		}
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	env := newTestEnv(t, false)
	cases := []struct {
		name string
		args []string
	}{
		{"list extra", []string{"list", "extra"}},
		{"add extra", []string{"add", "extra"}},
		{"add invalid alias", []string{"add", "--alias", "1"}},
		{"login extra", []string{"login", "extra"}},
		{"login invalid alias", []string{"login", "--alias", "1"}},
		{"switch two queries", []string{"switch", "one", "two"}},
		{"remove all plus query", []string{"remove", "--all", "work"}},
		{"remove missing selector", []string{"remove"}},
		{"alias missing subcommand", []string{"alias"}},
		{"alias set missing alias", []string{"alias", "set", "work"}},
		{"alias list extra", []string{"alias", "list", "extra"}},
		{"import missing path", []string{"import"}},
		{"status extra", []string{"status", "extra"}},
		{"current extra", []string{"current", "extra"}},
		{"path extra", []string{"path", "extra"}},
		{"version extra", []string{"version", "extra"}},
		{"unknown flag", []string{"list", "--nope"}},
		{"unknown command", []string{"unknown-command"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _, errOut := env.app("")
			if code := app.Run(tc.args); code != 2 {
				t.Fatalf("exit = %d, stderr = %s", code, errOut)
			}
			for _, want := range []string{"error:", "Usage:", "hint:"} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("stderr missing %q: %q", want, errOut.String())
				}
			}
		})
	}
}

func TestAliasSubcommandHelp(t *testing.T) {
	env := newTestEnv(t, false)
	for _, args := range [][]string{
		{"alias", "--help"},
		{"alias", "list", "--help"},
	} {
		app, out, errOut := env.app("")
		if code := app.Run(args); code != 0 {
			t.Errorf("%v exit = %d, stderr = %s", args, code, errOut)
			continue
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%v output = %q", args, out.String())
		}
	}
}

func TestJSONEnvelopeVersionAndPath(t *testing.T) {
	env := newTestEnv(t, false)

	app, out, errOut := env.app("")
	if code := app.Run([]string{"-v", "--json"}); code != 0 {
		t.Fatalf("version exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.SchemaVersion != 1 || envelope.Command != "version" || envelope.Error != nil {
		t.Fatalf("unexpected version envelope: %+v", envelope)
	}
	var versionData struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(envelope.Data, &versionData); err != nil {
		t.Fatal(err)
	}
	if versionData.Version != Version {
		t.Errorf("version = %q, want %q", versionData.Version, Version)
	}
	if errOut.Len() != 0 {
		t.Errorf("version stderr = %q", errOut.String())
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"paths", "--json"}); code != 0 {
		t.Fatalf("path exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if envelope.SchemaVersion != 1 || envelope.Command != "path" || envelope.Error != nil {
		t.Fatalf("unexpected path envelope: %+v", envelope)
	}
	var pathData map[string]any
	if err := json.Unmarshal(envelope.Data, &pathData); err != nil {
		t.Fatal(err)
	}
	registryDir, _ := pathData["registry"].(string)
	registryFile, _ := pathData["registry_file"].(string)
	if registryDir == "" || registryFile == "" {
		t.Errorf("unexpected path data: %+v", pathData)
	}
	if errOut.Len() != 0 {
		t.Errorf("path stderr = %q", errOut.String())
	}
}

func TestJSONUsageErrors(t *testing.T) {
	env := newTestEnv(t, false)
	cases := []struct {
		name    string
		args    []string
		command string
	}{
		{"extra positional", []string{"list", "extra", "--json"}, "list"},
		{"unknown flag", []string{"list", "--json", "--nope"}, "list"},
		{"unknown command", []string{"unknown-command", "--json"}, "unknown-command"},
		{"no command", []string{"--json"}, "help"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, out, errOut := env.app("")
			if code := app.Run(tc.args); code != 2 {
				t.Fatalf("exit = %d, stderr = %s", code, errOut)
			}
			envelope := decodeTestEnvelope(t, out)
			if envelope.SchemaVersion != 1 || envelope.Command != tc.command {
				t.Fatalf("unexpected envelope: %+v", envelope)
			}
			if envelope.Error == nil {
				t.Fatal("missing error document")
			}
			if envelope.Error.Code != "usage" {
				t.Errorf("code = %q", envelope.Error.Code)
			}
			if envelope.Error.Message == "" || envelope.Error.Hint == "" {
				t.Errorf("incomplete error: %+v", envelope.Error)
			}
			if errOut.Len() != 0 {
				t.Errorf("stderr = %q", errOut.String())
			}
		})
	}
}

func TestJSONOperationError(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, errOut := env.app("")
	if code := app.Run([]string{"current", "--json"}); code != 1 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.SchemaVersion != 1 || envelope.Command != "current" {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if envelope.Error == nil || envelope.Error.Code != "credential_not_found" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if !strings.Contains(envelope.Error.Message, "no agy credentials") {
		t.Errorf("message = %q", envelope.Error.Message)
	}
	if envelope.Error.Hint == "" {
		t.Errorf("missing hint: %+v", envelope.Error)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestJSONWarningsStayOnStderr(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, errOut := env.app("")
	app.JSON = true
	app.Command = "login"
	result := registry.UpsertResult{
		Created: true,
		Account: registry.Account{ID: "acct_test", Email: "user@example.com"},
	}
	if code := app.finishLogin(result, agy.WriteReport{}, []string{"test warning"}); code != 0 {
		t.Fatalf("finishLogin exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Command != "login" {
		t.Fatalf("command = %q", envelope.Command)
	}
	if bytes.Contains(out.Bytes(), []byte(`"warnings"`)) {
		t.Errorf("warnings leaked into JSON: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "warning: test warning") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestAliasMessages(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"alias", "set", "1", "work"}, "Set alias"},
		{[]string{"alias", "set", "1", "work"}, "already"},
		{[]string{"alias", "set", "1", "office"}, "Updated alias"},
		{[]string{"alias", "clear", "1"}, "Cleared alias"},
	}
	for _, tc := range cases {
		app, out, errOut := env.app("")
		if code := app.Run(tc.args); code != 0 {
			t.Fatalf("%v exit = %d, stderr = %s", tc.args, code, errOut)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v output = %q, want %q", tc.args, out.String(), tc.want)
		}
	}
}

func TestAliasAmbiguousPicker(t *testing.T) {
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
	firstMatches := reg.Resolve("first")
	if len(firstMatches) != 1 {
		t.Fatalf("first account matches = %d", len(firstMatches))
	}
	firstID := firstMatches[0].ID

	app, out, errOut := env.app("1\n")
	app.TTY = true
	if code := app.Run([]string{"alias", "set", "example.com", "chosen"}); code != 0 {
		t.Fatalf("alias exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Select an account") {
		t.Errorf("missing candidate picker: %q", out.String())
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	account := reg.FindByID(firstID)
	if account == nil || account.Alias != "chosen" {
		t.Errorf("first account = %+v", account)
	}
}

func TestRemoveAtomicResolution(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, out, _ := env.app("")
	if code := app.Run([]string{"remove", "first", "missing", "--yes", "--json"}); code != 1 {
		t.Fatalf("remove exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "account_not_found" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}

	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Errorf("accounts = %d after failed resolution", len(reg.Accounts))
	}
	if _, err := os.Stat(env.tokenPath); err != nil {
		t.Errorf("first snapshot was removed before resolution completed: %v", err)
	}
}

func TestRemoveDeduplicatesSelectors(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, out, _ := env.app("")
	if code := app.Run([]string{"remove", "first", "first", "--yes", "--json"}); code != 0 {
		t.Fatalf("remove exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	var data struct {
		Removed []registry.Account `json:"removed"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Removed) != 1 {
		t.Errorf("removed = %d, want 1", len(data.Removed))
	}

	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 {
		t.Errorf("accounts = %d, want 1", len(reg.Accounts))
	}
}

func TestRemoveAmbiguousPicker(t *testing.T) {
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
	secondMatches := reg.Resolve("second")
	if len(secondMatches) != 1 {
		t.Fatalf("second account matches = %d", len(secondMatches))
	}
	secondID := secondMatches[0].ID

	app, out, errOut := env.app("2\n")
	app.TTY = true
	if code := app.Run([]string{"remove", "example.com", "--yes"}); code != 0 {
		t.Fatalf("remove exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Select an account to remove") {
		t.Errorf("missing candidate picker: %q", out.String())
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 || reg.FindByID(secondID) != nil {
		t.Errorf("unexpected accounts after remove: %+v", reg.Accounts)
	}
}

func TestExportAmbiguousPicker(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	dir := filepath.Join(env.dir, "export-picker")
	app, out, errOut := env.app("1\n")
	app.TTY = true
	if code := app.Run([]string{"export", "--dir", dir, "example.com"}); code != 0 {
		t.Fatalf("export exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Select an account to export") {
		t.Errorf("missing candidate picker: %q", out.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("exported files = %d", len(entries))
	}
}

func TestCleanJSON(t *testing.T) {
	env := newTestEnv(t, false)
	store := registry.NewStore(env.storeDir)
	if err := store.Save(&registry.Registry{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.AccountsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(store.AccountsDir, "acct_orphan.json")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"clean", "--dry-run", "--json"}); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Command != "clean" {
		t.Fatalf("command = %q", envelope.Command)
	}
	var data struct {
		DryRun    bool     `json:"dry_run"`
		Backups   []string `json:"backups"`
		Snapshots []string `json:"snapshots"`
		Preserved bool     `json:"preserved"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !data.DryRun || data.Preserved || !reflect.DeepEqual(data.Snapshots, []string{"acct_orphan.json"}) {
		t.Fatalf("unexpected dry-run data: %+v", data)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("dry run removed orphan: %v", err)
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"clean", "--json"}); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.DryRun || data.Preserved || !reflect.DeepEqual(data.Snapshots, []string{"acct_orphan.json"}) {
		t.Fatalf("unexpected clean data: %+v", data)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("orphan still exists: %v", err)
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"clean", "--json"}); code != 0 {
		t.Fatalf("second clean exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Snapshots) != 0 {
		t.Errorf("snapshots = %v after clean", data.Snapshots)
	}
}

func writeTestToken(t *testing.T, dir, name, email, subject, refresh string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, makeCLIToken(t, email, subject, refresh), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportReportSkipsInvalidFiles(t *testing.T) {
	env := newTestEnv(t, false)
	dir := filepath.Join(env.dir, "imports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestToken(t, dir, "a.json", "a@example.com", "subject-a", "refresh-a")
	writeTestToken(t, dir, "b.json", "b@example.com", "subject-b", "refresh-b")
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", dir, "--json"}); code != 0 {
		t.Fatalf("import exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Command != "import" {
		t.Fatalf("command = %q", envelope.Command)
	}
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Imported != 2 || report.Summary.Updated != 0 || report.Summary.Skipped != 1 || report.Summary.Total != 3 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if len(report.Files) != 3 || report.Files[2].Outcome != outcomeSkipped || report.Files[2].Reason != "invalid credential" {
		t.Errorf("files = %+v", report.Files)
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Errorf("accounts = %d, want 2", len(reg.Accounts))
	}
}

func TestImportReportHuman(t *testing.T) {
	env := newTestEnv(t, false)
	dir := filepath.Join(env.dir, "imports-human")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestToken(t, dir, "good.json", "good@example.com", "subject-good", "refresh-good")
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", dir}); code != 0 {
		t.Fatalf("import exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "imported  good.json") {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(out.String(), "Import Summary: 1 imported, 0 updated, 1 skipped") {
		t.Errorf("stdout summary = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "skipped  bad.json: invalid credential") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestImportSingleInvalidFails(t *testing.T) {
	env := newTestEnv(t, false)
	badPath := filepath.Join(env.dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, _ := env.app("")
	if code := app.Run([]string{"import", badPath, "--json"}); code != 1 {
		t.Fatalf("import exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Imported != 0 || report.Summary.Skipped != 1 || report.Summary.Total != 1 {
		t.Errorf("summary = %+v", report.Summary)
	}
	if len(report.Files) != 1 || report.Files[0].Reason != "invalid credential" {
		t.Errorf("files = %+v", report.Files)
	}
}

func TestImportActivate(t *testing.T) {
	env := newTestEnv(t, false)
	path := writeTestToken(t, env.dir, "new.json", "new@example.com", "subject-new", "refresh-new")
	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", path, "--activate", "--json"}); code != 0 {
		t.Fatalf("import exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Activated || report.Summary.Imported != 1 {
		t.Errorf("report = %+v", report)
	}
	if _, err := os.Stat(env.tokenPath); err != nil {
		t.Errorf("live credential was not written: %v", err)
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != report.Imported[0].ID {
		t.Errorf("active = %q, want %q", reg.Active, report.Imported[0].ID)
	}
}

func TestPurgeRebuildsRegistry(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	app, _, errOut = env.app("")
	if code := app.Run([]string{"alias", "set", "second", "work"}); code != 0 {
		t.Fatalf("alias exit = %d, stderr = %s", code, errOut)
	}
	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "second", "--json"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", "--purge", "--json"}); code != 0 {
		t.Fatalf("purge exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Rebuilt || report.Summary.Imported != 2 || report.Summary.Skipped != 0 {
		t.Fatalf("report = %+v", report)
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Fatalf("accounts = %d", len(reg.Accounts))
	}
	first := reg.Resolve("first")
	second := reg.Resolve("work")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("aliases not carried: first=%v second=%v", first, second)
	}
	if reg.Active != second[0].ID || reg.Previous != first[0].ID {
		t.Errorf("active = %q, previous = %q", reg.Active, reg.Previous)
	}
}

func TestPurgeWithoutRegistry(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	if err := os.Remove(filepath.Join(env.storeDir, "registry.json")); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", "--purge", "--json"}); code != 0 {
		t.Fatalf("purge exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Imported != 2 || report.Active == "" {
		t.Fatalf("report = %+v", report)
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 || reg.Active == "" {
		t.Errorf("registry = %+v", reg)
	}
}

func TestExportReport(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	dir := filepath.Join(env.dir, "export-report")
	app, out, errOut := env.app("")
	if code := app.Run([]string{"export", "--dir", dir, "--json"}); code != 0 {
		t.Fatalf("export exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report exportReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Exported != 2 || report.Summary.Skipped != 0 || report.Summary.Total != 2 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if len(report.Exported) != 2 || len(report.Files) != 2 {
		t.Errorf("report = %+v", report)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("exported files = %d", len(entries))
	}
	for _, event := range report.Files {
		if event.Outcome != outcomeExported || !strings.HasSuffix(event.Path, ".json") {
			t.Errorf("event = %+v", event)
		}
	}
}

func TestExportSkipsUnreadableCredential(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	second := reg.Resolve("second")
	if len(second) != 1 {
		t.Fatalf("second matches = %d", len(second))
	}
	if err := os.Remove(filepath.Join(env.storeDir, "accounts", second[0].ID+".json")); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(env.dir, "export-skip")
	app, out, errOut := env.app("")
	if code := app.Run([]string{"export", "--dir", dir, "--json"}); code != 0 {
		t.Fatalf("export exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report exportReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Exported != 1 || report.Summary.Skipped != 1 {
		t.Errorf("summary = %+v", report.Summary)
	}
	for _, event := range report.Files {
		if event.Outcome == outcomeSkipped && event.Reason != "credential unreadable" {
			t.Errorf("skipped event = %+v", event)
		}
	}

	first := reg.Resolve("first")
	if len(first) != 1 {
		t.Fatalf("first matches = %d", len(first))
	}
	if err := os.Remove(filepath.Join(env.storeDir, "accounts", first[0].ID+".json")); err != nil {
		t.Fatal(err)
	}
	app, out, _ = env.app("")
	if code := app.Run([]string{"export", "--dir", filepath.Join(env.dir, "export-skip-all"), "--json"}); code != 1 {
		t.Fatalf("all-skipped export exit = %d", code)
	}
	envelope = decodeTestEnvelope(t, out)
	var all exportReport
	if err := json.Unmarshal(envelope.Data, &all); err != nil {
		t.Fatal(err)
	}
	if all.Summary.Exported != 0 || all.Summary.Skipped != 2 {
		t.Errorf("all-skipped summary = %+v", all.Summary)
	}
}

func TestPurgeMalformedRegistry(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")
	if err := os.WriteFile(filepath.Join(env.storeDir, "registry.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"import", "--purge", "--json"}); code != 0 {
		t.Fatalf("purge exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var report importReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Imported != 2 {
		t.Errorf("summary = %+v", report.Summary)
	}
	if !strings.Contains(errOut.String(), "rebuilding without aliases") {
		t.Errorf("stderr = %q", errOut.String())
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 {
		t.Errorf("accounts = %d", len(reg.Accounts))
	}
}

func TestJSONInteractiveGuards(t *testing.T) {
	env := newTestEnv(t, false)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"switch without query", []string{"switch", "--json"}, "requires a query"},
		{"remove without selection", []string{"remove", "--json"}, "requires one or more queries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, out, errOut := env.app("")
			if code := app.Run(tc.args); code != 2 {
				t.Fatalf("exit = %d, stderr = %s", code, errOut)
			}
			envelope := decodeTestEnvelope(t, out)
			if envelope.Error == nil || envelope.Error.Code != "usage" {
				t.Fatalf("unexpected error: %+v", envelope.Error)
			}
			if !strings.Contains(envelope.Error.Message, tc.want) {
				t.Errorf("message = %q, want %q", envelope.Error.Message, tc.want)
			}
		})
	}
}

func TestAmbiguousQueryCandidates(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.JSON = true
	app.Command = "switch"
	matches := []registry.Account{
		{ID: "acct_1", Email: "same@example.com", Alias: "one"},
		{ID: "acct_2", Email: "same@example.com", Alias: "two"},
	}
	if code := app.resolveFailure("same@example.com", matches); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "ambiguous_query" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if len(envelope.Error.Candidates) != 2 {
		t.Fatalf("candidates = %d", len(envelope.Error.Candidates))
	}
	if envelope.Error.Candidates[0].ID != "acct_1" || envelope.Error.Candidates[1].ID != "acct_2" {
		t.Errorf("candidates = %+v", envelope.Error.Candidates)
	}
}

func TestAccountNotFoundCode(t *testing.T) {
	env := newTestEnv(t, false)

	app, out, _ := env.app("")
	if code := app.Run([]string{"switch", "missing", "--json"}); code != 1 {
		t.Fatalf("switch exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "account_not_found" {
		t.Fatalf("unexpected switch error: %+v", envelope.Error)
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"remove", "--all", "--yes", "--json"}); code != 1 {
		t.Fatalf("remove exit = %d", code)
	}
	envelope = decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "account_not_found" {
		t.Fatalf("unexpected remove error: %+v", envelope.Error)
	}
}

func TestRegistryErrorSanitized(t *testing.T) {
	env := newTestEnv(t, false)
	if err := os.MkdirAll(env.storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(env.storeDir, "registry.json")
	if err := os.WriteFile(registryPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"list", "--json"}); code != 1 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "registry_error" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if strings.Contains(envelope.Error.Message, env.storeDir) || strings.Contains(envelope.Error.Message, "json:") {
		t.Errorf("error leaked internal detail: %q", envelope.Error.Message)
	}
	if envelope.Error.Hint == "" {
		t.Errorf("missing hint: %+v", envelope.Error)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestImportErrorCode(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	if code := app.Run([]string{"import", filepath.Join(env.dir, "missing.json"), "--json"}); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "import_failed" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if envelope.Error.Hint == "" {
		t.Errorf("missing hint: %+v", envelope.Error)
	}
}

func TestHumanErrorHint(t *testing.T) {
	env := newTestEnv(t, false)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"current"}); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "error: no agy credentials") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "hint:") {
		t.Errorf("stderr missing hint: %q", errOut.String())
	}
}

func TestJSONRemoveSuccess(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	app, out, errOut := env.app("")
	if code := app.Run([]string{"remove", "--all", "--yes", "--logout", "--json"}); code != 0 {
		t.Fatalf("remove exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Command != "remove" {
		t.Fatalf("command = %q", envelope.Command)
	}
	var data struct {
		Removed   []registry.Account `json:"removed"`
		LoggedOut bool               `json:"logged_out"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Removed) != 1 || !data.LoggedOut {
		t.Errorf("unexpected remove data: %+v", data)
	}
	if strings.Contains(out.String(), "Removed the live agy credential.") {
		t.Errorf("human output leaked into JSON: %q", out.String())
	}
}

func TestConfirmationRequired(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	app, out, _ := env.app("")
	if code := app.Run([]string{"remove", "1", "--json"}); code != 1 {
		t.Fatalf("remove exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "confirmation_required" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
}

func TestConfirmationDeclinedJSON(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	app, out, _ := env.app("n\n")
	app.TTY = true
	if code := app.Run([]string{"remove", "1", "--json"}); code != 1 {
		t.Fatalf("remove exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "confirmation_declined" {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
	if strings.Contains(out.String(), "Cancelled.") {
		t.Errorf("human output leaked into JSON: %q", out.String())
	}
}

func TestReorderArgs(t *testing.T) {
	fs := newTestFlagSet()
	got := reorderArgs(fs, []string{"file.json", "--alias", "work"})
	want := []string{"--alias", "work", "file.json"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("reorderArgs = %v, want %v", got, want)
	}

	got = reorderArgs(fs, []string{"--yes", "file.json"})
	if strings.Join(got, "|") != "--yes|file.json" {
		t.Errorf("reorderArgs = %v", got)
	}

	got = reorderArgs(fs, []string{"file.json", "--", "--alias"})
	if strings.Join(got, "|") != "file.json|--|--alias" {
		t.Errorf("reorderArgs = %v", got)
	}
}

func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("alias", "", "")
	fs.Bool("yes", false, "")
	return fs
}

type testEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	Command       string          `json:"command"`
	Data          json.RawMessage `json:"data"`
	Error         *testErrorDoc   `json:"error"`
}

type testErrorDoc struct {
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	Hint       string          `json:"hint"`
	Candidates []testCandidate `json:"candidates"`
}

type testCandidate struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Alias string `json:"alias"`
}

func decodeTestEnvelope(t *testing.T, out *bytes.Buffer) testEnvelope {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var envelope testEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v; output = %q", err, out.String())
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout must contain exactly one JSON document, extra decode = %v; output = %q", err, out.String())
	}
	return envelope
}

func setTestOAuthClient(t *testing.T) {
	t.Helper()
	oldID, oldSecret := oauth.ClientID, oauth.ClientSecret
	oauth.ClientID, oauth.ClientSecret = "test-client", "test-secret"
	t.Cleanup(func() {
		oauth.ClientID, oauth.ClientSecret = oldID, oldSecret
	})
}

func TestLoginOAuthFlow(t *testing.T) {
	setTestOAuthClient(t)
	env := newTestEnv(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "new-access",
			"expires_in":    3600,
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"id_token":      makeCLIIDToken(t, "login@example.com", "subject-login"),
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

	app, out, errOut := env.app("the-code\n")
	if code := app.Run([]string{"login", "--no-browser", "--alias", "work"}); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "Added account work (login@example.com)") {
		t.Errorf("login output = %q", out.String())
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 || reg.Accounts[0].Email != "login@example.com" || reg.Accounts[0].Alias != "work" {
		t.Errorf("unexpected registry: %+v", reg.Accounts)
	}
	raw, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	tokenFile, err := agy.ParseTokenFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tokenFile.Token.RefreshToken != "new-refresh" {
		t.Errorf("refresh token = %q", tokenFile.Token.RefreshToken)
	}
}

func TestLoginReusesStoredRefreshToken(t *testing.T) {
	setTestOAuthClient(t)
	env := newTestEnv(t, true)
	app, _, _ := env.app("")
	if code := app.Run([]string{"add", "--alias", "personal"}); code != 0 {
		t.Fatalf("add exit = %d", code)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
			"token_type":   "Bearer",
			"id_token":     makeCLIIDToken(t, "user@example.com", "subject-1"),
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

	app, _, errOut := env.app("the-code\n")
	if code := app.Run([]string{"login", "--no-browser"}); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, errOut)
	}
	raw, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	tokenFile, err := agy.ParseTokenFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tokenFile.Token.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want stored refresh-1", tokenFile.Token.RefreshToken)
	}
	if tokenFile.Token.AccessToken != "new-access" {
		t.Errorf("access token = %q", tokenFile.Token.AccessToken)
	}
}

func TestListActiveWithoutLiveCredential(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, _ := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d", code)
	}
	if err := os.Remove(env.tokenPath); err != nil {
		t.Fatal(err)
	}
	app, out, errOut := env.app("")
	if code := app.Run([]string{"list", "--active"}); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "No active account") {
		t.Errorf("output = %q", out.String())
	}
}

func TestEveryCommandHasHelp(t *testing.T) {
	env := newTestEnv(t, false)
	commands := []string{
		"list", "add", "login", "switch", "remove", "alias",
		"status", "current", "import", "export", "path", "clean", "version",
	}
	for _, command := range commands {
		app, out, errOut := env.app("")
		if code := app.Run([]string{command, "--help"}); code != 0 {
			t.Errorf("%s --help exit = %d, stderr = %s", command, code, errOut)
			continue
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%s --help output does not contain Usage: %q", command, out.String())
		}
	}

	aliases := []string{"ls", "use", "rm", "paths"}
	for _, alias := range aliases {
		app, out, errOut := env.app("")
		if code := app.Run([]string{alias, "--help"}); code != 0 {
			t.Errorf("%s --help exit = %d, stderr = %s", alias, code, errOut)
			continue
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%s --help output does not contain Usage: %q", alias, out.String())
		}
	}
}
