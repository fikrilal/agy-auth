package agy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, InstallationFileName)
	if err := os.WriteFile(path, []byte("AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInstallationID(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("id = %q", got)
	}
	if err := WriteInstallationID(path, got); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != got {
		t.Fatalf("written = %q", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
	fresh, err := NewInstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if NormalizeInstallationID(fresh) != fresh {
		t.Fatalf("generated id is not canonical: %q", fresh)
	}
	if _, err := ReadInstallationID(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing err = %v", err)
	}
	if err := os.WriteFile(path, []byte("not-a-uuid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInstallationID(path); err != ErrInvalidInstallationID {
		t.Fatalf("invalid err = %v", err)
	}
}

func TestStorageInstallationPathFollowsTokenFile(t *testing.T) {
	storage := &Storage{FilePath: filepath.Join("/tmp/agy-home", "antigravity-oauth-token")}
	if got, want := storage.InstallationPath(), filepath.Join("/tmp/agy-home", InstallationFileName); got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}
}
