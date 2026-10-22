package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/oauth"
)

const (
	DefaultEndpoint    = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	FallbackProjectID  = "default-cli-project"
	DefaultTTL         = 60 * time.Second
	defaultHTTPTimeout = 15 * time.Second
	maxResponseBytes   = 1 << 20
)

var (
	ErrMissingAuth     = errors.New("missing credential")
	ErrAuth            = errors.New("authentication failed")
	ErrForbidden       = errors.New("quota access forbidden")
	ErrUnsupported     = errors.New("quota unsupported")
	ErrTimeout         = errors.New("quota request timed out")
	ErrNetwork         = errors.New("quota network failure")
	ErrInvalidResponse = errors.New("invalid quota response")
	ErrRefresh         = errors.New("token refresh failed")
	ErrHTTP            = errors.New("quota HTTP error")
	ErrUnavailable     = errors.New("quota provider unavailable")
)

type Window struct {
	RemainingPercent int        `json:"remaining_percent"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
}

type Snapshot struct {
	Plan      string    `json:"-"`
	FiveHour  *Window   `json:"five_hour,omitempty"`
	Weekly    *Window   `json:"weekly,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Stale     bool      `json:"stale,omitempty"`
}

type Provider interface {
	Fetch(ctx context.Context, key string, token *agy.TokenFile) (Snapshot, error)
}

type Client struct {
	Endpoint     string
	ProjectID    string
	UserAgent    string
	HTTPClient   *http.Client
	Now          func() time.Time
	RefreshToken func(ctx context.Context, refreshToken string) (string, time.Time, error)

	ensureOnce sync.Once
	ensureErr  error
}

func NewClient(version string) *Client {
	return &Client{
		Endpoint:   DefaultEndpoint,
		ProjectID:  DefaultProjectID(),
		UserAgent:  DefaultUserAgent(version),
		HTTPClient: &http.Client{Timeout: defaultHTTPTimeout},
		Now:        time.Now,
	}
}

func DefaultProjectID() string {
	for _, key := range []string{"AGY_AUTH_PROJECT_ID", "ANTIGRAVITY_PROJECT_ID", "GEMINI_DEFAULT_PROJECT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".gemini", "antigravity-cli", "cache", "default_project_id.txt")
		if raw, readErr := os.ReadFile(path); readErr == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				return value
			}
		}
	}
	return FallbackProjectID
}

func DefaultUserAgent(version string) string {
	if version == "" {
		return fmt.Sprintf("antigravity/agy-auth (%s; %s)", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("antigravity/agy-auth/%s (%s; %s)", version, runtime.GOOS, runtime.GOARCH)
}

func (c *Client) Fetch(ctx context.Context, _ string, token *agy.TokenFile) (Snapshot, error) {
	if token == nil {
		return Snapshot{}, ErrMissingAuth
	}
	now := c.now()
	accessToken := strings.TrimSpace(token.Token.AccessToken)
	if accessToken == "" || token.Expired(now.Add(30*time.Second)) {
		refreshed, _, err := c.refresh(ctx, token)
		if err != nil {
			return Snapshot{}, err
		}
		accessToken = refreshed
	}
	if accessToken == "" {
		return Snapshot{}, ErrMissingAuth
	}

	project := strings.TrimSpace(c.ProjectID)
	if project == "" {
		project = FallbackProjectID
	}
	var assist codeAssistResponse
	status, err := c.post(ctx, "loadCodeAssist", codeAssistRequest{
		CloudAICompanionProject: project,
		Metadata: codeAssistMetadata{
			IDEType:     "IDE_UNSPECIFIED",
			Platform:    "PLATFORM_UNSPECIFIED",
			PluginType:  "GEMINI",
			DuetProject: project,
		},
	}, accessToken, &assist)
	if err != nil {
		return Snapshot{}, err
	}
	if status < http.StatusOK || status > 299 {
		return Snapshot{}, statusError(status)
	}

	snapshot := Snapshot{Plan: planLabel(assist), UpdatedAt: now.UTC()}
	var summary quotaSummaryResponse
	status, err = c.post(ctx, "retrieveUserQuotaSummary", quotaSummaryRequest{Project: project}, accessToken, &summary)
	if err != nil {
		return snapshot, err
	}
	if status < http.StatusOK || status > 299 {
		return snapshot, statusError(status)
	}
	applySummary(&snapshot, summary)
	return snapshot, nil
}

func (c *Client) post(ctx context.Context, method string, body any, accessToken string, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, ErrInvalidResponse
	}
	endpoint := strings.TrimRight(c.Endpoint, "/")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+":"+method, strings.NewReader(string(raw)))
	if err != nil {
		return 0, ErrNetwork
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		request.Header.Set("User-Agent", c.UserAgent)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, transportError(err)
	}
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return response.StatusCode, ErrNetwork
	}
	if response.StatusCode < http.StatusOK || response.StatusCode > 299 {
		return response.StatusCode, nil
	}
	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return response.StatusCode, ErrInvalidResponse
	}
	return response.StatusCode, nil
}

func (c *Client) refresh(ctx context.Context, token *agy.TokenFile) (string, time.Time, error) {
	refreshToken := strings.TrimSpace(token.Token.RefreshToken)
	if refreshToken == "" {
		return "", time.Time{}, ErrMissingAuth
	}
	if c.RefreshToken != nil {
		accessToken, expiry, err := c.RefreshToken(ctx, refreshToken)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("%w: %v", ErrRefresh, err)
		}
		return accessToken, expiry, nil
	}

	c.ensureOnce.Do(func() {
		c.ensureErr = oauth.EnsureClient("")
	})
	if c.ensureErr != nil {
		return "", time.Time{}, fmt.Errorf("%w: %v", ErrRefresh, c.ensureErr)
	}
	response, err := oauth.Refresh(ctx, refreshToken)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %v", ErrRefresh, err)
	}
	expiresIn := response.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return response.AccessToken, c.now().Add(time.Duration(expiresIn) * time.Second), nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func transportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrTimeout
	}
	return ErrNetwork
}

type HTTPError struct {
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("quota request failed (HTTP %d)", e.Status)
}

func (e *HTTPError) Unwrap() error {
	return ErrHTTP
}

func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return ErrAuth
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrUnsupported
	default:
		return &HTTPError{Status: status}
	}
}

func ShortError(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *HTTPError
	switch {
	case errors.As(err, &httpErr):
		return fmt.Sprintf("http_%d", httpErr.Status)
	case errors.Is(err, ErrMissingAuth):
		return "missing_auth"
	case errors.Is(err, ErrAuth):
		return "auth"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrNetwork):
		return "network"
	case errors.Is(err, ErrInvalidResponse):
		return "invalid_response"
	case errors.Is(err, ErrRefresh):
		return "refresh_failed"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	default:
		return "unavailable"
	}
}

type Cache struct {
	Provider Provider
	TTL      time.Duration
	Now      func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	Snapshot Snapshot
	Fetched  time.Time
}

func NewCache(provider Provider) *Cache {
	return &Cache{Provider: provider, TTL: DefaultTTL, Now: time.Now}
}

func (c *Cache) Fetch(ctx context.Context, key string, token *agy.TokenFile) (Snapshot, error) {
	if c == nil || c.Provider == nil {
		return Snapshot{}, ErrUnavailable
	}
	if key == "" {
		return c.Provider.Fetch(ctx, key, token)
	}
	now := c.now()
	c.mu.Lock()
	entry, found := c.entries[key]
	c.mu.Unlock()
	if found && now.Sub(entry.Fetched) < c.ttl() {
		return entry.Snapshot, nil
	}

	snapshot, err := c.Provider.Fetch(ctx, key, token)
	if err != nil {
		if found {
			entry.Snapshot.Stale = true
			return entry.Snapshot, err
		}
		return snapshot, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[key] = cacheEntry{Snapshot: snapshot, Fetched: now}
	c.mu.Unlock()
	return snapshot, nil
}

func (c *Cache) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultTTL
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

type codeAssistRequest struct {
	CloudAICompanionProject string             `json:"cloudaicompanionProject,omitempty"`
	Metadata                codeAssistMetadata `json:"metadata"`
}

type codeAssistMetadata struct {
	IDEType     string `json:"ideType"`
	Platform    string `json:"platform"`
	PluginType  string `json:"pluginType"`
	DuetProject string `json:"duetProject,omitempty"`
}

type codeAssistResponse struct {
	CurrentTier             *tier  `json:"currentTier"`
	PaidTier                *tier  `json:"paidTier"`
	CloudAICompanionProject string `json:"cloudaicompanionProject"`
}

type tier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type quotaSummaryRequest struct {
	Project string `json:"project,omitempty"`
}

type quotaSummaryResponse struct {
	Groups []struct {
		Buckets []struct {
			BucketID          string   `json:"bucketId"`
			Window            string   `json:"window"`
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         string   `json:"resetTime"`
		} `json:"buckets"`
	} `json:"groups"`
}

func applySummary(snapshot *Snapshot, summary quotaSummaryResponse) {
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			if bucket.RemainingFraction == nil {
				continue
			}
			window := bucketWindow(bucket.Window, bucket.BucketID)
			if window == "" {
				continue
			}
			fraction := *bucket.RemainingFraction
			if fraction < 0 {
				fraction = 0
			}
			if fraction > 1 {
				fraction = 1
			}
			candidate := Window{RemainingPercent: int(math.Round(fraction * 100))}
			if reset, ok := parseReset(bucket.ResetTime); ok {
				candidate.ResetsAt = &reset
			}
			switch window {
			case "5h":
				snapshot.FiveHour = lowerWindow(snapshot.FiveHour, candidate)
			case "weekly":
				snapshot.Weekly = lowerWindow(snapshot.Weekly, candidate)
			}
		}
	}
}

func bucketWindow(window, bucketID string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "5-hour", "five_hour", "five-hour":
		return "5h"
	case "weekly", "week":
		return "weekly"
	}
	id := strings.ToLower(bucketID)
	switch {
	case strings.Contains(id, "weekly"):
		return "weekly"
	case strings.Contains(id, "5h"), strings.Contains(id, "five"):
		return "5h"
	default:
		return ""
	}
}

func lowerWindow(current *Window, candidate Window) *Window {
	if current == nil {
		return &candidate
	}
	if candidate.RemainingPercent < current.RemainingPercent {
		return &candidate
	}
	if candidate.RemainingPercent == current.RemainingPercent && candidate.ResetsAt != nil {
		if current.ResetsAt == nil || candidate.ResetsAt.Before(*current.ResetsAt) {
			return &candidate
		}
	}
	return current
}

func parseReset(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func planLabel(response codeAssistResponse) string {
	for _, candidate := range []*tier{response.PaidTier, response.CurrentTier} {
		if candidate == nil {
			continue
		}
		if label := tierLabel(candidate.ID); label != "" {
			return label
		}
		if name := strings.TrimSpace(candidate.Name); name != "" {
			return name
		}
		if id := strings.TrimSpace(candidate.ID); id != "" {
			return id
		}
	}
	return ""
}

func tierLabel(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "free-tier":
		return "Free"
	case "standard-tier":
		return "Standard"
	case "g1-pro-tier":
		return "Pro"
	case "g1-ultra-tier":
		return "Ultra"
	default:
		return ""
	}
}
