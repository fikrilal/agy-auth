package agy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/backup"
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

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("permissions = %o", info.Mode().Perm())
		}
	}

	if err := storage.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Read(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("expected ErrNoCredentials, got %v", err)
	}
}

func TestStorageBacksUpChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	backups := backup.New(filepath.Join(dir, "backups"))
	storage := &Storage{FilePath: path, NoKeyring: true, Backup: backups}
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	first := makeTokenFile(t, "first@example.com", "subject-1", "refresh-1", expiry)
	second := makeTokenFile(t, "second@example.com", "subject-2", "refresh-2", expiry)

	if report := storage.Write(first); !report.OK() {
		t.Fatalf("first write failed: %v", report.Error())
	}
	if report := storage.Write(second); !report.OK() {
		t.Fatalf("second write failed: %v", report.Error())
	}
	managed, err := backups.Managed(backup.Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 1 {
		t.Fatalf("backups = %d, want 1", len(managed))
	}
	backedUp, err := os.ReadFile(managed[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backedUp, first) {
		t.Error("backup does not contain the previous credential")
	}

	if report := storage.Write(second); !report.OK() {
		t.Fatalf("idempotent write failed: %v", report.Error())
	}
	managed, err = backups.Managed(backup.Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 1 {
		t.Errorf("identical write created a backup: %d", len(managed))
	}

	if err := storage.Delete(); err != nil {
		t.Fatal(err)
	}
	managed, err = backups.Managed(backup.Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(managed) != 2 {
		t.Errorf("backups after delete = %d, want 2", len(managed))
	}
}

func TestBackupFailureStopsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	backupPath := filepath.Join(dir, "backups")
	if err := os.WriteFile(backupPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := &Storage{FilePath: path, NoKeyring: true, Backup: backup.New(backupPath)}
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	first := makeTokenFile(t, "first@example.com", "subject-1", "refresh-1", expiry)
	second := makeTokenFile(t, "second@example.com", "subject-2", "refresh-2", expiry)

	if report := storage.Write(first); !report.OK() {
		t.Fatalf("first write failed: %v", report.Error())
	}
	report := storage.Write(second)
	if report.OK() {
		t.Fatal("expected backup failure")
	}
	if !errors.Is(report.Error(), ErrBackup) {
		t.Fatalf("error = %v, want ErrBackup", report.Error())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, first) {
		t.Error("credential was overwritten despite backup failure")
	}
	if err := storage.Delete(); err == nil || !errors.Is(err, ErrBackup) {
		t.Errorf("delete error = %v, want ErrBackup", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("credential was deleted despite backup failure: %v", err)
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

func TestDesktopTokenFileMirrorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	desktopPath := filepath.Join(dir, "jetski-standalone-oauth-token")
	storage := &Storage{
		Service:         "test",
		User:            "test",
		FilePath:        filepath.Join(dir, "antigravity-oauth-token"),
		DesktopFilePath: desktopPath,
		NoKeyring:       true,
	}
	raw := makeTokenFile(t, "user@example.com", "subject-1", "refresh-1", time.Now().Add(time.Hour).Format(time.RFC3339Nano))

	if err := storage.WriteDesktopToken([]byte("not json")); err == nil {
		t.Fatal("expected invalid token to be rejected")
	}
	if _, err := os.Stat(desktopPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid token must not create desktop token file")
	}

	if err := storage.WriteDesktopToken(raw); err != nil {
		t.Fatalf("WriteDesktopToken: %v", err)
	}
	got, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("desktop token content mismatch")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(desktopPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("desktop token permissions = %o, want 0600", info.Mode().Perm())
		}
	}

	if err := storage.DeleteDesktopToken(); err != nil {
		t.Fatalf("DeleteDesktopToken: %v", err)
	}
	if _, err := os.Stat(desktopPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desktop token still exists after delete: %v", err)
	}
	if err := storage.DeleteDesktopToken(); err != nil {
		t.Fatalf("idempotent DeleteDesktopToken: %v", err)
	}

	// Empty DesktopFilePath is a no-op.
	empty := &Storage{FilePath: filepath.Join(dir, "cli-token"), NoKeyring: true}
	if err := empty.WriteDesktopToken(raw); err != nil {
		t.Fatalf("empty WriteDesktopToken: %v", err)
	}
	if err := empty.DeleteDesktopToken(); err != nil {
		t.Fatalf("empty DeleteDesktopToken: %v", err)
	}
}

func TestDefaultDesktopTokenFilePathIsolation(t *testing.T) {
	t.Setenv("AGY_AUTH_DESKTOP_TOKEN_FILE", "/custom/desktop-token")
	t.Setenv("AGY_AUTH_AGY_TOKEN_FILE", "/custom/cli-token")
	if got := defaultDesktopTokenFilePath(); got != "/custom/desktop-token" {
		t.Fatalf("override desktop path = %q", got)
	}

	t.Setenv("AGY_AUTH_DESKTOP_TOKEN_FILE", "")
	if got := defaultDesktopTokenFilePath(); got != "" {
		t.Fatalf("expected empty desktop path when AGY_AUTH_AGY_TOKEN_FILE is overridden, got %q", got)
	}
}
