package commitmsg

import (
	"strings"
	"testing"
)

func TestValidateAcceptsConventionalCommits(t *testing.T) {
	valid := []string{
		"feat(harness): add verification profiles",
		"fix(cli): handle EOF",
		"docs: explain credential storage",
		"chore(deps): update npm metadata",
		"feat(harness)!: change profile contract",
		"revert: restore previous behavior",
		"Merge branch 'main'",
		"fixup! feat(harness): add profiles",
		"# comment\nfeat(harness): add profiles\n",
	}
	for _, message := range valid {
		if findings := Validate(message); len(findings) > 0 {
			t.Errorf("Validate(%q) = %v", message, findings)
		}
	}
}

func TestValidateRejectsBadMessages(t *testing.T) {
	invalid := []string{
		"",
		"Add harness",
		"feat(Harness): uppercase scope",
		"feature(harness): wrong type",
		"feat(harness):",
	}
	for _, message := range invalid {
		if findings := Validate(message); len(findings) == 0 {
			t.Errorf("Validate(%q) accepted an invalid message", message)
		}
	}
}

func TestValidateRejectsBotAttribution(t *testing.T) {
	cases := []string{
		"feat(harness): add profiles\n\nCo-authored-by: CommandCodeBot <noreply@commandcode.ai>",
		"feat(harness): add profiles\n\nCo-authored-by: dependabot[bot] <dependabot@users.noreply.github.com>",
		"feat(harness): add profiles\n\nCommandCodeBot",
	}
	for _, message := range cases {
		if findings := Validate(message); len(findings) == 0 {
			t.Errorf("Validate(%q) accepted bot attribution", message)
		}
	}
}

func TestValidateRejectsLongSubject(t *testing.T) {
	message := "feat(harness): " + strings.Repeat("x", 100)
	if findings := Validate(message); len(findings) == 0 {
		t.Error("expected a long-subject finding")
	}
}
