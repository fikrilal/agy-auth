package agy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStorageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "antigravity-oauth-token")
	storage := &Storage{Service: "test", User: "test", FilePath: path, NoKeyring: true}
	raw := makeTokenFile(t, "user@example.com", "subject-1", "refresh-1", time.Now().Add(time.Hour).Format(time.RFC3339Nano))

	report := storage.Write(raw)
	if !report.OK() {
		t.Fatalf("Write failed: %v", report.Error())
	}
	if len(report.Written) != 1 || report.Written[0] != SourceFile {
		t.Fatalf("written = %v", report.Written)
	}

	read, err := storage.Read()
	if err != nil {
		t.Fatal(err)
	}
	if read.Source != SourceFile {
		t.Errorf("source = %s", read.Source)
	}
	if string(read.Raw) != string(raw) {
		t.Error("round trip content mismatch")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %o", info.Mode().Perm())
	}

	if err := storage.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Read(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("expected ErrNoCredentials, got %v", err)
	}
}

func TestWriteRejectsInvalidDocument(t *testing.T) {
	storage := &Storage{FilePath: filepath.Join(t.TempDir(), "token"), NoKeyring: true}
	report := storage.Write([]byte("not json"))
	if report.OK() {
		t.Error("expected write to fail")
	}
	if _, err := os.Stat(storage.FilePath); !errors.Is(err, os.ErrNotExist) {
		t.Error("invalid document must not be written")
	}
}

func TestReadRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := &Storage{FilePath: path, NoKeyring: true}
	if _, err := storage.Read(); err == nil {
		t.Fatal("expected corrupt credential error")
	}
}

func TestKeyringAvailableHonorsNoKeyring(t *testing.T) {
	storage := &Storage{NoKeyring: true}
	if storage.KeyringAvailable() {
		t.Error("expected keyring to be disabled")
	}
}

// TestKeyringRoundTrip is opt-in because it writes to the real OS keyring. It
// uses a throwaway service name and never touches agy's own entry.
func TestKeyringRoundTrip(t *testing.T) {
	if os.Getenv("AGY_AUTH_KEYRING_TEST") != "1" {
		t.Skip("set AGY_AUTH_KEYRING_TEST=1 to exercise the OS keyring")
	}
	storage := &Storage{
		Service:  "agy-auth-selftest",
		User:     "selftest",
		FilePath: filepath.Join(t.TempDir(), "token"),
	}
	if !storage.KeyringAvailable() {
		t.Skip("OS keyring unavailable")
	}
	raw := makeTokenFile(t, "test@example.com", "subject-test", "refresh-test", time.Now().Add(time.Hour).Format(time.RFC3339Nano))
	report := storage.Write(raw)
	if !report.OK() {
		t.Fatalf("Write: %v", report.Error())
	}
	hasKeyring := false
	for _, source := range report.Written {
		if source == SourceKeyring {
			hasKeyring = true
		}
	}
	if !hasKeyring {
		t.Fatalf("keyring write failed: %v", report.Error())
	}
	read, err := storage.Read()
	if err != nil {
		t.Fatal(err)
	}
	if read.Source != SourceKeyring {
		t.Errorf("source = %s", read.Source)
	}
	if string(read.Raw) != string(raw) {
		t.Error("keyring round trip mismatch")
	}
	if err := storage.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := storage.Delete(); err != nil && !errors.Is(err, ErrNoCredentials) {
		t.Errorf("second delete: %v", err)
	}
}
