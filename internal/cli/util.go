package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (a *App) readLine(prompt string) (string, error) {
	fmt.Fprint(a.promptWriter(), prompt)
	if a.reader == nil {
		a.reader = bufio.NewReader(a.In)
	}
	line, err := a.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if line == "" && errors.Is(err, io.EOF) {
		return "", io.EOF
	}
	return line, nil
}

func (a *App) color(code, value string) string {
	if !a.OutTTY || a.NoColor || os.Getenv("NO_COLOR") != "" {
		return value
	}
	return code + value + "\x1b[0m"
}

func (a *App) green(value string) string  { return a.color("\x1b[32m", value) }
func (a *App) yellow(value string) string { return a.color("\x1b[33m", value) }
func (a *App) red(value string) string    { return a.color("\x1b[31m", value) }
func (a *App) cyan(value string) string   { return a.color("\x1b[36m", value) }
func (a *App) dim(value string) string    { return a.color("\x1b[2m", value) }
func (a *App) bold(value string) string   { return a.color("\x1b[1m", value) }

func humanUntil(target time.Time, now time.Time) string {
	if target.IsZero() {
		return "-"
	}
	delta := target.Sub(now)
	if delta <= 0 {
		return "expired"
	}
	return "in " + humanDuration(delta)
}

func humanDuration(delta time.Duration) string {
	if delta < time.Minute {
		return "<1m"
	}
	delta = delta.Round(time.Minute)
	days := int(delta / (24 * time.Hour))
	delta -= time.Duration(days) * 24 * time.Hour
	hours := int(delta / time.Hour)
	delta -= time.Duration(hours) * time.Hour
	minutes := int(delta / time.Minute)

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func sanitizeFilename(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_', r == '@':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), "-.")
	if result == "" {
		result = "account"
	}
	if len(result) > 80 {
		result = result[:80]
	}
	return result
}

func shortID(id string) string {
	if len(id) <= 9 {
		return id
	}
	return id[:9]
}

func openBrowser(url string) error {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return errors.New("SSH session detected")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Start()
}

// runningAgyCount reports the number of agy processes. It returns -1 when the
// platform does not expose an inexpensive way to check.
func runningAgyCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return -1
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "agy" {
			count++
		}
	}
	return count
}
