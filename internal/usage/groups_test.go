package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, ":loadCodeAssist"):
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"cloudaicompanionProject": "project-1",
				"paidTier":                map[string]any{"id": "g1-pro-tier"},
			})
		case strings.HasSuffix(request.URL.Path, ":retrieveUserQuotaSummary"):
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"description": "Within each group, models share a weekly limit and a 5-hour limit.",
				"groups": []any{
					map[string]any{
						"displayName": "Gemini Models",
						"description": "Models within this group: Gemini Flash, Gemini Pro",
						"buckets": []any{
							map[string]any{"bucketId": "gemini-weekly", "displayName": "Weekly Limit Remaining", "window": "weekly", "remainingFraction": 0.58, "resetTime": "2026-10-24T02:39:46Z"},
							map[string]any{"bucketId": "gemini-5h", "displayName": "Five Hour Limit Remaining", "window": "5h", "remainingFraction": 1, "resetTime": "2026-10-22T22:32:29Z"},
						},
					},
					map[string]any{
						"displayName": "Claude and GPT models",
						"description": "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
						"buckets": []any{
							map[string]any{"bucketId": "3p-weekly", "displayName": "Weekly Limit Remaining", "window": "weekly", "remainingFraction": 1, "resetTime": "2026-10-29T17:32:29Z"},
							map[string]any{"bucketId": "3p-5h", "displayName": "Five Hour Limit Remaining", "window": "5h", "remainingFraction": 1, "resetTime": "2026-10-22T22:32:29Z"},
						},
					},
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
		UserAgent:  DefaultUserAgent("0.1.0"),
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
	}
	groups, err := client.FetchGroups(context.Background(), usageTestToken(usageTestNow.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups.Groups) != 2 {
		t.Fatalf("groups = %+v", groups.Groups)
	}
	if groups.Groups[0].Name != "Gemini Models" || len(groups.Groups[0].Buckets) != 2 {
		t.Errorf("gemini group = %+v", groups.Groups[0])
	}
	if groups.Groups[0].Buckets[0].RemainingPercent != 58 {
		t.Errorf("weekly percent = %d", groups.Groups[0].Buckets[0].RemainingPercent)
	}
	if groups.Groups[1].Name != "Claude and GPT models" || groups.Groups[1].Buckets[1].RemainingPercent != 100 {
		t.Errorf("claude group = %+v", groups.Groups[1])
	}
	if groups.UpdatedAt.IsZero() || groups.Description == "" {
		t.Errorf("groups metadata = %+v", groups)
	}
}

func TestFetchGroupsRequestUsesCLIUserAgent(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		userAgent = request.Header.Get("User-Agent")
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, ":loadCodeAssist") {
			_ = json.NewEncoder(writer).Encode(map[string]any{"cloudaicompanionProject": "project-1"})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"groups": []any{}})
	}))
	defer server.Close()

	client := &Client{
		Endpoint:   server.URL + "/v1internal",
		ProjectID:  "project-1",
		UserAgent:  DefaultUserAgent("0.1.0"),
		HTTPClient: server.Client(),
		Now:        func() time.Time { return usageTestNow },
	}
	if _, err := client.FetchGroups(context.Background(), usageTestToken(usageTestNow.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(userAgent, "antigravity/cli/1.4.5") || !strings.Contains(userAgent, "agy-auth/0.1.0") {
		t.Errorf("user agent = %q", userAgent)
	}
}
