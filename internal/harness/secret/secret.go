package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type Finding struct {
	Rule    string
	File    string
	Line    int
	Column  int
	Message string
}

type pattern struct {
	id      string
	message string
	re      *regexp.Regexp
}

var patterns = []pattern{
	{"google-oauth-client-secret", "Google OAuth client secret", regexp.MustCompile(`GOCSPX-[A-Za-z0-9_-]{10,}`)},
	{"google-oauth-client-id", "Google OAuth client ID", regexp.MustCompile(`[0-9]{10,}-[a-z0-9]{20,}\.apps\.googleusercontent\.com`)},
	{"npm-token", "npm access token", regexp.MustCompile(`npm_[A-Za-z0-9]{30,}`)},
	{"github-token", "GitHub access token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`)},
	{"github-fine-grained-token", "GitHub fine-grained token", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{30,}`)},
	{"openai-key", "OpenAI-style API key", regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{"aws-access-key", "AWS access key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"slack-token", "Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"private-key", "private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".pdf": true, ".zip": true, ".gz": true, ".tgz": true,
	".exe": true, ".bin": true, ".wasm": true, ".db": true, ".sqlite": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
}

func Scan(root string, files []string) ([]Finding, error) {
	var findings []Finding
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if skip(rel) || binaryExtensions[strings.ToLower(filepath.Ext(rel))] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() > 2<<20 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for _, item := range patterns {
			for lineIndex, line := range lines {
				for _, match := range item.re.FindAllStringIndex(line, -1) {
					findings = append(findings, Finding{
						Rule:    item.id,
						File:    rel,
						Line:    lineIndex + 1,
						Column:  match[0] + 1,
						Message: item.message,
					})
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

func skip(rel string) bool {
	for _, prefix := range []string{"_WIP/", "npm/vendor/", "bin/", "dist/", "coverage/", "node_modules/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return rel == "go.sum" || rel == "package-lock.json"
}
