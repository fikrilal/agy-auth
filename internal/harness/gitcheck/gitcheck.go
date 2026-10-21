package gitcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Finding struct {
	Commit string
	Reason string
}

var botMention = regexp.MustCompile(`(?i)commandcodebot`)
var botTrailer = regexp.MustCompile(`(?im)^co-authored-by:.*(\[bot\]|bot@)`)

// CheckBotTrailers scans commit messages in the current history. It reports
// only the commit and the reason, never the message body.
func CheckBotTrailers(root string) ([]Finding, error) {
	command := exec.Command("git", "log", "--format=%H%x00%B%x00")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	parts := strings.Split(string(output), "\x00")
	var findings []Finding
	for index := 0; index+1 < len(parts); index += 2 {
		hash := strings.TrimSpace(parts[index])
		message := parts[index+1]
		if hash == "" {
			continue
		}
		if botMention.MatchString(message) {
			findings = append(findings, Finding{Commit: shortHash(hash), Reason: "commit message mentions a bot co-author"})
			continue
		}
		if botTrailer.MatchString(message) {
			findings = append(findings, Finding{Commit: shortHash(hash), Reason: "commit message has a bot co-author trailer"})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Commit < findings[j].Commit })
	return findings, nil
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// InstallHooks points git at the checked-in hooks and makes them executable.
func InstallHooks(root string) error {
	command := exec.Command("git", "config", "core.hooksPath", ".githooks")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git config: %s", strings.TrimSpace(string(output)))
	}
	for _, name := range []string{"pre-commit", "commit-msg"} {
		path := filepath.Join(root, ".githooks", name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("missing hook %s: %w", name, err)
		}
		if err := os.Chmod(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}
