package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
)

// Antigravity CLI's OAuth client is embedded in the installed agy binary. This
// repository does not embed the client ID or secret; they are discovered from
// agy at login time and can be overridden with environment variables.
const (
	antigravityClientIDHash     = "bf00c418024ba6bf606ccdc37120976e41bc429dd1d46ecf16a729aa532626ea"
	antigravityClientSecretHash = "1d2f041093fd95aa8995a038c711d50a7960da09a505381c09a745d6ad0ecc60"
	clientIDSuffix              = ".apps.googleusercontent.com"
	clientSecretPrefix          = "GOCSPX-"
	discoveryCarry              = 128
	minClientIDLength           = 40
	minClientSecretLength       = 20
)

var (
	ClientID     = os.Getenv("AGY_AUTH_CLIENT_ID")
	ClientSecret = os.Getenv("AGY_AUTH_CLIENT_SECRET")
)

const (
	AuthEndpoint  = "https://accounts.google.com/o/oauth2/auth"
	RedirectURI   = "https://antigravity.google/oauth-callback"
	DefaultScopes = "https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email " +
		"https://www.googleapis.com/auth/userinfo.profile " +
		"https://www.googleapis.com/auth/cclog " +
		"https://www.googleapis.com/auth/experimentsandconfigs " +
		"https://www.googleapis.com/auth/aicode openid"
)

var TokenEndpoint = "https://oauth2.googleapis.com/token"

var HTTPClient = &http.Client{Timeout: 30 * time.Second}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func NewState() (string, error) {
	return randomURLSafe(24)
}

func NewVerifier() (string, error) {
	return randomURLSafe(32)
}

func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func AuthURL(state, verifier string) string {
	params := url.Values{}
	params.Set("access_type", "offline")
	params.Set("client_id", ClientID)
	params.Set("code_challenge", Challenge(verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "consent")
	params.Set("redirect_uri", RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", DefaultScopes)
	params.Set("state", state)
	return AuthEndpoint + "?" + params.Encode()
}

// NormalizeCode accepts either the bare authorization code shown by
// antigravity.google/oauth-callback or a pasted callback URL. When a state is
// present it must match the state generated for this login attempt.
func NormalizeCode(input, expectedState string) (string, error) {
	value := strings.TrimSpace(input)
	value = strings.Trim(value, `"'`)
	if value == "" {
		return "", errors.New("authorization code is empty")
	}

	var query url.Values
	if strings.Contains(value, "code=") || strings.Contains(value, "state=") || strings.Contains(value, "error=") {
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			parsed, err := url.Parse(value)
			if err != nil {
				return "", fmt.Errorf("parse callback URL: %w", err)
			}
			query = parsed.Query()
		} else {
			parsed, err := url.ParseQuery(value)
			if err != nil {
				return "", fmt.Errorf("parse authorization code: %w", err)
			}
			query = parsed
		}
	}

	if query != nil {
		if oauthErr := query.Get("error"); oauthErr != "" {
			description := query.Get("error_description")
			if description == "" {
				description = oauthErr
			}
			return "", fmt.Errorf("authorization failed: %s", description)
		}
		if state := query.Get("state"); state != "" && expectedState != "" && state != expectedState {
			return "", errors.New("authorization state mismatch; start the login flow again")
		}
		if code := query.Get("code"); code != "" {
			return code, nil
		}
		return "", errors.New("callback URL does not contain a code parameter")
	}

	return value, nil
}

func Exchange(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	if ClientID == "" || ClientSecret == "" {
		return nil, errors.New("OAuth client credentials are not configured; set AGY_AUTH_CLIENT_ID and AGY_AUTH_CLIENT_SECRET, or use --via-agy")
	}
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("client_secret", ClientSecret)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", RedirectURI)

	return requestToken(ctx, form, "token exchange")
}

// Refresh exchanges a stored refresh token for a new access token. The caller
// keeps the returned values in memory; agy remains the credential owner.
func Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("refresh token is empty")
	}
	if ClientID == "" || ClientSecret == "" {
		if err := EnsureClient(""); err != nil {
			return nil, err
		}
	}
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("client_secret", ClientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	response, err := requestToken(ctx, form, "token refresh")
	if err != nil {
		return nil, err
	}
	if response.RefreshToken == "" {
		response.RefreshToken = refreshToken
	}
	return response, nil
}

func requestToken(ctx context.Context, form url.Values, action string) (*TokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", action, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var tokenResponse TokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, fmt.Errorf("parse token response (HTTP %d): %w", response.StatusCode, err)
	}
	if response.StatusCode != http.StatusOK {
		description := tokenResponse.ErrorDesc
		if description == "" {
			description = tokenResponse.Error
		}
		if description == "" {
			description = http.StatusText(response.StatusCode)
		}
		return nil, fmt.Errorf("%s failed (HTTP %d): %s", action, response.StatusCode, description)
	}
	if tokenResponse.AccessToken == "" {
		return nil, fmt.Errorf("%s response did not contain an access token", action)
	}
	if tokenResponse.TokenType == "" {
		tokenResponse.TokenType = "Bearer"
	}
	return &tokenResponse, nil
}

func TokenFromResponse(response *TokenResponse, authMethod string, now time.Time) agy.TokenFile {
	expiresIn := response.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return agy.TokenFile{
		Token: agy.Token{
			AccessToken:  response.AccessToken,
			TokenType:    response.TokenType,
			RefreshToken: response.RefreshToken,
			Expiry:       now.Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339Nano),
		},
		AuthMethod: authMethod,
		IDToken:    response.IDToken,
	}
}

func MarshalTokenFile(tokenFile agy.TokenFile) ([]byte, error) {
	raw, err := json.MarshalIndent(tokenFile, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func randomURLSafe(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsureClient fills in ClientID and ClientSecret from the environment or from
// the installed agy binary. agy must already be installed to use this tool, so
// reading its embedded OAuth client avoids shipping the credential pair here.
func EnsureClient(agyPath string) error {
	if ClientID != "" && ClientSecret != "" {
		return nil
	}
	path := agyPath
	if path == "" {
		found, err := exec.LookPath("agy")
		if err != nil {
			return errors.New("agy executable not found; set AGY_AUTH_CLIENT_ID and AGY_AUTH_CLIENT_SECRET, or use --via-agy")
		}
		path = found
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open agy binary: %w", err)
	}
	defer file.Close()

	discoveredID, discoveredSecret, err := discoverClient(file, antigravityClientIDHash, antigravityClientSecretHash)
	if err != nil {
		return fmt.Errorf("discover the Antigravity OAuth client from %s: %w", path, err)
	}
	if ClientID == "" {
		ClientID = discoveredID
	}
	if ClientSecret == "" {
		ClientSecret = discoveredSecret
	}
	return nil
}

func discoverClient(reader io.Reader, wantedIDHash, wantedSecretHash string) (string, string, error) {
	ids := make(map[string]bool)
	secrets := make(map[string]bool)
	buf := make([]byte, 1<<20)
	carry := make([]byte, 0, discoveryCarry)

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, 0, len(carry)+n)
			chunk = append(chunk, carry...)
			chunk = append(chunk, buf[:n]...)
			collectCandidates(chunk, ids, secrets)
			if len(chunk) > discoveryCarry {
				carry = append(carry[:0], chunk[len(chunk)-discoveryCarry:]...)
			} else {
				carry = append(carry[:0], chunk...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
	}

	var clientID, clientSecret string
	for candidate := range ids {
		if hashString(candidate) == wantedIDHash {
			clientID = candidate
			break
		}
	}
	for candidate := range secrets {
		if hashString(candidate) == wantedSecretHash {
			clientSecret = candidate
			break
		}
	}
	if clientID == "" {
		return "", "", errors.New("Antigravity OAuth client ID not found")
	}
	if clientSecret == "" {
		return "", "", errors.New("Antigravity OAuth client secret not found")
	}
	return clientID, clientSecret, nil
}

func collectCandidates(chunk []byte, ids, secrets map[string]bool) {
	for offset := 0; ; {
		index := bytes.Index(chunk[offset:], []byte(clientSecretPrefix))
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(clientSecretPrefix)
		for end < len(chunk) && isSecretChar(chunk[end]) {
			end++
		}
		run := chunk[start:end]
		if len(run) > discoveryCarry {
			run = run[:discoveryCarry]
		}
		for length := minClientSecretLength; length <= len(run); length++ {
			secrets[string(run[:length])] = true
		}
		offset = end
	}

	for offset := 0; ; {
		index := bytes.Index(chunk[offset:], []byte(clientIDSuffix))
		if index < 0 {
			break
		}
		suffixStart := offset + index
		start := suffixStart
		for start > 0 && isClientIDChar(chunk[start-1]) {
			start--
		}
		run := chunk[start:suffixStart]
		if len(run) > discoveryCarry {
			run = run[len(run)-discoveryCarry:]
		}
		end := suffixStart + len(clientIDSuffix)
		for length := minClientIDLength; length <= len(run); length++ {
			candidate := string(run[len(run)-length:]) + clientIDSuffix
			ids[candidate] = true
		}
		offset = end
	}
}

func isSecretChar(value byte) bool {
	return (value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9') ||
		value == '-' || value == '_'
}

func isClientIDChar(value byte) bool {
	return (value >= 'a' && value <= 'z') ||
		(value >= '0' && value <= '9') ||
		value == '-'
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
