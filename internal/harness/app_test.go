package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProfile(t *testing.T) {
	cases := []struct {
		args    []string
		profile string
		ok      bool
	}{
		{nil, "fast", true},
		{[]string{"--profile", "full"}, "full", true},
		{[]string{"--profile=ci"}, "ci", true},
		{[]string{"--profile", "nope"}, "", false},
		{[]string{"--skip-tests"}, "", false},
		{[]string{"extra"}, "", false},
	}
	for _, test := range cases {
		profile, err := parseProfile(test.args)
		if test.ok && (err != nil || profile != test.profile) {
			t.Errorf("parseProfile(%v) = %q, %v", test.args, profile, err)
		}
		if !test.ok && err == nil {
			t.Errorf("parseProfile(%v) accepted invalid input", test.args)
		}
	}
}

func TestCommitMsgCommand(t *testing.T) {
	root := t.TempDir()
	app := NewWithIO(root, bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})

	good := filepath.Join(root, "good")
	if err := os.WriteFile(good, []byte("feat(harness): add profiles\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"commit-msg", good}); code != 0 {
		t.Fatalf("valid commit-msg exit = %d", code)
	}

	bad := filepath.Join(root, "bad")
	if err := os.WriteFile(bad, []byte("Add profiles\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run([]string{"commit-msg", bad}); code != 1 {
		t.Fatalf("invalid commit-msg exit = %d", code)
	}
}
