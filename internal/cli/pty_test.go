//go:build linux || darwin

package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/fikrilal/agy-auth/internal/registry"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLivePTY(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agy-auth")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agy-auth")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	storeDir := filepath.Join(t.TempDir(), "config")
	store := registry.NewStore(storeDir)
	first := makeCLIToken(t, "first@example.com", "subject-first", "refresh-first")
	second := makeCLIToken(t, "second@example.com", "subject-second", "refresh-second")
	if _, err := store.Upsert(first, "first", true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(second, "second", false); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "antigravity-oauth-token")
	if err := os.WriteFile(tokenPath, first, 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "list", "--live")
	command.Env = append(os.Environ(),
		"AGY_AUTH_HOME="+storeDir,
		"AGY_AUTH_STORAGE=file",
		"AGY_AUTH_AGY_TOKEN_FILE="+tokenPath,
		"TERM=xterm-256color",
		"NO_COLOR=1",
	)
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 12, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}()

	var output lockedBuffer
	read := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, terminal)
		read <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(output.String(), "Accounts (live)") || !strings.Contains(output.String(), "second@example.com") {
		if time.Now().After(deadline) {
			t.Fatalf("live view did not render: %q", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := terminal.Write([]byte("j2q")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-read:
	case <-time.After(3 * time.Second):
		t.Fatalf("live view did not exit: %q", output.String())
	}
	if err := command.Wait(); err != nil {
		t.Logf("live view wait: %v", err)
	}
	text := output.String()
	for _, want := range []string{"Accounts (live)", "first@example.com", "second@example.com", "\x1b[?1049l"} {
		if !strings.Contains(text, want) {
			t.Errorf("PTY output missing %q: %q", want, text)
		}
	}
}

func TestSwitchInteractiveThenUpdatePromptPTY(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"version": "99.0.0"}`)
	}))
	defer server.Close()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agy-auth")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agy-auth")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	storeDir := filepath.Join(t.TempDir(), "config")
	store := registry.NewStore(storeDir)
	first := makeCLIToken(t, "first@example.com", "subject-first", "refresh-first")
	second := makeCLIToken(t, "second@example.com", "subject-second", "refresh-second")
	if _, err := store.Upsert(first, "first", true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(second, "second", false); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "antigravity-oauth-token")
	if err := os.WriteFile(tokenPath, first, 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "switch")
	command.Env = append(os.Environ(),
		"AGY_AUTH_HOME="+storeDir,
		"AGY_AUTH_STORAGE=file",
		"AGY_AUTH_AGY_TOKEN_FILE="+tokenPath,
		"AGY_AUTH_UPDATE_URL="+server.URL,
		"TERM=xterm-256color",
		"NO_COLOR=1",
	)
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 15, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}()

	var output lockedBuffer
	read := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, terminal)
		read <- err
	}()

	// Wait for interactive picker to show
	pickerDeadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(output.String(), "Select an account to activate") {
		if time.Now().After(pickerDeadline) {
			t.Fatalf("interactive picker did not render: %q", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Press Enter to select account in picker
	if _, err := terminal.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}

	// Wait for update prompt to appear
	promptDeadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(output.String(), "Update now? [y/N]") {
		if time.Now().After(promptDeadline) {
			t.Fatalf("update prompt did not appear after switch: %q", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Answer "y\n" to prompt. If stdin was leaked, it would hang here!
	if _, err := terminal.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}

	// Wait for process to exit cleanly without hanging
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatalf("process hung on update prompt: %q", output.String())
	}
	if err := command.Wait(); err != nil {
		t.Logf("switch command wait: %v", err)
	}

	text := output.String()
	if !strings.Contains(text, "Switched to") {
		t.Errorf("missing switch confirmation: %q", text)
	}
	if !strings.Contains(text, "Updating to 99.0.0...") && !strings.Contains(text, "update failed:") {
		t.Errorf("missing update attempt in output: %q", text)
	}
}
