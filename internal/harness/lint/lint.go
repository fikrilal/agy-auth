package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fikrilal/agy-auth/internal/harness/repo"
)

type Finding struct {
	Rule    string
	File    string
	Line    int
	Column  int
	Message string
}

type RuleInfo struct {
	ID          string
	Description string
}

var Rules = []RuleInfo{
	{"output-boundary", "Production and CLI packages must not print directly; pass writers through App."},
	{"json-boundary", "CLI code must emit JSON through emitJSON, not ad-hoc encoders."},
	{"keyring-boundary", "Only internal/agy/storage.go may talk to the OS keyring directly."},
	{"atomic-writes", "Credential and registry writes must use the atomic write helper."},
	{"architecture", "Internal packages must respect the declared import graph."},
	{"no-panic", "Library packages must return errors instead of panicking."},
	{"no-os-exit", "Only cmd/* main packages may call os.Exit."},
	{"file-permissions", "Credential and registry files must use 0600/0700 modes."},
	{"test-isolation", "Tests must not use the default registry/storage or the real keyring."},
}

type fileInfo struct {
	root   string
	path   string
	rel    string
	dir    string
	isTest bool
	fset   *token.FileSet
	file   *ast.File
	src    []byte
}

// Check parses files and returns all findings sorted by path and position.
func Check(root string, files []string) ([]Finding, error) {
	var findings []Finding
	for _, path := range files {
		rel, err := repo.Rel(root, path)
		if err != nil {
			return nil, err
		}
		if isExcluded(rel) {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		info := &fileInfo{
			root:   root,
			path:   path,
			rel:    rel,
			dir:    filepath.ToSlash(filepath.Dir(rel)),
			isTest: repo.IsTestFile(rel),
			fset:   fset,
			file:   file,
			src:    src,
		}
		findings = append(findings, checkOutputBoundary(info)...)
		findings = append(findings, checkJSONBoundary(info)...)
		findings = append(findings, checkKeyringBoundary(info)...)
		findings = append(findings, checkAtomicWrites(info)...)
		findings = append(findings, checkArchitecture(info)...)
		findings = append(findings, checkNoPanic(info)...)
		findings = append(findings, checkNoOSExit(info)...)
		findings = append(findings, checkPermissions(info)...)
		findings = append(findings, checkTestIsolation(info)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Column < findings[j].Column
	})
	return findings, nil
}

func isExcluded(rel string) bool {
	for _, prefix := range []string{"_WIP/", "npm/vendor/", "bin/", "dist/", "coverage/", "node_modules/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func (f *fileInfo) add(rule string, pos token.Pos, message string) Finding {
	position := f.fset.Position(pos)
	return Finding{
		Rule:    rule,
		File:    f.rel,
		Line:    position.Line,
		Column:  position.Column,
		Message: message,
	}
}

func selectorName(expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name + "." + selector.Sel.Name
}

func callName(call *ast.CallExpr) string {
	return selectorName(call.Fun)
}

func checkOutputBoundary(info *fileInfo) []Finding {
	if info.isTest || !isOutputGuarded(info.dir) {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch callName(call) {
		case "fmt.Print", "fmt.Printf", "fmt.Println", "fmt.PrintErr":
			findings = append(findings, info.add("output-boundary", call.Pos(), "use App.Out/App.Err instead of direct fmt printing"))
		case "log.Print", "log.Printf", "log.Println", "log.Fatal", "log.Fatalf", "log.Panic", "log.Panicf":
			findings = append(findings, info.add("output-boundary", call.Pos(), "library packages must not log directly"))
		}
		if name := selectorName(call.Fun); name == "fmt.Fprintf" || name == "fmt.Fprintln" {
			if len(call.Args) > 0 && isStandardStream(call.Args[0]) && info.rel != "internal/cli/app.go" {
				findings = append(findings, info.add("output-boundary", call.Pos(), "write through App.Out/App.Err, not os.Stdout/os.Stderr"))
			}
		}
		return true
	})
	ast.Inspect(info.file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selectorName(selector)
		if (name == "os.Stdout" || name == "os.Stderr") && info.rel != "internal/cli/app.go" {
			findings = append(findings, info.add("output-boundary", selector.Pos(), "only internal/cli/app.go may wire the standard streams"))
		}
		return true
	})
	return findings
}

func isOutputGuarded(dir string) bool {
	return repo.Under(dir, "internal/agy") || repo.Under(dir, "internal/registry") ||
		repo.Under(dir, "internal/oauth") || repo.Under(dir, "internal/cli")
}

func isStandardStream(expr ast.Expr) bool {
	name := selectorName(expr)
	return name == "os.Stdout" || name == "os.Stderr"
}

func checkJSONBoundary(info *fileInfo) []Finding {
	if info.isTest || !repo.Under(info.dir, "internal/cli") || info.rel == "internal/cli/render.go" {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == "json.NewEncoder" {
			findings = append(findings, info.add("json-boundary", call.Pos(), "use emitJSON in internal/cli/render.go"))
		}
		return true
	})
	return findings
}

func checkKeyringBoundary(info *fileInfo) []Finding {
	if info.rel == "internal/agy/storage.go" {
		return nil
	}
	var findings []Finding
	for _, spec := range info.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil && path == "github.com/zalando/go-keyring" {
			findings = append(findings, info.add("keyring-boundary", spec.Pos(), "the OS keyring belongs in internal/agy/storage.go"))
		}
	}
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if strings.HasPrefix(callName(call), "keyring.") {
			findings = append(findings, info.add("keyring-boundary", call.Pos(), "the OS keyring belongs in internal/agy/storage.go"))
		}
		return true
	})
	return findings
}

func checkAtomicWrites(info *fileInfo) []Finding {
	if info.isTest || (!repo.Under(info.dir, "internal/agy") && !repo.Under(info.dir, "internal/registry") && !repo.Under(info.dir, "internal/desktop")) {
		return nil
	}
	allowed := functionRanges(info.file, "writeFileAtomic")
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if name != "os.WriteFile" && name != "os.Create" && name != "os.OpenFile" {
			return true
		}
		if insideRanges(call.Pos(), allowed) {
			return true
		}
		if name == "os.OpenFile" && !hasWriteFlag(call) {
			return true
		}
		findings = append(findings, info.add("atomic-writes", call.Pos(), "route this write through the atomic write helper"))
		return true
	})
	return findings
}

func functionRanges(file *ast.File, name string) []struct{ start, end token.Pos } {
	var ranges []struct{ start, end token.Pos }
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		ranges = append(ranges, struct{ start, end token.Pos }{fn.Body.Pos(), fn.Body.End()})
	}
	return ranges
}

func insideRanges(pos token.Pos, ranges []struct{ start, end token.Pos }) bool {
	for _, item := range ranges {
		if pos >= item.start && pos <= item.end {
			return true
		}
	}
	return false
}

func hasWriteFlag(call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	return exprContainsFlag(call.Args[1])
}

func exprContainsFlag(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "O_WRONLY", "O_RDWR", "O_CREATE", "O_TRUNC", "O_APPEND":
			found = true
		}
		return true
	})
	return found
}

func checkArchitecture(info *fileInfo) []Finding {
	var allowed []string
	switch {
	case info.dir == "cmd/agy-auth":
		allowed = []string{"internal/cli"}
	case info.dir == "cmd/harness":
		allowed = []string{"internal/harness"}
	case info.dir == "internal/backup":
		allowed = nil
	case info.dir == "internal/agy":
		allowed = []string{"internal/backup"}
	case info.dir == "internal/registry":
		allowed = []string{"internal/agy", "internal/backup"}
	case info.dir == "internal/oauth":
		allowed = []string{"internal/agy"}
	case info.dir == "internal/usage":
		allowed = []string{"internal/agy", "internal/oauth"}
	case info.dir == "internal/desktop":
		allowed = []string{"internal/backup"}
	case info.dir == "internal/cli":
		allowed = []string{"internal/agy", "internal/registry", "internal/oauth", "internal/backup", "internal/usage", "internal/desktop"}
	case repo.Under(info.dir, "internal/harness"):
		allowed = []string{"internal/harness", "internal/agy", "internal/registry", "internal/desktop"}
	default:
		allowed = []string{}
	}
	var findings []Finding
	const modulePrefix = "github.com/fikrilal/agy-auth/"
	for _, spec := range info.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.HasPrefix(path, modulePrefix) {
			continue
		}
		target := strings.TrimPrefix(path, modulePrefix)
		if isAllowed(target, allowed) {
			continue
		}
		if info.isTest && target == "internal/harness" {
			continue
		}
		findings = append(findings, info.add("architecture", spec.Pos(), fmt.Sprintf("%s must not import %s", info.dir, target)))
	}
	return findings
}

func isAllowed(target string, allowed []string) bool {
	for _, prefix := range allowed {
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return true
		}
	}
	return false
}

func checkNoPanic(info *fileInfo) []Finding {
	if info.isTest || !strings.HasPrefix(info.rel, "internal/") {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
			findings = append(findings, info.add("no-panic", call.Pos(), "return an error instead of panicking"))
		}
		return true
	})
	return findings
}

func checkNoOSExit(info *fileInfo) []Finding {
	if info.isTest || strings.HasPrefix(info.rel, "cmd/") {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(call) == "os.Exit" {
			findings = append(findings, info.add("no-os-exit", call.Pos(), "return an exit code to main instead of calling os.Exit"))
		}
		return true
	})
	return findings
}

func checkPermissions(info *fileInfo) []Finding {
	if !repo.Under(info.dir, "internal/agy") && !repo.Under(info.dir, "internal/registry") {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var index int
		switch callName(call) {
		case "os.MkdirAll", "os.Chmod":
			index = 1
		case "os.OpenFile", "os.WriteFile":
			index = 2
		default:
			return true
		}
		if len(call.Args) <= index {
			return true
		}
		literal, ok := call.Args[index].(*ast.BasicLit)
		if !ok || literal.Kind != token.INT {
			return true
		}
		value, err := strconv.ParseInt(literal.Value, 0, 64)
		if err != nil {
			return true
		}
		if value != 0o600 && value != 0o700 {
			findings = append(findings, info.add("file-permissions", literal.Pos(), "use 0600 for files or 0700 for directories"))
		}
		return true
	})
	return findings
}

func checkTestIsolation(info *fileInfo) []Finding {
	if !info.isTest {
		return nil
	}
	var findings []Finding
	ast.Inspect(info.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if name == "registry.DefaultStore" || name == "agy.DefaultStorage" {
			findings = append(findings, info.add("test-isolation", call.Pos(), "tests must use a temporary registry/storage with NoKeyring"))
		}
		if strings.HasPrefix(name, "keyring.") {
			findings = append(findings, info.add("test-isolation", call.Pos(), "tests must not touch the real OS keyring"))
		}
		return true
	})
	return findings
}
