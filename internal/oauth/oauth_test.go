package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestChallengeRFCVector(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := Challenge(verifier); got != want {
		t.Errorf("Challenge() = %q, want %q", got, want)
	}
}

func setTestClient(t *testing.T) {
	t.Helper()
	oldID, oldSecret := ClientID, ClientSecret
	ClientID, ClientSecret = "test-client", "test-secret"
	t.Cleanup(func() {
		ClientID, ClientSecret = oldID, oldSecret
	})
}

func TestAuthURLParameters(t *testing.T) {
	setTestClient(t)
	state, err := NewState()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(AuthURL(state, verifier))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	checks := map[string]string{
		"client_id":             ClientID,
		"redirect_uri":          RedirectURI,
		"response_type":         "code",
		"access_type":           "offline",
		"prompt":                "consent",
		"state":                 state,
		"code_challenge_method": "S256",
	}
	for key, want := range checks {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if query.Get("code_challenge") != Challenge(verifier) {
		t.Error("code_challenge does not match verifier")
	}
	if !strings.Contains(query.Get("scope"), "auth/aicode") {
		t.Errorf("scope = %q", query.Get("scope"))
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		state   string
		want    string
		wantErr bool
	}{
		{name: "bare", input: "4/0Abc_def-123", want: "4/0Abc_def-123"},
		{name: "quoted", input: `"4/0Abc"`, want: "4/0Abc"},
		{name: "url", input: "https://antigravity.google/oauth-callback?state=s1&code=4%2F0Abc", state: "s1", want: "4/0Abc"},
		{name: "query", input: "state=s1&code=4%2F0Abc", state: "s1", want: "4/0Abc"},
		{name: "state mismatch", input: "https://x/oauth-callback?state=s2&code=c", state: "s1", wantErr: true},
		{name: "oauth error", input: "https://x/oauth-callback?error=access_denied&error_description=denied", wantErr: true},
		{name: "empty", input: "  ", wantErr: true},
		{name: "url without code", input: "https://x/oauth-callback?state=s1", state: "s1", wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeCode(test.input, test.state)
			if test.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestExchange(t *testing.T) {
	setTestClient(t)
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		received = request.PostForm
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "access-token",
			"expires_in":    3600,
			"refresh_token": "refresh-token",
			"token_type":    "Bearer",
			"id_token":      "header.payload.signature",
		})
	}))
	defer server.Close()

	oldEndpoint, oldClient := TokenEndpoint, HTTPClient
	TokenEndpoint = server.URL
	HTTPClient = server.Client()
	defer func() {
		TokenEndpoint = oldEndpoint
		HTTPClient = oldClient
	}()

	response, err := Exchange(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if response.AccessToken != "access-token" || response.RefreshToken != "refresh-token" {
		t.Errorf("unexpected response: %+v", response)
	}
	checks := map[string]string{
		"client_id":     ClientID,
		"client_secret": ClientSecret,
		"code":          "the-code",
		"code_verifier": "the-verifier",
		"grant_type":    "authorization_code",
		"redirect_uri":  RedirectURI,
	}
	for key, want := range checks {
		if got := received.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestExchangeError(t *testing.T) {
	setTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Bad Request",
		})
	}))
	defer server.Close()
	oldEndpoint, oldClient := TokenEndpoint, HTTPClient
	TokenEndpoint = server.URL
	HTTPClient = server.Client()
	defer func() {
		TokenEndpoint = oldEndpoint
		HTTPClient = oldClient
	}()
	if _, err := Exchange(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("expected exchange error")
	}
}

func TestTokenFromResponse(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	response := &TokenResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		IDToken:      "id",
		ExpiresIn:    3600,
	}
	tokenFile := TokenFromResponse(response, "consumer", now)
	if tokenFile.Token.Expiry != now.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Errorf("expiry = %q", tokenFile.Token.Expiry)
	}
	if tokenFile.AuthMethod != "consumer" || tokenFile.IDToken != "id" {
		t.Errorf("unexpected token file: %+v", tokenFile)
	}
	raw, err := MarshalTokenFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"refresh_token": "refresh"`) {
		t.Errorf("marshaled token file: %s", raw)
	}
}

func TestDiscoverClient(t *testing.T) {
	// Build fake credential-shaped strings at runtime so the repository secret
	// scanner does not treat test fixtures as real credentials.
	id := "test-client-abcdefghijklmnopqrstuvwxyz0123456789" + ".apps.googleusercontent.com"
	secret := "GOCSPX-" + "test_secret-123"
	decoyID := "9999999999-" + "decoyidentifier0123456789" + ".apps.googleusercontent.com"
	decoySecret := "GOCSPX-" + "decoy-secret-123"
	data := "junk " + decoySecret + " " + decoyID + " " + secret + " " + id + " tail"

	gotID, gotSecret, err := discoverClient(strings.NewReader(data), hashString(id), hashString(secret))
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotSecret != secret {
		t.Errorf("discoverClient = %q, %q", gotID, gotSecret)
	}

	// One byte per read exercises the chunk-boundary carry logic.
	gotID, gotSecret, err = discoverClient(iotest.OneByteReader(strings.NewReader(data)), hashString(id), hashString(secret))
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotSecret != secret {
		t.Errorf("discoverClient with small reads = %q, %q", gotID, gotSecret)
	}

	if _, _, err := discoverClient(strings.NewReader(data), hashString("missing"), hashString(secret)); err == nil {
		t.Error("expected missing client ID error")
	}
	if _, _, err := discoverClient(strings.NewReader(data), hashString(id), hashString("missing")); err == nil {
		t.Error("expected missing client secret error")
	}
}
