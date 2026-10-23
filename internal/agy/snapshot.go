package agy

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	snapshotPrefix = "agy-auth-encrypted:v1:"
	snapshotKeyLen = 32
)

var ErrSnapshotKey = errors.New("snapshot encryption key unavailable")

// SnapshotVault encrypts credential snapshots at rest with AES-256-GCM. The
// key is a separate 0600 file so snapshots can be copied or synced without
// exposing refresh tokens.
type SnapshotVault struct {
	KeyPath string
}

func NewSnapshotVault(keyPath string) *SnapshotVault {
	return &SnapshotVault{KeyPath: keyPath}
}

func (v *SnapshotVault) IsEncrypted(raw []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte(snapshotPrefix))
}

func (v *SnapshotVault) Encode(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	key, err := v.loadKey(true)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("snapshot cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("snapshot cipher: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("snapshot nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, raw, []byte(snapshotPrefix))
	encoded := snapshotPrefix + base64.StdEncoding.EncodeToString(sealed)
	return []byte(encoded), nil
}

func (v *SnapshotVault) Decode(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte(snapshotPrefix)) {
		return raw, nil
	}
	key, err := v.loadKey(false)
	if err != nil {
		return nil, err
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(string(trimmed), snapshotPrefix))
	if err != nil {
		return nil, fmt.Errorf("snapshot encoding: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("snapshot cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("snapshot cipher: %w", err)
	}
	if len(payload) < gcm.NonceSize() {
		return nil, errors.New("snapshot ciphertext is truncated")
	}
	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(snapshotPrefix))
	if err != nil {
		return nil, errors.New("snapshot decryption failed")
	}
	return plain, nil
}

func (v *SnapshotVault) loadKey(create bool) ([]byte, error) {
	if v == nil || v.KeyPath == "" {
		return nil, ErrSnapshotKey
	}
	raw, err := os.ReadFile(v.KeyPath)
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil || len(key) != snapshotKeyLen {
			return nil, fmt.Errorf("%w: invalid key file", ErrSnapshotKey)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read snapshot key: %w", err)
	}
	if !create {
		return nil, ErrSnapshotKey
	}
	if err := os.MkdirAll(filepath.Dir(v.KeyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}
	lockPath := v.KeyPath + ".lock"
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			defer os.Remove(lockPath)
			if raw, err := os.ReadFile(v.KeyPath); err == nil {
				key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
				if decodeErr != nil || len(key) != snapshotKeyLen {
					return nil, fmt.Errorf("%w: invalid key file", ErrSnapshotKey)
				}
				return key, nil
			}
			key := make([]byte, snapshotKeyLen)
			if _, err := rand.Read(key); err != nil {
				return nil, fmt.Errorf("generate snapshot key: %w", err)
			}
			encoded := []byte(base64.StdEncoding.EncodeToString(key))
			if err := writeFileAtomic(v.KeyPath, encoded, 0o600); err != nil {
				return nil, fmt.Errorf("write snapshot key: %w", err)
			}
			return key, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("lock snapshot key: %w", err)
		}
		if raw, err := os.ReadFile(v.KeyPath); err == nil {
			key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
			if decodeErr != nil || len(key) != snapshotKeyLen {
				return nil, fmt.Errorf("%w: invalid key file", ErrSnapshotKey)
			}
			return key, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: key creation is locked", ErrSnapshotKey)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
