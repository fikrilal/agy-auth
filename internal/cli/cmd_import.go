package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdImport(args []string) int {
	fs := a.newFlagSet("import")
	alias := fs.String("alias", "", "alias for a single imported file")
	activate := fs.Bool("activate", false, "activate a single imported file")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return a.usageFor("import", "import takes exactly one file or directory")
	}
	path := fs.Arg(0)
	info, err := os.Stat(path)
	if err != nil {
		return a.fail("%v", err)
	}

	var files []string
	fromDirectory := info.IsDir()
	if fromDirectory {
		if *alias != "" || *activate {
			return a.usageFor("import", "--alias and --activate only work when importing a single file")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return a.fail("%v", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			if strings.EqualFold(entry.Name(), "registry.json") {
				continue
			}
			files = append(files, filepath.Join(path, entry.Name()))
		}
		sort.Strings(files)
		if len(files) == 0 {
			return a.fail("no JSON credential files found in %s", path)
		}
	} else {
		files = []string{path}
	}

	raws := make([][]byte, 0, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return a.fail("read %s: %v", file, err)
		}
		if _, err := agy.ParseTokenFile(raw); err != nil {
			return a.fail("import %s: %v", file, err)
		}
		raws = append(raws, raw)
	}

	imported := make([]registry.Account, 0, len(raws))
	for i, raw := range raws {
		result, err := a.Store.Upsert(raw, *alias, *activate)
		if err != nil {
			return a.fail("import %s: %v", files[i], err)
		}
		imported = append(imported, result.Account)
	}

	activated := false
	if *activate && len(files) == 1 {
		report := a.Storage.Write(raws[0])
		if !report.OK() {
			return a.fail("imported but could not write agy credentials: %v", report.Error())
		}
		activated = true
	}

	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"imported":  imported,
			"activated": activated,
		}); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	for _, account := range imported {
		fmt.Fprintf(a.Out, "Imported account %s [%s].\n", account.DisplayName(), account.ID)
	}
	if activated {
		fmt.Fprintln(a.Out, "Activated the imported account. Restart agy for it to take effect.")
	}
	return 0
}

func (a *App) cmdExport(args []string) int {
	fs := a.newFlagSet("export")
	dir := fs.String("dir", "", "destination directory (default: current directory)")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	if len(reg.Accounts) == 0 {
		return a.fail("no stored accounts to export")
	}

	var accounts []registry.Account
	if fs.NArg() == 0 {
		accounts = append(accounts, reg.Accounts...)
	} else {
		for _, query := range fs.Args() {
			matches := reg.Resolve(query)
			if len(matches) != 1 {
				return a.resolveFailure(query, matches)
			}
			accounts = append(accounts, matches[0])
		}
		accounts = dedupeAccounts(accounts)
	}

	target := *dir
	if target == "" {
		target = "."
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return a.fail("%v", err)
	}

	type exported struct {
		Account registry.Account `json:"account"`
		Path    string           `json:"path"`
	}
	results := make([]exported, 0, len(accounts))
	for _, account := range accounts {
		raw, err := a.Store.LoadToken(account.ID)
		if err != nil {
			return a.fail("%v", err)
		}
		name := sanitizeFilename(valueOr(account.Email, valueOr(account.Alias, account.ID)))
		filename := fmt.Sprintf("%s-%s.json", name, shortID(account.ID))
		path := filepath.Join(target, filename)
		if err := writeExportFile(path, raw); err != nil {
			return a.fail("%v", err)
		}
		results = append(results, exported{Account: account, Path: path})
	}

	if a.JSON {
		if err := a.emitJSON(results); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	for _, result := range results {
		fmt.Fprintln(a.Out, result.Path)
	}
	return 0
}

func writeExportFile(path string, raw []byte) error {
	if _, err := agy.ParseTokenFile(raw); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	return nil
}
