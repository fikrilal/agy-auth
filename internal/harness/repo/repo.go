package repo

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	".idea":        true,
	".vscode":      true,
	"bin":          true,
	"build":        true,
	"coverage":     true,
	"dist":         true,
	"node_modules": true,
	"_WIP":         true,
	".harness-tmp": true,
}

// FindRoot walks up from start until it finds go.mod.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// GoFiles returns all Go source files in the repository, excluding generated
// and vendored trees.
func GoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "." {
				return nil
			}
			if skipDirs[entry.Name()] || rel == "npm/vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
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

// TrackedFiles returns tracked plus untracked-but-not-ignored files. It falls
// back to a filesystem walk outside git.
func TrackedFiles(root string) ([]string, error) {
	command := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return walkFiles(root)
	}
	var files []string
	for _, item := range strings.Split(string(output), "\x00") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(item))
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// Rel returns the slash-separated path relative to root.
func Rel(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// Under reports whether rel is inside dir (or equals it).
func Under(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

func IsTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}
