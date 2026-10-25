package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/oauth"
	"github.com/fikrilal/agy-auth/internal/registry"
	"github.com/fikrilal/agy-auth/internal/usage"
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
	store := registry.NewStore(e.storeDir)
	return &App{
		In:      strings.NewReader(input),
		Out:     out,
		Err:     errOut,
		Store:   store,
		Storage: &agy.Storage{Service: "test", User: "test", FilePath: e.tokenPath, NoKeyring: true, Backup: store.Backups},
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
	app.Usage = &testUsageProvider{snapshot: usageSnapshot()}
	if code := app.Run([]string{"switch", "example.com"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	for _, want := range []string{"Select an account to activate", "PLAN", "WEEKLY RESET", "Gemini Models", "01", "02", "Switched to second"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("picker output missing %q: %q", want, out.String())
		}
	}
}

func TestSwitchNoQueryPickerUsage(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	app, out, errOut := env.app("2\n")
	app.TTY = true
	app.Usage = &testUsageProvider{snapshot: usageSnapshot()}
	if code := app.Run([]string{"switch"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	for _, want := range []string{"PLAN", "5H", "WEEKLY", "GROUP", "WEEKLY RESET", "Gemini Models", "Switched to second"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("switch picker output missing %q: %q", want, out.String())
		}
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

func TestListJSONCanonicalOrder(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "aaa@example.com", "subject-aaa", "refresh-aaa", "aaa")

	listEmails := func() []accountRow {
		t.Helper()
		app, out, errOut := env.app("")
		if code := app.Run([]string{"list", "--json"}); code != 0 {
			t.Fatalf("list exit = %d, stderr = %s", code, errOut)
		}
		envelope := decodeTestEnvelope(t, out)
		var payload struct {
			Accounts []accountRow `json:"accounts"`
		}
		if err := json.Unmarshal(envelope.Data, &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Accounts
	}

	accounts := listEmails()
	if len(accounts) != 2 {
		t.Fatalf("accounts = %d", len(accounts))
	}
	if accounts[0].Email != "user@example.com" || accounts[0].Number != 1 {
		t.Errorf("first row = %+v", accounts[0])
	}
	if accounts[1].Email != "aaa@example.com" || accounts[1].Number != 2 {
		t.Errorf("second row = %+v", accounts[1])
	}

	app, _, errOut = env.app("")
	if code := app.Run([]string{"switch", "aaa", "--json"}); code != 0 {
		t.Fatalf("switch exit = %d, stderr = %s", code, errOut)
	}
	accounts = listEmails()
	if accounts[0].Email != "aaa@example.com" || accounts[0].Number != 1 || !accounts[0].Active {
		t.Errorf("active first row = %+v", accounts[0])
	}
	if accounts[1].Email != "user@example.com" || accounts[1].Number != 2 {
		t.Errorf("active second row = %+v", accounts[1])
	}
}

func TestRenderAccountTableNarrow(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 34
	future := testNow.Add(2 * time.Hour)
	past := testNow.Add(-24 * time.Hour)
	rows := []accountRow{
		{Number: 1, ID: "acct_1", Active: true, Stored: true, Email: "very.long.account.name@example.com", Alias: "long-alias", AuthMethod: "consumer", ExpiresAt: &future, LastUsedAt: &past},
		{Number: 2, ID: "acct_2", Stored: true, Email: "second@example.com", AuthMethod: "consumer", ExpiresAt: &future, LastUsedAt: &past},
	}
	app.renderAccountRows(rows, testNow)
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if visibleWidth(line) > app.Width {
			t.Errorf("line exceeds width: %q (%d)", line, visibleWidth(line))
		}
	}
	if !strings.Contains(out.String(), "…") {
		t.Errorf("expected truncated account: %q", out.String())
	}
	for _, unwanted := range []string{"EXPIRES", "METHOD", "LAST"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("account table kept status column %q: %q", unwanted, out.String())
		}
	}
}

func TestRenderAccountTableWide(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 140
	snapshot := usageSnapshot()
	rows := []accountRow{
		{Number: 1, ID: "acct_1", Active: true, Stored: true, Email: "very.long.account.name@example.com", Alias: "long-alias", Plan: "Pro", Usage: &snapshot},
	}
	app.renderAccountRows(rows, testNow)
	for _, want := range []string{"PLAN", "5H", "WEEKLY", "GROUP", "5H RESET", "WEEKLY RESET", "Gemini Models", "very.long.account.name@example.com"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("wide table missing %q: %q", want, out.String())
		}
	}
	for _, unwanted := range []string{"EXPIRES", "METHOD", "LAST"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("wide table kept status column %q: %q", unwanted, out.String())
		}
	}
}

func TestRenderAccountTableGroupsSameEmail(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 100
	rows := []accountRow{
		{Number: 1, ID: "acct_1", Active: true, Stored: true, Email: "same@example.com", Alias: "one"},
		{Number: 2, ID: "acct_2", Stored: true, Email: "same@example.com", Alias: "two"},
	}
	app.renderAccountRows(rows, testNow)
	if !strings.Contains(out.String(), "same@example.com") {
		t.Errorf("missing group header: %q", out.String())
	}
	for _, want := range []string{"  one", "  two", "01", "02"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("grouped table missing %q: %q", want, out.String())
		}
	}
}

func TestRenderAccountTableInteractiveHighlight(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 100
	app.OutTTY = true
	rows := []accountRow{
		{Number: 1, ID: "acct_1", Active: true, Stored: true, Email: "first@example.com", Alias: "one"},
		{Number: 2, ID: "acct_2", Stored: true, Email: "second@example.com", Alias: "two"},
	}

	app.renderAccountTable(rows, testNow, 0, "\n")
	output := out.String()

	if !strings.Contains(output, "\x1b[32m>\x1b[0m") {
		t.Errorf("expected green cursor prefix in output: %q", output)
	}
	if !strings.Contains(output, "\x1b[36m") {
		t.Errorf("expected cyan account styling in output: %q", output)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d: %q", len(lines), output)
	}
	if !strings.HasPrefix(lines[0], "   #") {
		t.Errorf("header should have padding prefix: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "\x1b[32m>\x1b[0m \x1b[1m01\x1b[0m") {
		t.Errorf("selected row should have cursor prefix: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "  02") {
		t.Errorf("unselected row should have padding prefix: %q", lines[2])
	}

	out.Reset()
	app.NoColor = true
	app.renderAccountTable(rows, testNow, 1, "\n")
	noColorOutput := out.String()
	if strings.Contains(noColorOutput, "\x1b[") {
		t.Errorf("expected no ANSI escape sequences with NoColor: %q", noColorOutput)
	}
	noColorLines := strings.Split(strings.TrimRight(noColorOutput, "\n"), "\n")
	if !strings.HasPrefix(noColorLines[1], "  01") {
		t.Errorf("unselected row 1 should have padding: %q", noColorLines[1])
	}
	if !strings.HasPrefix(noColorLines[2], "> 02") {
		t.Errorf("selected row 2 should have plain cursor prefix: %q", noColorLines[2])
	}
}

func makeLiveRows(count int) []accountRow {
	rows := make([]accountRow, count)
	for i := range rows {
		rows[i] = accountRow{
			Number:     i + 1,
			ID:         fmt.Sprintf("acct_%d", i+1),
			Stored:     true,
			Email:      fmt.Sprintf("user%d@example.com", i+1),
			Alias:      fmt.Sprintf("alias%d", i+1),
			AuthMethod: "consumer",
		}
	}
	return rows
}

func TestLiveRequiresTTY(t *testing.T) {
	env := newTestEnv(t, false)
	for _, args := range [][]string{
		{"list", "--live"},
		{"switch", "--live"},
		{"remove", "--live"},
	} {
		app, _, errOut := env.app("")
		if code := app.Run(args); code != 2 {
			t.Errorf("%v exit = %d", args, code)
		}
		if !strings.Contains(errOut.String(), "requires an interactive terminal") {
			t.Errorf("%v stderr = %q", args, errOut.String())
		}
	}

	app, out, _ := env.app("")
	if code := app.Run([]string{"list", "--live", "--json"}); code != 2 {
		t.Fatalf("json live exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "usage" || !strings.Contains(envelope.Error.Message, "cannot be used with --json") {
		t.Errorf("unexpected error: %+v", envelope.Error)
	}
}

func TestLiveViewport(t *testing.T) {
	state := liveState{rows: makeLiveRows(10), selectedID: "acct_10"}
	start, end := state.viewport(10)
	if start != 6 || end != 10 {
		t.Errorf("viewport = [%d,%d), want [6,10)", start, end)
	}
	state.moveTo(0)
	start, end = state.viewport(10)
	if start != 0 || end != 4 {
		t.Errorf("home viewport = [%d,%d), want [0,4)", start, end)
	}
	state.selectedID = "acct_5"
	start, end = state.viewport(10)
	if start > 4 || end <= 4 {
		t.Errorf("selected row not visible: [%d,%d)", start, end)
	}
}

func TestLiveStatusLine(t *testing.T) {
	state := liveState{source: "keyring", nextRefresh: testNow.Add(42 * time.Second)}
	if line := state.statusLine(testNow); line != "Live refresh: keyring | Refresh in 42s" {
		t.Errorf("status = %q", line)
	}
	state.refreshing = true
	if line := state.statusLine(testNow); !strings.Contains(line, "Refresh running") {
		t.Errorf("running status = %q", line)
	}
	state.refreshing = false
	state.refreshErr = "refresh failed"
	if line := state.statusLine(testNow); !strings.Contains(line, "Error: refresh failed") {
		t.Errorf("error status = %q", line)
	}
}

func TestRenderLiveFrame(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 48
	app.Height = 10
	state := liveState{
		rows:        makeLiveRows(10),
		selectedID:  "acct_10",
		checked:     map[string]bool{"acct_10": true},
		action:      "Removed 1 account(s).",
		source:      "keyring",
		nextRefresh: testNow.Add(42 * time.Second),
		interval:    time.Minute,
	}
	app.renderLiveFrame(&state, liveModeRemove)
	text := out.String()
	for _, want := range []string{"Select accounts to remove", "[x]", "Live refresh: keyring | Refresh in 42s", "Removed 1 account(s).", "space toggle"} {
		if !strings.Contains(text, want) {
			t.Errorf("frame missing %q: %q", want, text)
		}
	}
	if !strings.Contains(text, "07") || strings.Contains(text, "01 ") {
		t.Errorf("viewport did not start at the selected row: %q", text)
	}
	for _, line := range strings.Split(text, "\r\n") {
		if visibleWidth(line) > app.Width {
			t.Errorf("line exceeds width: %q (%d)", line, visibleWidth(line))
		}
	}
}

func TestLiveNavigationAndToggle(t *testing.T) {
	env := newTestEnv(t, false)
	app, _, _ := env.app("")
	state := liveState{rows: makeLiveRows(5), selectedID: "acct_1", checked: map[string]bool{}}
	if quit, refresh := app.handleLiveKey(&state, keyEvent{kind: keyRune, r: 'j'}, liveModeRemove, false); quit || refresh {
		t.Fatalf("j = quit %v refresh %v", quit, refresh)
	}
	if state.selectedID != "acct_2" {
		t.Errorf("selected = %q", state.selectedID)
	}
	app.handleLiveKey(&state, keyEvent{kind: keySpace}, liveModeRemove, false)
	if !state.checked["acct_2"] {
		t.Errorf("checked = %+v", state.checked)
	}
	app.handleLiveKey(&state, keyEvent{kind: keyRune, r: 'G'}, liveModeRemove, false)
	if state.selectedID != "acct_5" {
		t.Errorf("end selected = %q", state.selectedID)
	}
	app.handleLiveKey(&state, keyEvent{kind: keyRune, r: 'g'}, liveModeRemove, false)
	if state.selectedID != "acct_1" {
		t.Errorf("home selected = %q", state.selectedID)
	}
	if quit, _ := app.handleLiveKey(&state, keyEvent{kind: keyRune, r: 'q'}, liveModeRemove, false); !quit {
		t.Error("q did not quit")
	}
}

func TestLiveTypeahead(t *testing.T) {
	env := newTestEnv(t, false)
	app, _, _ := env.app("")
	state := liveState{rows: makeLiveRows(12), selectedID: "acct_1"}
	app.handleLiveKey(&state, keyEvent{kind: keyRune, r: '1'}, liveModeRemove, false)
	if state.selectedID != "acct_1" {
		t.Errorf("first digit selected = %q", state.selectedID)
	}
	app.handleLiveKey(&state, keyEvent{kind: keyRune, r: '2'}, liveModeRemove, false)
	if state.selectedID != "acct_12" {
		t.Errorf("two-digit selected = %q", state.selectedID)
	}
	state.digits = "1"
	state.digitAt = testNow.Add(-2 * time.Second)
	app.handleLiveKey(&state, keyEvent{kind: keyRune, r: '2'}, liveModeRemove, false)
	if state.selectedID != "acct_2" {
		t.Errorf("expired typeahead selected = %q", state.selectedID)
	}
}

func TestLiveSilentActions(t *testing.T) {
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
	first := reg.Resolve("first")
	second := reg.Resolve("second")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("resolution first=%v second=%v", first, second)
	}

	app, _, _ = env.app("")
	action := app.liveSwitch(second[0].ID, second[0].DisplayName())
	if !strings.Contains(action, "Switched to") {
		t.Errorf("switch action = %q", action)
	}
	reg, err = registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != second[0].ID {
		t.Errorf("active = %q, want %q", reg.Active, second[0].ID)
	}

	app, _, _ = env.app("")
	action = app.liveRemove([]string{first[0].ID}, false)
	if !strings.Contains(action, "Removed 1") {
		t.Errorf("remove action = %q", action)
	}
	reg, err = registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 || reg.FindByID(first[0].ID) != nil {
		t.Errorf("registry after remove = %+v", reg.Accounts)
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
	if code := app.resolveFailureWithNumbers("same@example.com", matches, map[string]int{"acct_1": 3, "acct_2": 5}); code != 1 {
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
	if envelope.Error.Candidates[0].Number != 3 || envelope.Error.Candidates[1].Number != 5 {
		t.Errorf("candidate numbers = %+v", envelope.Error.Candidates)
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
	Number int    `json:"number"`
	ID     string `json:"id"`
	Email  string `json:"email"`
	Alias  string `json:"alias"`
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

func fakeAgyLogin(t *testing.T, token []byte, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agy")
	script := "#!/bin/sh\n"
	if token != nil {
		script += "mkdir -p \"$HOME/.gemini/antigravity-cli\"\n"
		script += "cat > \"$HOME/.gemini/antigravity-cli/antigravity-oauth-token\" <<'TOKEN'\n"
		script += string(token) + "\nTOKEN\n"
	}
	script += fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func loginTestApp(t *testing.T, env *testEnv) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	app, out, errOut := env.app("")
	app.TTY = true
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		stdout.Close()
		stderr.Close()
	})
	app.StdinFile = stdin
	app.StdoutFile = stdout
	app.StderrFile = stderr
	return app, out, errOut
}

func TestLoginViaAgyImportsIntoRealStorage(t *testing.T) {
	env := newTestEnv(t, true)
	script := fakeAgyLogin(t, makeCLIToken(t, "second@example.com", "subject-2", "refresh-2"), 0)
	app, out, errOut := loginTestApp(t, env)
	if code := app.Run([]string{"login", "--via-agy", "--agy", script, "--alias", "second", "--json"}); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Account registry.Account `json:"account"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Account.Email != "second@example.com" || payload.Account.Alias != "second" {
		t.Errorf("account = %+v", payload.Account)
	}

	raw, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := agy.ParseTokenFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if live.Email() != "second@example.com" {
		t.Errorf("live credential = %q", live.Email())
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 2 || reg.Active != payload.Account.ID {
		t.Errorf("registry = %+v active=%q", reg.Accounts, reg.Active)
	}
}

func TestLoginViaAgyFailureLeavesActiveUntouched(t *testing.T) {
	env := newTestEnv(t, true)
	before, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		code int
	}{
		{"cancel", 0},
		{"error", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := fakeAgyLogin(t, nil, test.code)
			app, out, _ := loginTestApp(t, env)
			if code := app.Run([]string{"login", "--via-agy", "--agy", script, "--json"}); code != 1 {
				t.Fatalf("login exit = %d", code)
			}
			envelope := decodeTestEnvelope(t, out)
			if envelope.Error == nil || envelope.Error.Code != "credential_not_found" {
				t.Fatalf("error = %+v", envelope.Error)
			}
			after, err := os.ReadFile(env.tokenPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("active credential changed on failed login")
			}
		})
	}
}

func TestLoginViaAgyInvalidTokenLeavesActiveUntouched(t *testing.T) {
	env := newTestEnv(t, true)
	before, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	script := fakeAgyLogin(t, []byte(`{"token":{}}`), 0)
	app, out, _ := loginTestApp(t, env)
	if code := app.Run([]string{"login", "--via-agy", "--agy", script, "--json"}); code != 1 {
		t.Fatalf("login exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "credential_error" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	after, err := os.ReadFile(env.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("active credential changed on invalid login")
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

type testUsageProvider struct {
	mu       sync.Mutex
	snapshot usage.Snapshot
	err      error
	calls    int
}

func (p *testUsageProvider) Fetch(context.Context, string, *agy.TokenFile) (usage.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.snapshot, p.err
}

func (p *testUsageProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func usageSnapshot() usage.Snapshot {
	fiveReset := testNow.Add(2 * time.Hour)
	weeklyReset := testNow.Add(6 * 24 * time.Hour)
	return usage.Snapshot{
		Plan:      "Pro",
		FiveHour:  &usage.Window{RemainingPercent: 75, ResetsAt: &fiveReset, Group: "Claude and GPT models"},
		Weekly:    &usage.Window{RemainingPercent: 50, ResetsAt: &weeklyReset, Group: "Gemini Models"},
		UpdatedAt: testNow,
	}
}

func TestListUsageColumnsAndJSON(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "personal"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	app, out, errOut := env.app("")
	app.Width = 120
	app.Usage = &testUsageProvider{snapshot: usageSnapshot()}
	if code := app.Run([]string{"list", "--no-sync"}); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, errOut)
	}
	for _, want := range []string{"PLAN", "5H", "WEEKLY", "GROUP", "Gemini Models", "Pro", "75%", "50%"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list output missing %q: %q", want, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q", errOut.String())
	}

	app, out, errOut = env.app("")
	app.Usage = &testUsageProvider{snapshot: usageSnapshot()}
	if code := app.Run([]string{"list", "--no-sync", "--json"}); code != 0 {
		t.Fatalf("list json exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Accounts []accountRow `json:"accounts"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(payload.Accounts))
	}
	row := payload.Accounts[0]
	if row.Plan != "Pro" || row.Usage == nil || row.Usage.FiveHour == nil || row.Usage.Weekly == nil {
		t.Fatalf("usage row = %+v", row)
	}
	if row.Usage.FiveHour.RemainingPercent != 75 || row.Usage.Weekly.RemainingPercent != 50 {
		t.Errorf("usage = %+v", row.Usage)
	}
	if row.Usage.Weekly.Group != "Gemini Models" || row.Usage.LimitGroup() != "Gemini Models" {
		t.Errorf("usage group = %+v", row.Usage)
	}
	if row.UsageError != "" {
		t.Errorf("usage error = %q", row.UsageError)
	}
}

func TestListNoUsageSkipsProvider(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	provider := &testUsageProvider{snapshot: usageSnapshot()}
	app, out, errOut := env.app("")
	app.Width = 120
	app.Usage = provider
	if code := app.Run([]string{"list", "--no-sync", "--no-usage"}); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, errOut)
	}
	if provider.callCount() != 0 {
		t.Errorf("provider calls = %d", provider.callCount())
	}
	if strings.Contains(out.String(), "PLAN") {
		t.Errorf("no-usage output has plan column: %q", out.String())
	}
}

func TestListUsageFailureIsNonFatal(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	snapshot := usage.Snapshot{Plan: "Pro"}
	app, out, errOut := env.app("")
	app.Usage = &testUsageProvider{snapshot: snapshot, err: usage.ErrForbidden}
	if code := app.Run([]string{"list", "--no-sync"}); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "PLAN") || !strings.Contains(out.String(), "Pro") {
		t.Errorf("list output = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "quota lookup failed") || !strings.Contains(errOut.String(), "forbidden") {
		t.Errorf("stderr = %q", errOut.String())
	}

	app, out, _ = env.app("")
	app.Usage = &testUsageProvider{snapshot: snapshot, err: usage.ErrForbidden}
	if code := app.Run([]string{"list", "--no-sync", "--json"}); code != 0 {
		t.Fatalf("list json exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Accounts []accountRow `json:"accounts"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].UsageError != "forbidden" {
		t.Errorf("accounts = %+v", payload.Accounts)
	}
}

func TestRenderAccountTableUsageWidth(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.Width = 80
	snapshot := usageSnapshot()
	rows := []accountRow{
		{Number: 1, ID: "acct_1", Active: true, Stored: true, Email: "user@example.com", Plan: "Pro", Usage: &snapshot},
	}
	app.renderAccountRows(rows, testNow)
	for _, want := range []string{"PLAN", "5H", "WEEKLY", "GROUP", "Gemini Models", "75%", "50%"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table missing %q: %q", want, out.String())
		}
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if visibleWidth(line) > app.Width {
			t.Errorf("line exceeds width: %q (%d)", line, visibleWidth(line))
		}
	}

	out.Reset()
	app.Width = 40
	app.renderAccountRows(rows, testNow)
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if visibleWidth(line) > app.Width {
			t.Errorf("narrow line exceeds width: %q (%d)", line, visibleWidth(line))
		}
	}
}

func TestAutoSwitchCandidate(t *testing.T) {
	fiveResetSoon := testNow.Add(time.Hour)
	fiveResetLater := testNow.Add(2 * time.Hour)
	weeklyReset := testNow.Add(6 * 24 * time.Hour)
	active := accountRow{
		ID:     "acct_active",
		Active: true,
		Usage: &usage.Snapshot{
			FiveHour: &usage.Window{RemainingPercent: 0, ResetsAt: &fiveResetSoon},
			Weekly:   &usage.Window{RemainingPercent: 80, ResetsAt: &weeklyReset},
		},
	}
	later := accountRow{
		ID: "acct_later",
		Usage: &usage.Snapshot{
			FiveHour: &usage.Window{RemainingPercent: 100, ResetsAt: &fiveResetLater},
			Weekly:   &usage.Window{RemainingPercent: 100, ResetsAt: &weeklyReset},
		},
	}
	sooner := accountRow{
		ID: "acct_sooner",
		Usage: &usage.Snapshot{
			FiveHour: &usage.Window{RemainingPercent: 90, ResetsAt: &fiveResetSoon},
			Weekly:   &usage.Window{RemainingPercent: 90, ResetsAt: &weeklyReset},
		},
	}
	candidate := autoSwitchCandidate([]accountRow{active, later, sooner})
	if candidate == nil || candidate.ID != "acct_sooner" {
		t.Fatalf("candidate = %+v", candidate)
	}

	active.Usage.FiveHour.RemainingPercent = 50
	if candidate := autoSwitchCandidate([]accountRow{active, later, sooner}); candidate != nil {
		t.Errorf("unexpected candidate for healthy active: %+v", candidate)
	}
	active.Usage.FiveHour.RemainingPercent = 0
	active.Usage.Stale = true
	if candidate := autoSwitchCandidate([]accountRow{active, later, sooner}); candidate != nil {
		t.Errorf("unexpected candidate for stale active: %+v", candidate)
	}
	active.Usage.Stale = false
	sooner.Usage.Weekly.RemainingPercent = 0
	if candidate := autoSwitchCandidate([]accountRow{active, later, sooner}); candidate == nil || candidate.ID != "acct_later" {
		t.Errorf("exhausted candidate was selected: %+v", candidate)
	}
}

func TestAutoSwitchCandidateUsesModelDetail(t *testing.T) {
	reset := testNow.Add(time.Hour)
	active := accountRow{
		ID:     "acct_active",
		Active: true,
		Usage:  &usage.Snapshot{FiveHour: &usage.Window{RemainingPercent: 100}, Weekly: &usage.Window{RemainingPercent: 100}},
		detail: &usage.Detail{Buckets: []usage.Model{{ModelID: "gemini-2.5-pro", RemainingPercent: 0, ResetsAt: &reset}}},
	}
	candidate := accountRow{
		ID:     "acct_candidate",
		Usage:  &usage.Snapshot{FiveHour: &usage.Window{RemainingPercent: 100}, Weekly: &usage.Window{RemainingPercent: 100}},
		detail: &usage.Detail{Buckets: []usage.Model{{ModelID: "gemini-2.5-pro", RemainingPercent: 90, ResetsAt: &reset}}},
	}
	if got := autoSwitchCandidate([]accountRow{active, candidate}); got == nil || got.ID != "acct_candidate" {
		t.Errorf("candidate = %+v", got)
	}
	active.detail.Stale = true
	if got := autoSwitchCandidate([]accountRow{active, candidate}); got != nil {
		t.Errorf("stale detail should be ignored: %+v", got)
	}
}

func TestAutoSwitchCandidatePrefersNamedGroups(t *testing.T) {
	reset := testNow.Add(time.Hour)
	healthy := usage.Groups{Groups: []usage.Group{
		{Name: "Gemini Models", Buckets: []usage.GroupBucket{
			{Window: "weekly", RemainingPercent: 58, RemainingFraction: 0.58, ResetTime: reset.Format(time.RFC3339)},
			{Window: "5h", RemainingPercent: 100, RemainingFraction: 1, ResetTime: reset.Format(time.RFC3339)},
		}},
		{Name: "Claude and GPT models", Buckets: []usage.GroupBucket{
			{Window: "weekly", RemainingPercent: 100, RemainingFraction: 1, ResetTime: reset.Format(time.RFC3339)},
			{Window: "5h", RemainingPercent: 100, RemainingFraction: 1, ResetTime: reset.Format(time.RFC3339)},
		}},
	}}
	exhausted := usage.Groups{Groups: []usage.Group{
		{Name: "Gemini Models", Buckets: []usage.GroupBucket{
			{Window: "weekly", RemainingPercent: 0, RemainingFraction: 0, ResetTime: reset.Format(time.RFC3339)},
			{Window: "5h", RemainingPercent: 100, RemainingFraction: 1, ResetTime: reset.Format(time.RFC3339)},
		}},
	}}
	active := accountRow{
		ID:     "acct_active",
		Active: true,
		groups: &healthy,
		Usage:  &usage.Snapshot{FiveHour: &usage.Window{RemainingPercent: 0}},
		detail: &usage.Detail{Buckets: []usage.Model{{ModelID: "old-detail", RemainingPercent: 0}}},
	}
	candidate := accountRow{ID: "acct_candidate", groups: &healthy}
	if got := autoSwitchCandidate([]accountRow{active, candidate}); got != nil {
		t.Errorf("named groups should override stale model detail: %+v", got)
	}

	active.groups = &exhausted
	if got := autoSwitchCandidate([]accountRow{active, candidate}); got == nil || got.ID != "acct_candidate" {
		t.Errorf("exhausted named group did not trigger switch: %+v", got)
	}

	candidate.groups = &exhausted
	if got := autoSwitchCandidate([]accountRow{active, candidate}); got != nil {
		t.Errorf("exhausted candidate should be skipped: %+v", got)
	}
}

func TestMaybeAutoSwitch(t *testing.T) {
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
	first := reg.Resolve("first")
	second := reg.Resolve("second")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("resolution first=%v second=%v", first, second)
	}
	reset := testNow.Add(time.Hour)
	state := &liveState{rows: []accountRow{
		{ID: first[0].ID, Active: true, Usage: &usage.Snapshot{
			FiveHour: &usage.Window{RemainingPercent: 0, ResetsAt: &reset},
			Weekly:   &usage.Window{RemainingPercent: 50, ResetsAt: &reset},
		}},
		{ID: second[0].ID, Usage: &usage.Snapshot{
			FiveHour: &usage.Window{RemainingPercent: 100, ResetsAt: &reset},
			Weekly:   &usage.Window{RemainingPercent: 100, ResetsAt: &reset},
		}},
	}}
	action := app.maybeAutoSwitch(state, liveModeSwitch)
	if !strings.Contains(action, "Auto-switched to") {
		t.Fatalf("action = %q", action)
	}
	if !state.autoSwitched || state.activeRow() == nil || state.activeRow().ID != second[0].ID {
		t.Errorf("state after auto-switch = %+v", state)
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Active != second[0].ID {
		t.Errorf("registry active = %q, want %q", reg.Active, second[0].ID)
	}
	if action := app.maybeAutoSwitch(state, liveModeSwitch); action != "" {
		t.Errorf("second auto-switch = %q", action)
	}
}

type testGroupsProvider struct {
	mu     sync.Mutex
	groups usage.Groups
	err    error
	calls  int
}

func (p *testGroupsProvider) FetchGroups(context.Context, *agy.TokenFile) (usage.Groups, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.groups, p.err
}

func (p *testGroupsProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func usageGroups() usage.Groups {
	return usage.Groups{
		Description: "Within each group, models share a weekly limit and a 5-hour limit.",
		Groups: []usage.Group{
			{
				Name:        "Gemini Models",
				Description: "Models within this group: Gemini Flash, Gemini Pro",
				Buckets: []usage.GroupBucket{
					{ID: "gemini-weekly", Window: "weekly", RemainingFraction: 0.58, RemainingPercent: 58, ResetTime: "2026-10-24T02:39:46Z"},
					{ID: "gemini-5h", Window: "5h", RemainingFraction: 1, RemainingPercent: 100, ResetTime: "2026-10-22T22:32:29Z"},
				},
			},
			{
				Name:        "Claude and GPT models",
				Description: "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
				Buckets: []usage.GroupBucket{
					{ID: "3p-weekly", Window: "weekly", RemainingFraction: 1, RemainingPercent: 100, ResetTime: "2026-10-29T17:32:29Z"},
					{ID: "3p-5h", Window: "5h", RemainingFraction: 1, RemainingPercent: 100, ResetTime: "2026-10-22T22:32:29Z"},
				},
			},
		},
		UpdatedAt: testNow,
	}
}

func TestUsageCommandAll(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	addSecondAccount(t, env, "second@example.com", "subject-2", "refresh-2", "second")

	provider := &testGroupsProvider{groups: usageGroups()}
	app, out, errOut := env.app("")
	app.Width = 140
	app.UsageGroups = provider
	if code := app.Run([]string{"usage", "--all"}); code != 0 {
		t.Fatalf("usage --all exit = %d, stderr = %s", code, errOut)
	}
	for _, want := range []string{"ACCOUNT", "GROUP", "5H", "WEEKLY", "Gemini Models", "Claude and GPT models", "58%", "first", "second"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage --all output missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "LOWEST") || strings.Contains(out.String(), "MODEL") {
		t.Errorf("usage --all still has the old model columns: %q", out.String())
	}
	if strings.Count(out.String(), "Gemini Models") != 2 {
		t.Errorf("gemini group rows = %d", strings.Count(out.String(), "Gemini Models"))
	}
	if provider.callCount() != 2 {
		t.Errorf("provider calls = %d", provider.callCount())
	}
	if !strings.Contains(out.String(), "\n\n") {
		t.Errorf("usage --all has no blank line between accounts: %q", out.String())
	}

	provider = &testGroupsProvider{groups: usageGroups()}
	app, out, errOut = env.app("")
	app.UsageGroups = provider
	if code := app.Run([]string{"usage", "--all", "--json"}); code != 0 {
		t.Fatalf("usage --all json exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Accounts []struct {
			Email      string        `json:"email"`
			Groups     []usage.Group `json:"groups"`
			UsageError string        `json:"usage_error"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 2 {
		t.Fatalf("accounts = %+v", payload.Accounts)
	}
	first := payload.Accounts[0]
	if first.Email != "user@example.com" || len(first.Groups) != 2 || first.UsageError != "" {
		t.Errorf("first account = %+v", first)
	}
	if first.Groups[0].Name != "Gemini Models" || len(first.Groups[0].Buckets) != 2 || first.Groups[0].Buckets[0].RemainingPercent != 58 {
		t.Errorf("first groups = %+v", first.Groups)
	}
}

func TestUsageCommandAllValidation(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.UsageGroups = &testGroupsProvider{groups: usageGroups()}
	if code := app.Run([]string{"usage", "--all", "--json"}); code != 1 {
		t.Fatalf("usage --all exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "account_not_found" {
		t.Errorf("error = %+v", envelope.Error)
	}

	app, out, _ = env.app("")
	if code := app.Run([]string{"usage", "work", "--all", "--json"}); code != 2 {
		t.Fatalf("usage --all query exit = %d", code)
	}
	envelope = decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "usage" {
		t.Errorf("error = %+v", envelope.Error)
	}
}

func TestUsageCommandAllFetchFailure(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	app, _, errOut = env.app("")
	app.UsageGroups = &testGroupsProvider{err: usage.ErrForbidden}
	if code := app.Run([]string{"usage", "--all"}); code != 0 {
		t.Fatalf("usage --all exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "forbidden") {
		t.Errorf("stderr = %q", errOut.String())
	}
	app, out, _ := env.app("")
	app.UsageGroups = &testGroupsProvider{err: usage.ErrForbidden}
	if code := app.Run([]string{"usage", "--all", "--json"}); code != 0 {
		t.Fatalf("usage --all json exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Accounts []struct {
			UsageError string `json:"usage_error"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].UsageError != "forbidden" {
		t.Errorf("accounts = %+v", payload.Accounts)
	}
}

func TestUsageCommand(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "personal"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}

	provider := &testGroupsProvider{groups: usageGroups()}
	app, out, errOut := env.app("")
	app.Width = 100
	app.UsageGroups = provider
	if code := app.Run([]string{"usage"}); code != 0 {
		t.Fatalf("usage exit = %d, stderr = %s", code, errOut)
	}
	for _, want := range []string{"Account:", "GROUP", "Gemini Models", "Claude and GPT models", "58%", "100%", "2026-10-24"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage output missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "LOWEST") || strings.Contains(out.String(), "MODEL") {
		t.Errorf("usage output still has the old model columns: %q", out.String())
	}

	provider = &testGroupsProvider{groups: usageGroups()}
	app, out, errOut = env.app("")
	app.UsageGroups = provider
	if code := app.Run([]string{"usage", "personal", "--json"}); code != 0 {
		t.Fatalf("usage json exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var payload struct {
		Email   string            `json:"email"`
		Groups  []usage.Group     `json:"groups"`
		Account *registry.Account `json:"account"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Email != "user@example.com" || len(payload.Groups) != 2 {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Account == nil || payload.Account.Alias != "personal" {
		t.Errorf("account = %+v", payload.Account)
	}
	if payload.Groups[0].Name != "Gemini Models" || len(payload.Groups[0].Buckets) != 2 || payload.Groups[0].Buckets[0].RemainingPercent != 58 {
		t.Errorf("groups = %+v", payload.Groups)
	}
}

func TestUsageCommandFallsBackToStoredAccount(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	if err := os.Remove(env.tokenPath); err != nil {
		t.Fatal(err)
	}
	provider := &testGroupsProvider{groups: usageGroups()}
	app, out, errOut := env.app("")
	app.UsageGroups = provider
	if code := app.Run([]string{"usage"}); code != 0 {
		t.Fatalf("usage exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "user@example.com") || provider.callCount() != 1 {
		t.Errorf("output = %q calls = %d", out.String(), provider.callCount())
	}
}

func TestUsageCommandNoCredential(t *testing.T) {
	env := newTestEnv(t, false)
	app, out, _ := env.app("")
	app.UsageGroups = &testGroupsProvider{groups: usageGroups()}
	if code := app.Run([]string{"usage", "--json"}); code != 1 {
		t.Fatalf("usage exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "credential_not_found" {
		t.Errorf("error = %+v", envelope.Error)
	}
}

func TestUsageCommandFetchFailure(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	app, out, _ := env.app("")
	app.UsageGroups = &testGroupsProvider{err: usage.ErrForbidden}
	if code := app.Run([]string{"usage", "--json"}); code != 1 {
		t.Fatalf("usage exit = %d", code)
	}
	envelope := decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "quota_error" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	if !strings.Contains(envelope.Error.Message, "forbidden") {
		t.Errorf("message = %q", envelope.Error.Message)
	}
}

func TestConfigLiveInterval(t *testing.T) {
	env := newTestEnv(t, false)
	store := registry.NewStore(env.storeDir)

	app, out, errOut := env.app("")
	if code := app.Run([]string{"config", "live", "--json"}); code != 0 {
		t.Fatalf("config get exit = %d, stderr = %s", code, errOut)
	}
	envelope := decodeTestEnvelope(t, out)
	var setting struct {
		Interval int `json:"live_interval_seconds"`
	}
	if err := json.Unmarshal(envelope.Data, &setting); err != nil {
		t.Fatal(err)
	}
	if setting.Interval != 60 {
		t.Errorf("default interval = %d", setting.Interval)
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"config", "live", "--interval", "15", "--json"}); code != 0 {
		t.Fatalf("config set exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if err := json.Unmarshal(envelope.Data, &setting); err != nil {
		t.Fatal(err)
	}
	if setting.Interval != 15 {
		t.Errorf("set interval = %d", setting.Interval)
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.LiveIntervalSeconds() != 15 {
		t.Errorf("stored interval = %d", reg.LiveIntervalSeconds())
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"config", "live"}); code != 0 {
		t.Fatalf("config human exit = %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out.String(), "15s") {
		t.Errorf("human output = %q", out.String())
	}
	app, _, _ = env.app("")
	if interval := app.liveInterval(); interval != 15*time.Second {
		t.Errorf("live interval = %s", interval)
	}

	app, out, errOut = env.app("")
	if code := app.Run([]string{"config", "live", "--interval", "4", "--json"}); code != 2 {
		t.Fatalf("invalid interval exit = %d, stderr = %s", code, errOut)
	}
	envelope = decodeTestEnvelope(t, out)
	if envelope.Error == nil || envelope.Error.Code != "usage" {
		t.Errorf("invalid interval error = %+v", envelope.Error)
	}
	reg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.LiveIntervalSeconds() != 15 {
		t.Errorf("invalid interval changed registry to %d", reg.LiveIntervalSeconds())
	}
}

func TestConfigLiveIntervalSurvivesPurge(t *testing.T) {
	env := newTestEnv(t, true)
	app, _, errOut := env.app("")
	if code := app.Run([]string{"add", "--alias", "first"}); code != 0 {
		t.Fatalf("add exit = %d, stderr = %s", code, errOut)
	}
	app, _, errOut = env.app("")
	if code := app.Run([]string{"config", "live", "--interval", "30"}); code != 0 {
		t.Fatalf("config exit = %d, stderr = %s", code, errOut)
	}
	app, _, errOut = env.app("")
	if code := app.Run([]string{"import", "--purge"}); code != 0 {
		t.Fatalf("purge exit = %d, stderr = %s", code, errOut)
	}
	reg, err := registry.NewStore(env.storeDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if reg.LiveIntervalSeconds() != 30 {
		t.Errorf("purge lost interval: %d", reg.LiveIntervalSeconds())
	}
}

func TestEveryCommandHasHelp(t *testing.T) {
	env := newTestEnv(t, false)
	commands := []string{
		"list", "add", "login", "switch", "remove", "alias",
		"status", "current", "usage", "import", "export", "path", "clean", "config", "version",
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
