package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func newerTestVersion() string {
	parts, ok := parseVersion(Version)
	if !ok {
		return "99.0.0"
	}
	return fmt.Sprintf("%d.0.0", parts[0]+1)
}

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
		_, _ = w.Write([]byte(`{"version":"` + newerTestVersion() + `"}`))
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
	if !status.Available || status.Latest != newerTestVersion() || status.Current != Version {
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
	if !strings.Contains(string(raw), `"`+newerTestVersion()+`"`) {
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
		_, _ = w.Write([]byte(`{"version":"` + newerTestVersion() + `"}`))
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	app := &App{
		Store:     registry.NewStore(t.TempDir()),
		Out:       &out,
		Err:       &errOut,
		UpdateURL: server.URL,
		Now:       func() time.Time { return testNow },
		TTY:       true,
		OutTTY:    false,
	}
	app.maybeNotifyUpdate()
	if !strings.Contains(errOut.String(), "update available") || !strings.Contains(errOut.String(), newerTestVersion()) {
		t.Errorf("stderr = %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestUpdatePromptDeclineDoesNotNag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"` + newerTestVersion() + `"}`))
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	app := &App{
		Store:     registry.NewStore(t.TempDir()),
		In:        strings.NewReader("n\n"),
		Out:       &out,
		Err:       &errOut,
		UpdateURL: server.URL,
		Now:       func() time.Time { return testNow },
		TTY:       true,
		OutTTY:    true,
		Command:   "list",
	}
	app.maybeNotifyUpdate()
	if !strings.Contains(out.String(), "Update now? [y/N]") || !strings.Contains(out.String(), "Update skipped") {
		t.Errorf("stdout = %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	app.maybeNotifyUpdate()
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("second prompt: stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestInstallReleaseBinary(t *testing.T) {
	asset, ok := releaseAssetName()
	if !ok {
		t.Skip("unsupported test platform")
	}
	binary := []byte("new binary")
	sum := sha256.Sum256(binary)
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "agy-auth")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installReleaseBinary(server.URL, "0.2.0", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Errorf("target = %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("target mode = %o", info.Mode().Perm())
	}
}

func TestInstallReleaseBinaryRejectsBadChecksum(t *testing.T) {
	asset, ok := releaseAssetName()
	if !ok {
		t.Skip("unsupported test platform")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write([]byte("tampered"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "agy-auth")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installReleaseBinary(server.URL, "0.2.0", target); err == nil {
		t.Fatal("bad checksum was accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old binary" {
		t.Errorf("target changed: %q", got)
	}
}

func TestUpdateCheckOptOut(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"version":"` + newerTestVersion() + `"}`))
	}))
	defer server.Close()

	t.Setenv(noUpdateCheckEnv, "1")
	app := &App{
		Store:     registry.NewStore(t.TempDir()),
		Out:       &bytes.Buffer{},
		Err:       &bytes.Buffer{},
		UpdateURL: server.URL,
		Now:       func() time.Time { return testNow },
		TTY:       true,
	}
	app.maybeNotifyUpdate()
	if requests.Load() != 0 {
		t.Errorf("requests = %d", requests.Load())
	}
	if app.Out.(*bytes.Buffer).Len() != 0 || app.Err.(*bytes.Buffer).Len() != 0 {
		t.Errorf("opt-out produced output")
	}
}

func TestVersionCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"` + newerTestVersion() + `"}`))
	}))
	defer server.Close()

	env := newTestEnv(t, false)
	app, out, errOut := env.app("")
	app.UpdateURL = server.URL
	if code := app.Run([]string{"version", "--check"}); code != 0 {
		t.Fatalf("version check exit = %d, stderr = %s", code, errOut.String())
	}
	for _, want := range []string{"agy-auth " + Version, "latest " + newerTestVersion(), "update available", "npm install -g"} {
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
