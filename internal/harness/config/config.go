package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

var goVersionPattern = regexp.MustCompile(`(?m)^var Version = "([^"]+)"`)
var changelogVersionPattern = regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)`)

func PackageVersion(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", err
	}
	var pkg packageJSON
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("parse package.json: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("package.json has no version")
	}
	return pkg.Version, nil
}

func GoCLIVersion(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "internal", "cli", "app.go"))
	if err != nil {
		return "", err
	}
	match := goVersionPattern.FindSubmatch(raw)
	if len(match) != 2 {
		return "", fmt.Errorf("internal/cli/app.go has no Version literal")
	}
	return string(match[1]), nil
}

func ChangelogVersions(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		return nil, err
	}
	matches := changelogVersionPattern.FindAllSubmatch(raw, -1)
	versions := make([]string, 0, len(matches))
	for _, match := range matches {
		versions = append(versions, string(match[1]))
	}
	return versions, nil
}

// Check keeps the Go CLI default version, the npm package version, and the
// changelog in sync.
func Check(root string) error {
	pkgVersion, err := PackageVersion(root)
	if err != nil {
		return err
	}
	goVersion, err := GoCLIVersion(root)
	if err != nil {
		return err
	}
	if pkgVersion != goVersion {
		return fmt.Errorf("version drift: package.json=%s internal/cli/app.go=%s", pkgVersion, goVersion)
	}
	versions, err := ChangelogVersions(root)
	if err != nil {
		return err
	}
	for _, version := range versions {
		if version == pkgVersion {
			return nil
		}
	}
	return fmt.Errorf("CHANGELOG.md has no entry for %s (found %s)", pkgVersion, strings.Join(versions, ", "))
}
