package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/registry"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"0.2.0", "0.1.0", true},
		{"v0.2.0", "0.1.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"1.0.0", "0.9.9", true},
		{"invalid", "0.1.0", false},
	}
	for _, test := range cases {
		if got := versionNewer(test.latest, test.current); got != test.want {
			t.Errorf("versionNewer(%q, %q) = %v", test.latest, test.current, got)
		}
	}
}

func TestCheckForUpdateCaches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.2.0"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	app := &App{
		Store:     registry.NewStore(dir),
		UpdateURL: server.URL,
		Now:       func() time.Time { return now },
	}
	status, err := app.checkForUpdate(true)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.Latest != "0.2.0" || status.Current != Version {
		t.Errorf("status = %+v", status)
	}
	if requests.Load() != 1 {
		t.Errorf("requests = %d", requests.Load())
	}
	status, err = app.checkForUpdate(false)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || requests.Load() != 1 {
		t.Errorf("cached status = %+v requests=%d", status, requests.Load())
	}
	raw, err := os.ReadFile(filepath.Join(dir, updateCacheFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"0.2.0"`) {
		t.Errorf("cache = %q", raw)
	}
	info, err := os.Stat(filepath.Join(dir, updateCacheFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache permissions = %o", info.Mode().Perm())
	}
}

func TestMaybeNotifyUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.2.0"}`))
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	app := &App{
		Store:     registry.NewStore(t.TempDir()),
		Out:       &out,
		Err:       &errOut,
		UpdateURL: server.URL,
		Now:       func() time.Time { return testNow },
	}
	app.maybeNotifyUpdate()
	if !strings.Contains(errOut.String(), "update available") || !strings.Contains(errOut.String(), "0.2.0") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestVersionCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.2.0"}`))
	}))
	defer server.Close()

	env := newTestEnv(t, false)
	app, out, errOut := env.app("")
	app.UpdateURL = server.URL
	if code := app.Run([]string{"version", "--check"}); code != 0 {
		t.Fatalf("version check exit = %d, stderr = %s", code, errOut.String())
	}
	for _, want := range []string{"agy-auth " + Version, "latest 0.2.0", "update available", "npm install -g"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestShouldCheckForUpdates(t *testing.T) {
	if !shouldCheckForUpdates("ls", nil) {
		t.Error("list alias should check")
	}
	if shouldCheckForUpdates("list", []string{"--live"}) {
		t.Error("live list should not check")
	}
	if shouldCheckForUpdates("version", nil) {
		t.Error("version handles its own check")
	}
	if shouldCheckForUpdates("help", nil) {
		t.Error("help should not check")
	}
}
