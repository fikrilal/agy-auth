package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
)

var usageTestNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func usageTestToken(expiry time.Time) *agy.TokenFile {
	return &agy.TokenFile{
		Token: agy.Token{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			TokenType:    "Bearer",
			Expiry:       expiry.Format(time.RFC3339Nano),
		},
		AuthMethod: "consumer",
	}
}

func TestFetchPlanAndSummary(t *testing.T) {
	var requests []struct {
		method string
		auth   string
		ua     string
		body   map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, struct {
			method string
			auth   string
			ua     string
			body   map[string]any
		}{request.URL.Path, request.Header.Get("Authorization"), request.Header.Get("User-Agent"), body})
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, ":loadCodeAssist"):
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"currentTier":             map[string]any{"id": "free-tier", "name": "Antigravity"},
				"paidTier":                map[string]any{"id": "g1-pro-tier", "name": "Google AI Pro"},
				"cloudaicompanionProject": "aicode-consumers",
			})
		case strings.HasSuffix(request.URL.Path, ":retrieveUserQuotaSummary"):
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"groups": []any{
					map[string]any{"buckets": []any{
						map[string]any{"bucketId": "3p-weekly", "window": "weekly", "remainingFraction": 1, "resetTime": "2026-10-29T13:11:00Z"},
						map[string]any{"bucketId": "3p-5h", "window": "5h", "remainingFraction": 1, "resetTime": "2026-10-22T18:11:00Z"},
					}},
					map[string]any{"buckets": []any{
						map[string]any{"bucketId": "3p-weekly", "window": "weekly", "remainingFraction": 0.5, "resetTime": "2026-10-29T14:11:00Z"},
						map[string]any{"bucketId": "3p-5h", "window": "5h", "remainingFraction": 0.75, "resetTime": "2026-10-22T19:11:00Z"},
					}},
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL + "/v1internal",
		ProjectID:  "project-1",
		UserAgent:  "antigravity/test",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
	}
	snapshot, err := client.Fetch(context.Background(), "acct_1", usageTestToken(usageTestNow.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Plan != "Pro" {
		t.Errorf("plan = %q", snapshot.Plan)
	}
	if snapshot.FiveHour == nil || snapshot.FiveHour.RemainingPercent != 75 {
		t.Errorf("five hour = %+v", snapshot.FiveHour)
	}
	if snapshot.Weekly == nil || snapshot.Weekly.RemainingPercent != 50 {
		t.Errorf("weekly = %+v", snapshot.Weekly)
	}
	if snapshot.FiveHour.ResetsAt == nil || snapshot.FiveHour.ResetsAt.Hour() != 19 {
		t.Errorf("five hour reset = %v", snapshot.FiveHour.ResetsAt)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, request := range requests {
		if request.auth != "Bearer access-token" {
			t.Errorf("authorization = %q", request.auth)
		}
		if !strings.HasPrefix(request.ua, "antigravity/") {
			t.Errorf("user agent = %q", request.ua)
		}
		if got := request.body["cloudaicompanionProject"]; got != nil && got != "project-1" {
			t.Errorf("project = %v", got)
		}
		if got := request.body["project"]; got != nil && got != "project-1" {
			t.Errorf("summary project = %v", got)
		}
	}
}

func TestFetchRefreshesExpiredToken(t *testing.T) {
	refreshed := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer refreshed-token" {
			t.Errorf("authorization = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, ":loadCodeAssist") {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"paidTier": map[string]any{"id": "g1-pro-tier"},
			})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{})
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL + "/v1internal",
		ProjectID:  "project-1",
		UserAgent:  "antigravity/test",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
		RefreshToken: func(ctx context.Context, refreshToken string) (string, time.Time, error) {
			refreshed = true
			if refreshToken != "refresh-token" {
				t.Errorf("refresh token = %q", refreshToken)
			}
			return "refreshed-token", usageTestNow.Add(time.Hour), nil
		},
	}
	if _, err := client.Fetch(context.Background(), "acct_1", usageTestToken(usageTestNow.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Error("expired token was not refreshed")
	}
}

func TestFetchDetail(t *testing.T) {
	var quotaBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, ":loadCodeAssist"):
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"paidTier": map[string]any{"id": "g1-pro-tier"},
			})
		case strings.HasSuffix(request.URL.Path, ":retrieveUserQuota"):
			if err := json.NewDecoder(request.Body).Decode(&quotaBody); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"buckets": []any{
					map[string]any{"tokenType": "WTUS", "modelId": "z-model", "remainingFraction": 1, "resetTime": "2026-10-22T18:11:00Z"},
					map[string]any{"tokenType": "WTUS", "modelId": "gemini-2.5-pro", "remainingFraction": 0.75, "resetTime": "2026-10-22T14:11:00Z"},
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL + "/v1internal",
		ProjectID:  "project-1",
		UserAgent:  "antigravity/test",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
	}
	detail, err := client.FetchDetail(context.Background(), usageTestToken(usageTestNow.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Plan != "Pro" {
		t.Errorf("plan = %q", detail.Plan)
	}
	if len(detail.Buckets) != 2 {
		t.Fatalf("buckets = %+v", detail.Buckets)
	}
	if detail.Buckets[0].ModelID != "gemini-2.5-pro" || detail.Buckets[1].ModelID != "z-model" {
		t.Errorf("buckets are not sorted: %+v", detail.Buckets)
	}
	if detail.Buckets[0].RemainingPercent != 75 {
		t.Errorf("remaining = %d", detail.Buckets[0].RemainingPercent)
	}
	if detail.Buckets[0].ResetsAt == nil || detail.Buckets[0].ResetsAt.Hour() != 14 {
		t.Errorf("reset = %v", detail.Buckets[0].ResetsAt)
	}
	if quotaBody["project"] != "project-1" {
		t.Errorf("quota body = %#v", quotaBody)
	}
}

func TestFetchKeepsPlanOnForbiddenQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, ":loadCodeAssist") {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"paidTier": map[string]any{"id": "g1-pro-tier"},
			})
			return
		}
		writer.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]any{"status": "PERMISSION_DENIED"}})
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL + "/v1internal",
		ProjectID:  "project-1",
		UserAgent:  "antigravity/test",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
	}
	snapshot, err := client.Fetch(context.Background(), "acct_1", usageTestToken(usageTestNow.Add(time.Hour)))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v", err)
	}
	if snapshot.Plan != "Pro" {
		t.Errorf("plan = %q", snapshot.Plan)
	}
	if ShortError(err) != "forbidden" {
		t.Errorf("short error = %q", ShortError(err))
	}
}

type fakeUsageProvider struct {
	calls    int
	snapshot Snapshot
	err      error
}

func (f *fakeUsageProvider) Fetch(context.Context, string, *agy.TokenFile) (Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestCacheTTLAndStaleFallback(t *testing.T) {
	now := usageTestNow
	provider := &fakeUsageProvider{snapshot: Snapshot{
		Plan:      "Pro",
		FiveHour:  &Window{RemainingPercent: 80},
		UpdatedAt: now,
	}}
	cache := &Cache{Provider: provider, TTL: time.Minute, Now: func() time.Time { return now }}
	token := usageTestToken(now.Add(time.Hour))

	first, err := cache.Fetch(context.Background(), "acct_1", token)
	if err != nil || first.FiveHour == nil {
		t.Fatalf("first fetch = %+v, %v", first, err)
	}
	now = now.Add(30 * time.Second)
	second, err := cache.Fetch(context.Background(), "acct_1", token)
	if err != nil || provider.calls != 1 || second.Stale {
		t.Fatalf("cached fetch calls=%d stale=%v err=%v", provider.calls, second.Stale, err)
	}

	now = now.Add(2 * time.Minute)
	provider.err = ErrTimeout
	stale, err := cache.Fetch(context.Background(), "acct_1", token)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("stale error = %v", err)
	}
	if !stale.Stale || stale.FiveHour == nil || stale.FiveHour.RemainingPercent != 80 {
		t.Fatalf("stale snapshot = %+v", stale)
	}
}

func TestCacheSetTTL(t *testing.T) {
	cache := &Cache{TTL: time.Minute}
	cache.SetTTL(5 * time.Second)
	if got := cache.ttl(); got != 5*time.Second {
		t.Errorf("ttl = %s", got)
	}
	cache.SetTTL(0)
	if got := cache.ttl(); got != DefaultTTL {
		t.Errorf("default ttl = %s", got)
	}
}

func TestSummaryChoosesMostConstrainedWindow(t *testing.T) {
	var summary quotaSummaryResponse
	if err := json.Unmarshal([]byte(`{
		"groups": [
			{"buckets": [
				{"bucketId": "3p-5h", "window": "5h", "remainingFraction": 0.8, "resetTime": "2026-10-22T18:11:00Z"},
				{"bucketId": "3p-weekly", "window": "weekly", "remainingFraction": 0.6, "resetTime": "2026-10-29T13:11:00Z"}
			]},
			{"buckets": [
				{"bucketId": "3p-5h", "window": "5h", "remainingFraction": 0.2, "resetTime": "2026-10-22T19:11:00Z"},
				{"bucketId": "3p-weekly", "window": "weekly", "remainingFraction": 0.4, "resetTime": "2026-10-29T14:11:00Z"}
			]}
		]
	}`), &summary); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{}
	applySummary(&snapshot, summary)
	if snapshot.FiveHour == nil || snapshot.FiveHour.RemainingPercent != 20 {
		t.Errorf("five hour = %+v", snapshot.FiveHour)
	}
	if snapshot.Weekly == nil || snapshot.Weekly.RemainingPercent != 40 {
		t.Errorf("weekly = %+v", snapshot.Weekly)
	}
}

func TestShortError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrMissingAuth, "missing_auth"},
		{ErrAuth, "auth"},
		{ErrForbidden, "forbidden"},
		{ErrTimeout, "timeout"},
		{ErrNetwork, "network"},
		{ErrInvalidResponse, "invalid_response"},
		{ErrRefresh, "refresh_failed"},
		{&HTTPError{Status: 500}, "http_500"},
		{errors.New("other"), "unavailable"},
	}
	for _, test := range cases {
		if got := ShortError(test.err); got != test.want {
			t.Errorf("ShortError(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestDefaultProjectAndUserAgent(t *testing.T) {
	t.Setenv("AGY_AUTH_PROJECT_ID", "project-from-env")
	if got := DefaultProjectID(); got != "project-from-env" {
		t.Errorf("project = %q", got)
	}
	if got := DefaultUserAgent("1.2.3"); !strings.HasPrefix(got, "antigravity/agy-auth/1.2.3") {
		t.Errorf("user agent = %q", got)
	}
}
