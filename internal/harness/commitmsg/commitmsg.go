package commitmsg

import (
	"regexp"
	"strings"
)

var conventional = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: .+$`)
var botMention = regexp.MustCompile(`(?i)commandcodebot`)
var botTrailer = regexp.MustCompile(`(?im)^co-authored-by:.*(\[bot\]|bot@)`)

// Validate enforces the repository commit policy: Conventional Commits with a
// lowercase scope, and no bot co-author trailers or mentions.
func Validate(message string) []string {
	var findings []string
	var content []string
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		content = append(content, line)
	}
	if len(content) == 0 {
		return []string{"commit message is empty"}
	}
	subject := strings.TrimSpace(content[0])
	if len(subject) > 100 {
		findings = append(findings, "subject must be 100 characters or fewer")
	}
	if !isAllowedSpecial(subject) && !conventional.MatchString(subject) {
		findings = append(findings, "subject must use Conventional Commits, for example: feat(harness): add verification profiles")
	}
	if botMention.MatchString(message) {
		findings = append(findings, "bot co-author mentions are not allowed; the user is the only author")
	}
	if botTrailer.MatchString(message) {
		findings = append(findings, "bot co-author trailers are not allowed; the user is the only author")
	}
	return findings
}

func isAllowedSpecial(subject string) bool {
	for _, prefix := range []string{"Merge ", "Revert ", "fixup! ", "squash! "} {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return false
}
