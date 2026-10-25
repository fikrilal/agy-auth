package docs

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Finding struct {
	File    string
	Line    int
	Link    string
	Message string
}

var linkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func Check(root string) ([]Finding, error) {
	files, err := markdownFiles(root)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(raw), "\n")
		inFence := false
		for index, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, match := range linkPattern.FindAllStringSubmatch(line, -1) {
				link := strings.TrimSpace(match[1])
				if skipLink(link) {
					continue
				}
				target := link
				if hash := strings.IndexByte(target, '#'); hash >= 0 {
					target = target[:hash]
				}
				if target == "" {
					continue
				}
				if decoded, err := url.PathUnescape(target); err == nil {
					target = decoded
				}
				resolved := filepath.Join(filepath.Dir(path), filepath.FromSlash(target))
				if _, err := os.Stat(resolved); err != nil {
					findings = append(findings, Finding{
						File:    rel,
						Line:    index + 1,
						Link:    link,
						Message: "relative link target does not exist",
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

func markdownFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			for _, prefix := range []string{".git", "node_modules", "bin", "dist", "coverage", "npm/vendor", "_WIP"} {
				if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func skipLink(link string) bool {
	lower := strings.ToLower(link)
	return link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "file://")
}

func Format(finding Finding) string {
	return fmt.Sprintf("%s:%d: %s (%s)", finding.File, finding.Line, finding.Message, finding.Link)
}
