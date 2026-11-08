package agy

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const InstallationFileName = "installation_id"

// ErrInvalidInstallationID means the file is not a UUID.
var ErrInvalidInstallationID = errors.New("installation id is invalid")

// InstallationPath is the live installation_id next to the credential file agy reads.
func (s *Storage) InstallationPath() string {
	token := ""
	if s != nil {
		token = s.FilePath
	}
	if token == "" {
		token = defaultTokenFilePath()
	}
	return filepath.Join(filepath.Dir(token), InstallationFileName)
}

// NormalizeInstallationID returns the canonical UUID, or "" when value is not one.
// A single trailing newline is accepted. agy writes the file with no newline.
func NormalizeInstallationID(value string) string {
	id := strings.ToLower(strings.TrimSpace(value))
	if len(id) != 36 {
		return ""
	}
	for i := 0; i < len(id); i++ {
		switch i {
		case 8, 13, 18, 23:
			if id[i] != '-' {
				return ""
			}
		default:
			c := id[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return ""
			}
		}
	}
	return id
}

// ReadInstallationID reads a live or stored installation id.
func ReadInstallationID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := NormalizeInstallationID(string(raw))
	if id == "" {
		return "", ErrInvalidInstallationID
	}
	return id, nil
}

// WriteInstallationID atomically writes the canonical UUID with mode 0600 and no newline.
func WriteInstallationID(path, id string) error {
	id = NormalizeInstallationID(id)
	if id == "" {
		return ErrInvalidInstallationID
	}
	return writeFileAtomic(path, []byte(id), 0o600)
}

// NewInstallationID returns a new random UUID version 4.
func NewInstallationID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate installation id: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
