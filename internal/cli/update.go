package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultUpdateURL    = "https://registry.npmjs.org/%40fikrilal%2Fagy-auth/latest"
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 2 * time.Second
	noUpdateCheckEnv    = "AGY_AUTH_NO_UPDATE_CHECK"
	updateInstructions  = "npm install -g @fikrilal/agy-auth@latest\n  go install github.com/fikrilal/agy-auth/cmd/agy-auth@latest"
	updateCacheFilename = "update-check.json"
	updateResponseLimit = 1 << 20
)

type updateStatus struct {
	Current   string     `json:"current"`
	Latest    string     `json:"latest,omitempty"`
	Available bool       `json:"update_available"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

func (a *App) checkForUpdate(force bool) (updateStatus, error) {
	status := updateStatus{Current: Version}
	if a.UpdateURL == "" {
		return status, errors.New("update checks are not configured")
	}
	now := a.now()
	cached, cacheOK := a.readUpdateCache()
	if !force && cacheOK && !cached.CheckedAt.IsZero() {
		elapsed := now.Sub(cached.CheckedAt)
		if elapsed >= 0 && elapsed < updateCheckInterval {
			return a.updateStatus(cached.Latest, cached.CheckedAt), nil
		}
	}

	latest, err := fetchLatestVersion(a.UpdateURL)
	checkedAt := now
	if err != nil {
		a.writeUpdateCache(updateCache{CheckedAt: checkedAt, Latest: cached.Latest})
		return a.updateStatus(cached.Latest, checkedAt), err
	}
	a.writeUpdateCache(updateCache{CheckedAt: checkedAt, Latest: latest})
	return a.updateStatus(latest, checkedAt), nil
}

func (a *App) updateStatus(latest string, checkedAt time.Time) updateStatus {
	status := updateStatus{Current: Version, Latest: latest, Available: versionNewer(latest, Version)}
	if !checkedAt.IsZero() {
		value := checkedAt
		status.CheckedAt = &value
	}
	return status
}

func (a *App) maybeNotifyUpdate() {
	if a.JSON || a.UpdateURL == "" || os.Getenv(noUpdateCheckEnv) != "" {
		return
	}
	status, err := a.checkForUpdate(false)
	if err != nil || !status.Available {
		return
	}
	fmt.Fprintf(a.Err, "\nupdate available: %s -> %s\n  %s\n", status.Current, status.Latest, updateInstructions)
}

func (a *App) readUpdateCache() (updateCache, bool) {
	if a.Store == nil {
		return updateCache{}, false
	}
	raw, err := os.ReadFile(filepath.Join(a.Store.Dir, updateCacheFilename))
	if err != nil {
		return updateCache{}, false
	}
	var cache updateCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return updateCache{}, false
	}
	return cache, true
}

func (a *App) writeUpdateCache(cache updateCache) {
	if a.Store == nil {
		return
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		return
	}
	if err := os.MkdirAll(a.Store.Dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(a.Store.Dir, updateCacheFilename), append(raw, '\n'), 0o600)
}

func fetchLatestVersion(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build update request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "agy-auth/"+Version)
	client := &http.Client{Timeout: updateCheckTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("check update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check update: HTTP %d", response.StatusCode)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, updateResponseLimit)).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode update response: %w", err)
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", errors.New("update response has no version")
	}
	return version, nil
}

func versionNewer(latest, current string) bool {
	latestParts, latestOK := parseVersion(latest)
	currentParts, currentOK := parseVersion(current)
	if !latestOK || !currentOK {
		return false
	}
	for i := range latestParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	value = strings.SplitN(value, "-", 2)[0]
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var result [3]int
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return [3]int{}, false
		}
		result[i] = number
	}
	return result, true
}

func shouldCheckForUpdates(command string, args []string) bool {
	for _, arg := range args {
		if arg == "--live" {
			return false
		}
	}
	switch canonicalCommand(command) {
	case "list", "switch", "status", "usage":
		return true
	default:
		return false
	}
}
