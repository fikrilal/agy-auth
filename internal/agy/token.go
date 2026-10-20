package agy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TokenFile mirrors the JSON document Antigravity CLI stores in the OS keyring
// (service "gemini", account "antigravity") and in the file fallback at
// ~/.gemini/antigravity-cli/antigravity-oauth-token.
type TokenFile struct {
	Token      Token  `json:"token"`
	AuthMethod string `json:"auth_method"`
	IDToken    string `json:"id_token"`
}

type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

// Claims is the subset of OIDC claims agy-auth uses. The ID token signature is
// not verified: it was issued to the Antigravity OAuth client and is read back
// from local credential storage.
type Claims struct {
	Issuer        string
	Audience      []string
	AuthorizedFor string
	Email         string
	EmailVerified bool
	Subject       string
	ExpiresAt     int64
	IssuedAt      int64
}

func ParseTokenFile(raw []byte) (*TokenFile, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errors.New("empty credential document")
	}
	var tf TokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return nil, fmt.Errorf("invalid credential JSON: %w", err)
	}
	if tf.Token.AccessToken == "" && tf.Token.RefreshToken == "" {
		return nil, errors.New("credential document has neither access_token nor refresh_token")
	}
	return &tf, nil
}

func (t *TokenFile) Email() string {
	claims, err := t.Claims()
	if err != nil {
		return ""
	}
	return claims.Email
}

func (t *TokenFile) Subject() string {
	claims, err := t.Claims()
	if err != nil {
		return ""
	}
	return claims.Subject
}

func (t *TokenFile) Claims() (Claims, error) {
	return ParseIDToken(t.IDToken)
}

func (t *TokenFile) ExpiryTime() (time.Time, bool) {
	value := strings.TrimSpace(t.Token.Expiry)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02T15:04:05.999999999Z0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (t *TokenFile) Expired(now time.Time) bool {
	expiry, ok := t.ExpiryTime()
	return ok && !expiry.After(now)
}

func (t *TokenFile) HasRefreshToken() bool {
	return strings.TrimSpace(t.Token.RefreshToken) != ""
}

// Fingerprint identifies the credential by its long-lived secret. Refresh
// tokens are stable for a Google account; access tokens rotate.
func (t *TokenFile) Fingerprint() string {
	secret := t.Token.RefreshToken
	if secret == "" {
		secret = t.Token.AccessToken
	}
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:16]
}

func ParseIDToken(idToken string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(idToken), ".")
	if len(parts) < 2 {
		return Claims{}, errors.New("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("decode id_token payload: %w", err)
	}
	var raw struct {
		Issuer        string          `json:"iss"`
		Audience      json.RawMessage `json:"aud"`
		AuthorizedFor string          `json:"azp"`
		Email         string          `json:"email"`
		EmailVerified bool            `json:"email_verified"`
		Subject       string          `json:"sub"`
		ExpiresAt     int64           `json:"exp"`
		IssuedAt      int64           `json:"iat"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Claims{}, fmt.Errorf("parse id_token claims: %w", err)
	}
	audience, err := parseAudience(raw.Audience)
	if err != nil {
		return Claims{}, err
	}
	return Claims{
		Issuer:        raw.Issuer,
		Audience:      audience,
		AuthorizedFor: raw.AuthorizedFor,
		Email:         raw.Email,
		EmailVerified: raw.EmailVerified,
		Subject:       raw.Subject,
		ExpiresAt:     raw.ExpiresAt,
		IssuedAt:      raw.IssuedAt,
	}, nil
}

func parseAudience(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil, nil
		}
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("parse id_token audience: %w", err)
	}
	return many, nil
}
