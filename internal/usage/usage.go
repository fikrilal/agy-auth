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
	"sort"
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
	Group            string     `json:"group,omitempty"`
}

func (s Snapshot) LimitGroup() string {
	switch {
	case s.FiveHour == nil && s.Weekly == nil:
		return ""
	case s.FiveHour == nil:
		return s.Weekly.Group
	case s.Weekly == nil:
		return s.FiveHour.Group
	case s.Weekly.RemainingPercent < s.FiveHour.RemainingPercent:
		return s.Weekly.Group
	case s.FiveHour.RemainingPercent < s.Weekly.RemainingPercent:
		return s.FiveHour.Group
	case s.FiveHour.Group != "":
		return s.FiveHour.Group
	default:
		return s.Weekly.Group
	}
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

type Model struct {
	ModelID          string     `json:"model_id"`
	RemainingPercent int        `json:"remaining_percent"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
	TokenType        string     `json:"token_type,omitempty"`
}

type Detail struct {
	Plan      string    `json:"-"`
	Buckets   []Model   `json:"buckets"`
	UpdatedAt time.Time `json:"updated_at"`
	Stale     bool      `json:"stale,omitempty"`
}

func (d Detail) Lowest() *Model {
	if len(d.Buckets) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(d.Buckets); i++ {
		if d.Buckets[i].RemainingPercent < d.Buckets[best].RemainingPercent {
			best = i
			continue
		}
		if d.Buckets[i].RemainingPercent == d.Buckets[best].RemainingPercent && d.Buckets[i].ResetsAt != nil {
			if d.Buckets[best].ResetsAt == nil || d.Buckets[i].ResetsAt.Before(*d.Buckets[best].ResetsAt) {
				best = i
			}
		}
	}
	return &d.Buckets[best]
}

func (d Detail) Exhausted() bool {
	for _, bucket := range d.Buckets {
		if bucket.RemainingPercent <= 0 {
			return true
		}
	}
	return false
}

type Full struct {
	Snapshot Snapshot
	Detail   Detail
	Groups   Groups
	Err      error
}

type FullProvider interface {
	FetchFull(ctx context.Context, key string, token *agy.TokenFile) (Full, error)
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
	client := "agy-auth"
	if version != "" {
		client += "/" + version
	}
	return fmt.Sprintf("antigravity/cli/1.4.5 (%s; os_type=%s; arch=%s; auth_method=consumer)", client, runtime.GOOS, runtime.GOARCH)
}

func (c *Client) Fetch(ctx context.Context, _ string, token *agy.TokenFile) (Snapshot, error) {
	accessToken, err := c.accessToken(ctx, token)
	if err != nil {
		return Snapshot{}, err
	}
	project, assist, err := c.loadCodeAssist(ctx, accessToken)
	if err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{Plan: planLabel(assist), UpdatedAt: c.now().UTC()}
	var summary quotaSummaryResponse
	status, err := c.post(ctx, "retrieveUserQuotaSummary", quotaSummaryRequest{Project: project}, accessToken, &summary)
	if err != nil {
		return snapshot, err
	}
	if status < http.StatusOK || status > 299 {
		return snapshot, statusError(status)
	}
	applySummary(&snapshot, summary)
	return snapshot, nil
}

func (c *Client) FetchFull(ctx context.Context, _ string, token *agy.TokenFile) (Full, error) {
	accessToken, err := c.accessToken(ctx, token)
	if err != nil {
		return Full{}, err
	}
	project, assist, err := c.loadCodeAssist(ctx, accessToken)
	if err != nil {
		return Full{}, err
	}

	now := c.now().UTC()
	full := Full{
		Snapshot: Snapshot{Plan: planLabel(assist), UpdatedAt: now},
		Detail:   Detail{Plan: planLabel(assist), Buckets: []Model{}, UpdatedAt: now},
		Groups:   Groups{Groups: []Group{}, UpdatedAt: now},
	}
	var summary quotaSummaryResponse
	status, err := c.post(ctx, "retrieveUserQuotaSummary", quotaSummaryRequest{Project: project}, accessToken, &summary)
	switch {
	case err != nil:
		full.Err = err
	case status < http.StatusOK || status > 299:
		full.Err = statusError(status)
	default:
		applySummary(&full.Snapshot, summary)
		full.Groups = groupsFromSummary(summary, now)
	}

	var quota quotaResponse
	status, err = c.post(ctx, "retrieveUserQuota", quotaRequest{Project: project}, accessToken, &quota)
	switch {
	case err != nil:
		if full.Err == nil {
			full.Err = err
		}
	case status < http.StatusOK || status > 299:
		if full.Err == nil {
			full.Err = statusError(status)
		}
	default:
		full.Detail = detailFromQuota(assist, quota, now)
	}
	return full, nil
}

func detailFromQuota(assist codeAssistResponse, quota quotaResponse, now time.Time) Detail {
	detail := Detail{Plan: planLabel(assist), Buckets: []Model{}, UpdatedAt: now}
	for _, bucket := range quota.Buckets {
		if strings.TrimSpace(bucket.ModelID) == "" || bucket.RemainingFraction == nil {
			continue
		}
		model := Model{
			ModelID:          bucket.ModelID,
			RemainingPercent: percent(*bucket.RemainingFraction),
			TokenType:        bucket.TokenType,
		}
		if reset, ok := parseReset(bucket.ResetTime); ok {
			model.ResetsAt = &reset
		}
		detail.Buckets = append(detail.Buckets, model)
	}
	sort.Slice(detail.Buckets, func(i, j int) bool {
		if detail.Buckets[i].ModelID != detail.Buckets[j].ModelID {
			return detail.Buckets[i].ModelID < detail.Buckets[j].ModelID
		}
		return detail.Buckets[i].TokenType < detail.Buckets[j].TokenType
	})
	return detail
}

func (c *Client) accessToken(ctx context.Context, token *agy.TokenFile) (string, error) {
	if token == nil {
		return "", ErrMissingAuth
	}
	accessToken := strings.TrimSpace(token.Token.AccessToken)
	if accessToken == "" || token.Expired(c.now().Add(30*time.Second)) {
		refreshed, _, err := c.refresh(ctx, token)
		if err != nil {
			return "", err
		}
		accessToken = refreshed
	}
	if accessToken == "" {
		return "", ErrMissingAuth
	}
	return accessToken, nil
}

func (c *Client) loadCodeAssist(ctx context.Context, accessToken string) (string, codeAssistResponse, error) {
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
		return project, codeAssistResponse{}, err
	}
	if status < http.StatusOK || status > 299 {
		return project, codeAssistResponse{}, statusError(status)
	}
	return project, assist, nil
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

func (c *Cache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	c.TTL = ttl
	c.mu.Unlock()
}

func (c *Cache) ttl() time.Duration {
	c.mu.Lock()
	ttl := c.TTL
	c.mu.Unlock()
	if ttl > 0 {
		return ttl
	}
	return DefaultTTL
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

type FullCache struct {
	Provider FullProvider
	TTL      time.Duration
	Now      func() time.Time

	mu      sync.Mutex
	entries map[string]fullCacheEntry
}

type fullCacheEntry struct {
	Full    Full
	Fetched time.Time
}

func NewFullCache(provider FullProvider) *FullCache {
	return &FullCache{Provider: provider, TTL: DefaultTTL, Now: time.Now}
}

func (c *FullCache) FetchFull(ctx context.Context, key string, token *agy.TokenFile) (Full, error) {
	if c == nil || c.Provider == nil {
		return Full{}, ErrUnavailable
	}
	if key == "" {
		return c.Provider.FetchFull(ctx, key, token)
	}
	now := c.now()
	c.mu.Lock()
	entry, found := c.entries[key]
	c.mu.Unlock()
	if found && now.Sub(entry.Fetched) < c.ttl() {
		return entry.Full, nil
	}

	full, err := c.Provider.FetchFull(ctx, key, token)
	if err != nil {
		if found {
			entry.Full.Snapshot.Stale = true
			entry.Full.Detail.Stale = true
			entry.Full.Groups.Stale = true
			return entry.Full, err
		}
		return full, err
	}
	if full.Err != nil {
		return full, nil
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]fullCacheEntry{}
	}
	c.entries[key] = fullCacheEntry{Full: full, Fetched: now}
	c.mu.Unlock()
	return full, nil
}

func (c *FullCache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	c.TTL = ttl
	c.mu.Unlock()
}

func (c *FullCache) ttl() time.Duration {
	c.mu.Lock()
	ttl := c.TTL
	c.mu.Unlock()
	if ttl > 0 {
		return ttl
	}
	return DefaultTTL
}

func (c *FullCache) now() time.Time {
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

type quotaRequest struct {
	Project string `json:"project,omitempty"`
}

type quotaResponse struct {
	Buckets []struct {
		TokenType         string   `json:"tokenType"`
		ModelID           string   `json:"modelId"`
		RemainingFraction *float64 `json:"remainingFraction"`
		ResetTime         string   `json:"resetTime"`
	} `json:"buckets"`
}

type quotaSummaryResponse struct {
	Description string `json:"description"`
	Groups      []struct {
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Buckets     []struct {
			BucketID          string   `json:"bucketId"`
			DisplayName       string   `json:"displayName"`
			Description       string   `json:"description"`
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
			candidate := Window{RemainingPercent: percent(*bucket.RemainingFraction), Group: group.DisplayName}
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

func percent(fraction float64) int {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	return int(math.Round(fraction * 100))
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
