package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeText(t *testing.T, root, rel, content string) string {
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

func TestScanFindsHighConfidencePatterns(t *testing.T) {
	root := t.TempDir()
	oauthSecret := "GOCSPX-" + strings.Repeat("a", 24)
	npmToken := "npm_" + strings.Repeat("b", 40)
	path := writeText(t, root, "notes.md", "a "+oauthSecret+"\nb "+npmToken+"\n")
	findings, err := Scan(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %v", findings)
	}
}

func TestScanSkipsIgnoredPathsAndBinaries(t *testing.T) {
	root := t.TempDir()
	value := "GOCSPX-" + strings.Repeat("a", 24)
	wip := writeText(t, root, "_WIP/notes.md", value)
	binary := writeText(t, root, "image.png", value)
	findings, err := Scan(root, []string{wip, binary})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
}

func TestScanCleanFile(t *testing.T) {
	root := t.TempDir()
	path := writeText(t, root, "clean.md", "nothing to see here\n")
	findings, err := Scan(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
}
