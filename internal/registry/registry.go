package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
)

const Version = 1

type Account struct {
	ID          string    `json:"id"`
	Email       string    `json:"email,omitempty"`
	Subject     string    `json:"sub,omitempty"`
	Alias       string    `json:"alias,omitempty"`
	AuthMethod  string    `json:"auth_method,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	AddedAt     time.Time `json:"added_at"`
	LastUsedAt  time.Time `json:"last_used_at,omitempty"`
}

func (a Account) DisplayName() string {
	switch {
	case a.Alias != "" && a.Email != "":
		return a.Alias + " (" + a.Email + ")"
	case a.Email != "":
		return a.Email
	case a.Alias != "":
		return a.Alias
	default:
		return a.ID
	}
}

type Registry struct {
	Version  int       `json:"version"`
	Active   string    `json:"active,omitempty"`
	Previous string    `json:"previous,omitempty"`
	Accounts []Account `json:"accounts"`
}

type Store struct {
	Dir         string
	AccountsDir string
	Registry    string
}

func DefaultStore() (*Store, error) {
	if dir := os.Getenv("AGY_AUTH_HOME"); dir != "" {
		return NewStore(dir), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config directory: %w", err)
	}
	return NewStore(filepath.Join(base, "agy-auth")), nil
}

func NewStore(dir string) *Store {
	return &Store{
		Dir:         dir,
		AccountsDir: filepath.Join(dir, "accounts"),
		Registry:    filepath.Join(dir, "registry.json"),
	}
}

func (s *Store) Load() (*Registry, error) {
	raw, err := os.ReadFile(s.Registry)
	if errors.Is(err, os.ErrNotExist) {
		return &Registry{Version: Version}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", s.Registry, err)
	}
	if reg.Version == 0 {
		reg.Version = Version
	}
	if reg.Version > Version {
		return nil, fmt.Errorf("registry version %d is newer than supported version %d", reg.Version, Version)
	}
	seenIDs := make(map[string]bool, len(reg.Accounts))
	seenAliases := make(map[string]string, len(reg.Accounts))
	for i := range reg.Accounts {
		account := &reg.Accounts[i]
		if err := validateID(account.ID); err != nil {
			return nil, fmt.Errorf("registry account %d: %w", i+1, err)
		}
		if seenIDs[account.ID] {
			return nil, fmt.Errorf("registry contains duplicate account id %q", account.ID)
		}
		seenIDs[account.ID] = true
		if account.Alias != "" {
			key := strings.ToLower(account.Alias)
			if other, exists := seenAliases[key]; exists && other != account.ID {
				return nil, fmt.Errorf("registry alias %q is used by multiple accounts", account.Alias)
			}
			seenAliases[key] = account.ID
		}
	}
	if reg.Active != "" && !seenIDs[reg.Active] {
		reg.Active = ""
	}
	if reg.Previous != "" && !seenIDs[reg.Previous] {
		reg.Previous = ""
	}
	if reg.Previous == reg.Active {
		reg.Previous = ""
	}
	return &reg, nil
}

func (s *Store) Save(reg *Registry) error {
	if reg.Version == 0 {
		reg.Version = Version
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	raw = append(raw, '\n')
	return writeFileAtomic(s.Registry, raw, 0o600)
}

func (s *Store) AccountTokenPath(id string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.AccountsDir, id+".json"), nil
}

func (s *Store) SaveToken(id string, raw []byte) error {
	if _, err := agy.ParseTokenFile(raw); err != nil {
		return err
	}
	path, err := s.AccountTokenPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.AccountsDir, 0o700); err != nil {
		return fmt.Errorf("create accounts directory: %w", err)
	}
	return writeFileAtomic(path, raw, 0o600)
}

func (s *Store) LoadToken(id string) ([]byte, error) {
	path, err := s.AccountTokenPath(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read account %s: %w", id, err)
	}
	return raw, nil
}

func (s *Store) DeleteToken(id string) error {
	path, err := s.AccountTokenPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete account %s: %w", id, err)
	}
	return nil
}

type UpsertResult struct {
	Account Account
	Created bool
}

// Upsert adds or refreshes an account from a raw agy credential document.
// When activate is true the account becomes the active account.
func (s *Store) Upsert(raw []byte, alias string, activate bool) (UpsertResult, error) {
	tf, err := agy.ParseTokenFile(raw)
	if err != nil {
		return UpsertResult{}, err
	}
	claims, _ := tf.Claims()
	fingerprint := tf.Fingerprint()
	id := accountID(claims.Subject, fingerprint)
	if alias != "" {
		if err := ValidateAlias(alias); err != nil {
			return UpsertResult{}, err
		}
	}

	reg, err := s.Load()
	if err != nil {
		return UpsertResult{}, err
	}
	previousActive := reg.Active

	index := findAccountIndex(reg, id, claims.Subject, fingerprint)
	now := time.Now().UTC()
	created := index < 0
	if created {
		reg.Accounts = append(reg.Accounts, Account{
			ID:         id,
			AddedAt:    now,
			LastUsedAt: now,
		})
		index = len(reg.Accounts) - 1
	}
	account := &reg.Accounts[index]

	if alias != "" {
		for i := range reg.Accounts {
			if i == index {
				continue
			}
			if strings.EqualFold(reg.Accounts[i].Alias, alias) {
				return UpsertResult{}, fmt.Errorf("alias %q is already used by %s", alias, reg.Accounts[i].DisplayName())
			}
		}
		account.Alias = strings.TrimSpace(alias)
	}
	if claims.Email != "" {
		account.Email = claims.Email
	}
	if claims.Subject != "" {
		account.Subject = claims.Subject
	}
	if tf.AuthMethod != "" {
		account.AuthMethod = tf.AuthMethod
	}
	if fingerprint != "" {
		account.Fingerprint = fingerprint
	}
	account.LastUsedAt = now
	if account.AddedAt.IsZero() {
		account.AddedAt = now
	}
	if activate || reg.Active == "" {
		if previousActive != "" && previousActive != account.ID {
			reg.Previous = previousActive
		}
		if reg.Previous == account.ID {
			reg.Previous = ""
		}
		reg.Active = account.ID
	}

	if err := s.SaveToken(account.ID, raw); err != nil {
		return UpsertResult{}, err
	}
	if err := s.Save(reg); err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{Account: *account, Created: created}, nil
}

// Match finds the stored account that corresponds to a live credential
// document, preferring the stable Google subject over the refresh token.
func (r *Registry) Match(raw []byte) (*Account, error) {
	tf, err := agy.ParseTokenFile(raw)
	if err != nil {
		return nil, err
	}
	claims, _ := tf.Claims()
	fingerprint := tf.Fingerprint()

	if claims.Subject != "" {
		for i := range r.Accounts {
			if r.Accounts[i].Subject == claims.Subject {
				return &r.Accounts[i], nil
			}
		}
	}
	if fingerprint != "" {
		for i := range r.Accounts {
			if r.Accounts[i].Fingerprint == fingerprint {
				return &r.Accounts[i], nil
			}
		}
	}
	if claims.Email != "" {
		var match *Account
		for i := range r.Accounts {
			if strings.EqualFold(r.Accounts[i].Email, claims.Email) {
				if match != nil {
					return nil, nil
				}
				match = &r.Accounts[i]
			}
		}
		return match, nil
	}
	return nil, nil
}

func (r *Registry) ActiveAccount() *Account {
	if r.Active == "" {
		return nil
	}
	for i := range r.Accounts {
		if r.Accounts[i].ID == r.Active {
			return &r.Accounts[i]
		}
	}
	return nil
}

func (r *Registry) FindByID(id string) *Account {
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			return &r.Accounts[i]
		}
	}
	return nil
}

// Resolve supports row numbers (1-based), exact account IDs, exact aliases,
// exact emails, account ID prefixes, and email/alias/ID substrings. Earlier
// forms win, so an exact alias is not widened by a substring match elsewhere.
// Results preserve registry order.
func (r *Registry) Resolve(query string) []Account {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if n, err := strconv.Atoi(query); err == nil {
		if n >= 1 && n <= len(r.Accounts) {
			return []Account{r.Accounts[n-1]}
		}
		return nil
	}

	for _, account := range r.Accounts {
		if strings.EqualFold(account.ID, query) {
			return []Account{account}
		}
	}

	for _, account := range r.Accounts {
		if account.Alias != "" && strings.EqualFold(account.Alias, query) {
			return []Account{account}
		}
	}

	var exactEmail []Account
	for _, account := range r.Accounts {
		if account.Email != "" && strings.EqualFold(account.Email, query) {
			exactEmail = append(exactEmail, account)
		}
	}
	if len(exactEmail) > 0 {
		return dedupe(exactEmail)
	}

	lower := strings.ToLower(query)
	var idPrefix []Account
	for _, account := range r.Accounts {
		if strings.HasPrefix(strings.ToLower(account.ID), lower) {
			idPrefix = append(idPrefix, account)
		}
	}
	if len(idPrefix) > 0 {
		return dedupe(idPrefix)
	}

	var partial []Account
	for _, account := range r.Accounts {
		if strings.Contains(strings.ToLower(account.Email), lower) ||
			strings.Contains(strings.ToLower(account.Alias), lower) ||
			strings.Contains(strings.ToLower(account.ID), lower) {
			partial = append(partial, account)
		}
	}
	return dedupe(partial)
}

func (r *Registry) SetAlias(id, alias string) error {
	alias = strings.TrimSpace(alias)
	if err := ValidateAlias(alias); err != nil {
		return err
	}
	index := -1
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			index = i
		} else if strings.EqualFold(r.Accounts[i].Alias, alias) {
			return fmt.Errorf("alias %q is already used by %s", alias, r.Accounts[i].DisplayName())
		}
	}
	if index < 0 {
		return fmt.Errorf("account %s not found", id)
	}
	r.Accounts[index].Alias = alias
	return nil
}

func (r *Registry) ClearAlias(id string) error {
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			r.Accounts[i].Alias = ""
			return nil
		}
	}
	return fmt.Errorf("account %s not found", id)
}

func (r *Registry) Remove(id string) bool {
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			r.Accounts = append(r.Accounts[:i], r.Accounts[i+1:]...)
			if r.Active == id {
				r.Active = ""
			}
			if r.Previous == id {
				r.Previous = ""
			}
			return true
		}
	}
	return false
}

var aliasPattern = regexp.MustCompile(`^[^\s]+$`)

func ValidateAlias(alias string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return errors.New("alias cannot be empty")
	}
	if len(alias) > 32 {
		return errors.New("alias cannot be longer than 32 characters")
	}
	if _, err := strconv.Atoi(alias); err == nil {
		return errors.New("alias cannot be a number; numbers select rows in list output")
	}
	if strings.HasPrefix(alias, "-") {
		return errors.New("alias cannot start with '-'")
	}
	if !aliasPattern.MatchString(alias) {
		return errors.New("alias cannot contain whitespace")
	}
	return nil
}

func accountID(subject, fingerprint string) string {
	key := subject
	if key == "" {
		key = fingerprint
	}
	if key == "" {
		key = time.Now().String()
	}
	sum := sha256.Sum256([]byte(key))
	return "acct_" + hex.EncodeToString(sum[:])[:10]
}

func findAccountIndex(reg *Registry, id, subject, fingerprint string) int {
	for i := range reg.Accounts {
		if subject != "" && reg.Accounts[i].Subject == subject {
			return i
		}
	}
	for i := range reg.Accounts {
		if fingerprint != "" && reg.Accounts[i].Fingerprint == fingerprint {
			return i
		}
	}
	for i := range reg.Accounts {
		if reg.Accounts[i].ID == id {
			return i
		}
	}
	return -1
}

func validateID(id string) error {
	if id == "" {
		return errors.New("empty account id")
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("invalid account id %q", id)
	}
	return nil
}

func dedupe(accounts []Account) []Account {
	seen := make(map[string]bool, len(accounts))
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if seen[account.ID] {
			continue
		}
		seen[account.ID] = true
		result = append(result, account)
	}
	return result
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
