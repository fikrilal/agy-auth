package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSource(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func checkSource(t *testing.T, rel, content string) []Finding {
	t.Helper()
	root := t.TempDir()
	path := writeSource(t, root, rel, content)
	findings, err := Check(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func hasRule(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

func TestRuleOutputBoundary(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
import "fmt"
func f() { fmt.Println("x") }
`)
	if !hasRule(findings, "output-boundary") {
		t.Errorf("expected output-boundary, got %v", findings)
	}
}

func TestRuleJSONBoundary(t *testing.T) {
	findings := checkSource(t, "internal/cli/x.go", `package cli
import "encoding/json"
func f() { _ = json.NewEncoder(nil) }
`)
	if !hasRule(findings, "json-boundary") {
		t.Errorf("expected json-boundary, got %v", findings)
	}
}

func TestRuleKeyringBoundary(t *testing.T) {
	findings := checkSource(t, "internal/foo/x.go", `package foo
import "github.com/zalando/go-keyring"
func f() { _ = keyring.Get("a", "b") }
`)
	if !hasRule(findings, "keyring-boundary") {
		t.Errorf("expected keyring-boundary, got %v", findings)
	}
}

func TestRuleAtomicWrites(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
import "os"
func f() { _ = os.WriteFile("x", []byte("x"), 0o600) }
`)
	if !hasRule(findings, "atomic-writes") {
		t.Errorf("expected atomic-writes, got %v", findings)
	}

	allowed := checkSource(t, "internal/agy/x.go", `package agy
import "os"
func writeFileAtomic() { _ = os.WriteFile("x", []byte("x"), 0o600) }
`)
	if hasRule(allowed, "atomic-writes") {
		t.Errorf("writeFileAtomic should be allowed, got %v", allowed)
	}
}

func TestRuleFilePermissions(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
import "os"
func f() { _, _ = os.OpenFile("x", os.O_CREATE, 0o644) }
`)
	if !hasRule(findings, "file-permissions") {
		t.Errorf("expected file-permissions, got %v", findings)
	}
}

func TestRuleArchitecture(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
import _ "github.com/fikrilal/agy-auth/internal/cli"
`)
	if !hasRule(findings, "architecture") {
		t.Errorf("expected architecture, got %v", findings)
	}

	clean := checkSource(t, "internal/cli/x.go", `package cli
import _ "github.com/fikrilal/agy-auth/internal/agy"
`)
	if hasRule(clean, "architecture") {
		t.Errorf("internal/cli may import internal/agy, got %v", clean)
	}
}

func TestRuleNoPanic(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
func f() { panic("x") }
`)
	if !hasRule(findings, "no-panic") {
		t.Errorf("expected no-panic, got %v", findings)
	}
}

func TestRuleNoOSExit(t *testing.T) {
	findings := checkSource(t, "internal/agy/x.go", `package agy
import "os"
func f() { os.Exit(1) }
`)
	if !hasRule(findings, "no-os-exit") {
		t.Errorf("expected no-os-exit, got %v", findings)
	}
}

func TestRuleTestIsolation(t *testing.T) {
	findings := checkSource(t, "internal/cli/x_test.go", `package cli
func f() { _ = registry.DefaultStore() }
`)
	if !hasRule(findings, "test-isolation") {
		t.Errorf("expected test-isolation, got %v", findings)
	}
}

func TestCleanSource(t *testing.T) {
	findings := checkSource(t, "internal/cli/x.go", `package cli
func f() {}
`)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %v", findings)
	}
}
