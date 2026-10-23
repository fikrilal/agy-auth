package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultUpdateURL        = "https://registry.npmjs.org/%40fikrilal%2Fagy-auth/latest"
	defaultUpdateReleaseURL = "https://github.com/fikrilal/agy-auth/releases/download"
	updateCheckInterval     = 24 * time.Hour
	updateCheckTimeout      = 2 * time.Second
	updateDownloadTimeout   = 60 * time.Second
	noUpdateCheckEnv        = "AGY_AUTH_NO_UPDATE_CHECK"
	updateInstructions      = "npm install -g @fikrilal/agy-auth@latest\n  go install github.com/fikrilal/agy-auth/cmd/agy-auth@latest"
	updateCacheFilename     = "update-check.json"
	updateResponseLimit     = 1 << 20
	updateBinaryLimit       = 64 << 20
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
	Skipped   string    `json:"skipped,omitempty"`
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
		a.writeUpdateCache(updateCache{CheckedAt: checkedAt, Latest: cached.Latest, Skipped: cached.Skipped})
		return a.updateStatus(cached.Latest, checkedAt), err
	}
	skipped := ""
	if cached.Latest == latest {
		skipped = cached.Skipped
	}
	a.writeUpdateCache(updateCache{CheckedAt: checkedAt, Latest: latest, Skipped: skipped})
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
	cache, _ := a.readUpdateCache()
	if cache.Skipped == status.Latest {
		return
	}
	if !a.TTY || a.Command == "version" || !a.canAutoUpdate() {
		fmt.Fprintf(a.Err, "\nupdate available: %s -> %s\n  %s\n", status.Current, status.Latest, updateInstructions)
		return
	}

	fmt.Fprintf(a.Out, "\nupdate available: %s -> %s\n", status.Current, status.Latest)
	answer, err := a.readLine("Update now? [y/N] ")
	if err != nil || !updateConfirmed(answer) {
		a.skipUpdate(status.Latest)
		fmt.Fprintln(a.Out, "Update skipped for this version.")
		return
	}
	fmt.Fprintf(a.Out, "Updating to %s...\n", status.Latest)
	if err := a.applyUpdate(status); err != nil {
		fmt.Fprintf(a.Err, "update failed: %v\n  %s\n", err, updateInstructions)
		return
	}
	fmt.Fprintf(a.Out, "Updated to %s. Restart running agy-auth instances.\n", status.Latest)
}

func (a *App) skipUpdate(latest string) {
	cache, _ := a.readUpdateCache()
	if cache.CheckedAt.IsZero() {
		cache.CheckedAt = a.now()
	}
	cache.Latest = latest
	cache.Skipped = latest
	a.writeUpdateCache(cache)
}

func updateConfirmed(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func (a *App) canAutoUpdate() bool {
	switch installMethod() {
	case "npm":
		return true
	case "standalone":
		return runtime.GOOS != "windows"
	default:
		return false
	}
}

func installMethod() string {
	executable, err := os.Executable()
	if err != nil {
		return "manual"
	}
	normalized := filepath.ToSlash(executable)
	if strings.Contains(normalized, "/node_modules/@fikrilal/agy-auth/") {
		return "npm"
	}
	return "standalone"
}

func (a *App) applyUpdate(status updateStatus) error {
	switch installMethod() {
	case "npm":
		return a.applyNPMUpdate()
	case "standalone":
		target, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate current binary: %w", err)
		}
		target, err = filepath.EvalSymlinks(target)
		if err != nil {
			return fmt.Errorf("resolve current binary: %w", err)
		}
		base := a.UpdateReleaseURL
		if base == "" {
			base = defaultUpdateReleaseURL
		}
		return installReleaseBinary(base, status.Latest, target)
	default:
		return errors.New("automatic update is not supported for this install")
	}
}

func (a *App) applyNPMUpdate() error {
	command := exec.Command("npm", "install", "-g", "@fikrilal/agy-auth@latest")
	command.Stdin = a.In
	command.Stdout = a.Out
	command.Stderr = a.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("npm update: %w", err)
	}
	return nil
}

func installReleaseBinary(baseURL, version, target string) error {
	asset, ok := releaseAssetName()
	if !ok {
		return errors.New("no release binary for this platform")
	}
	tag := "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	base := strings.TrimRight(baseURL, "/") + "/" + tag
	checksums, err := downloadUpdateFile(base+"/checksums.txt", updateResponseLimit)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	expected := checksumFor(checksums, asset)
	if expected == "" {
		return fmt.Errorf("release has no checksum for %s", asset)
	}
	binary, err := downloadUpdateFile(base+"/"+asset, updateBinaryLimit)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	sum := sha256.Sum256(binary)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return errors.New("downloaded binary checksum does not match")
	}
	return replaceExecutable(target, binary)
}

func releaseAssetName() (string, bool) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", false
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		return "agy-auth-" + runtime.GOOS + "-" + arch, true
	case "windows":
		return "agy-auth-windows-" + arch + ".exe", true
	default:
		return "", false
	}
}

func checksumFor(checksums []byte, asset string) string {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == asset {
			return fields[0]
		}
	}
	return ""
}

func downloadUpdateFile(url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateDownloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "agy-auth/"+Version)
	client := &http.Client{Timeout: updateDownloadTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download is too large")
	}
	return data, nil
}

func replaceExecutable(target string, binary []byte) error {
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".agy-auth-update-*")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, target); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
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
