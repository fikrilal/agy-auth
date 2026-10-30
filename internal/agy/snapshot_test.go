package agy

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSnapshotVaultRoundTrip(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "keys", "encryption.key")
	vault := NewSnapshotVault(keyPath)
	raw := []byte(`{"token":{"refresh_token":"secret-refresh"}}`)
	encoded, err := vault.Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("secret-refresh")) {
		t.Fatalf("ciphertext contains plaintext: %q", encoded)
	}
	if !vault.IsEncrypted(encoded) {
		t.Fatal("encoded value is not marked encrypted")
	}
	second, err := vault.Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encoded, second) {
		t.Fatal("nonce was reused")
	}
	decoded, err := vault.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Errorf("decoded = %q", decoded)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("key permissions = %o", info.Mode().Perm())
		}
		keyDir, err := os.Stat(filepath.Dir(keyPath))
		if err != nil {
			t.Fatal(err)
		}
		if keyDir.Mode().Perm() != 0o700 {
			t.Errorf("key directory permissions = %o", keyDir.Mode().Perm())
		}
	}
}

func TestSnapshotVaultPlaintextAndMissingKey(t *testing.T) {
	vault := NewSnapshotVault(filepath.Join(t.TempDir(), "encryption.key"))
	raw := []byte(`{"token":{"refresh_token":"legacy"}}`)
	decoded, err := vault.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Errorf("plaintext changed: %q", decoded)
	}
	encoded, err := vault.Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	missing := NewSnapshotVault(filepath.Join(t.TempDir(), "missing.key"))
	if _, err := missing.Decode(encoded); err == nil {
		t.Fatal("decrypting without the key succeeded")
	}
	corrupt := append([]byte(snapshotPrefix), []byte("not-base64")...)
	if _, err := vault.Decode(corrupt); err == nil {
		t.Fatal("corrupt ciphertext succeeded")
	}
}
