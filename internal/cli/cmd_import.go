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
	gcp := fs.Bool("gcp", false, "mark a single imported file as a Google Cloud project account")
	project := fs.String("project", "", "Google Cloud project ID for a single imported file")
	region := fs.String("region", "", "Google Cloud region (global, us, or eu) for a single imported file")
	purge := fs.Bool("purge", false, "rebuild the registry from stored snapshots")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	isGCP := *gcp || strings.TrimSpace(*project) != ""
	if !isGCP && strings.TrimSpace(*region) != "" {
		return a.usageFor("import", "--region requires --gcp or --project")
	}
	if *purge {
		if *alias != "" || *activate || isGCP || strings.TrimSpace(*region) != "" {
			return a.usageFor("import", "--purge cannot be combined with --alias, --activate, --gcp, --project, or --region")
		}
		if fs.NArg() != 0 {
			return a.usageFor("import", "import --purge does not take a path")
		}
		return a.purgeRegistry()
	}
	if fs.NArg() != 1 {
		return a.usageFor("import", "import takes exactly one file or directory")
	}
	if *alias != "" {
		if err := registry.ValidateAlias(*alias); err != nil {
			return a.usageFor("import", "%s", err)
		}
	}
	path := fs.Arg(0)
	info, err := os.Stat(path)
	if err != nil {
		return a.failCode("import_failed", "Check the path and permissions.", "could not access %s", path)
	}

	var files []string
	fromDirectory := info.IsDir()
	if fromDirectory {
		if *alias != "" || *activate || isGCP || strings.TrimSpace(*region) != "" {
			return a.usageFor("import", "--alias, --activate, --gcp, --project, and --region only work when importing a single file")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return a.failCode("import_failed", "Check the directory permissions.", "could not read %s", path)
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
			return a.failCode("import_failed", "Put agy credential JSON files in the directory.", "no JSON credential files found in %s", path)
		}
	} else {
		files = []string{path}
	}

	report := importReport{Files: []transferEvent{}, Imported: []registry.Account{}}
	var singleRaw []byte
	for _, file := range files {
		event := transferEvent{Path: file}
		raw, err := os.ReadFile(file)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "unreadable"
			report.Files = append(report.Files, event)
			continue
		}
		plain, err := a.Store.DecodeToken(raw)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "unreadable"
			report.Files = append(report.Files, event)
			continue
		}
		tf, err := agy.ParseTokenFile(plain)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "invalid credential"
			report.Files = append(report.Files, event)
			continue
		}
		if !fromDirectory && isGCP {
			effectiveProject := strings.TrimSpace(*project)
			if effectiveProject == "" {
				effectiveProject = strings.TrimSpace(tf.ProjectID)
			}
			if effectiveProject == "" {
				return a.usageFor("import", "--gcp requires --project <id> when the credential file has no project_id")
			}
			effectiveRegion := strings.TrimSpace(*region)
			if effectiveRegion == "" {
				effectiveRegion = strings.TrimSpace(tf.Region)
			}
			updatedPlain, err := agy.WithGCPOptions(plain, agy.AuthMethodGCP, effectiveProject, effectiveRegion)
			if err != nil {
				return a.usageFor("import", "%s", err)
			}
			plain = updatedPlain
		}

		aliasValue := ""
		activateValue := false
		if !fromDirectory {
			aliasValue = *alias
			activateValue = *activate
		}
		result, err := a.Store.Upsert(plain, aliasValue, activateValue)
		if err != nil {
			if errors.Is(err, registry.ErrBackup) {
				return a.failCode("backup_error", "Check the backup directory permissions.", "could not back up the account registry")
			}
			event.Outcome = outcomeSkipped
			event.Reason = "registry error"
			report.Files = append(report.Files, event)
			continue
		}

		account := result.Account
		event.Outcome = outcomeImported
		if !result.Created {
			event.Outcome = outcomeUpdated
		}
		event.Account = &account
		report.Files = append(report.Files, event)
		report.Imported = append(report.Imported, account)
		if !fromDirectory {
			singleRaw = plain
		}
	}
	summarizeImport(&report)

	if *activate && !fromDirectory && singleRaw != nil && len(report.Imported) == 1 {
		writeReport := a.Storage.Write(singleRaw)
		if !writeReport.OK() {
			if errors.Is(writeReport.Error(), agy.ErrBackup) {
				return a.failCode("backup_error", fmt.Sprintf("The account is imported; fix backup permissions and run `agy-auth switch %s`.", report.Imported[0].ID), "could not back up the current agy credential")
			}
			return a.errorDoc(1, ErrorDoc{
				Code:    "state_uncertain",
				Message: "imported the account but could not write the agy credential",
				Hint:    fmt.Sprintf("Run `agy-auth switch %s` after fixing credential storage.", report.Imported[0].ID),
			})
		}
		if tf, err := agy.ParseTokenFile(singleRaw); err == nil {
			_ = a.Storage.SyncSettings(tf.AuthMethod, tf.ProjectID, tf.Region)
		}
		report.Activated = true
	}

	if a.JSON {
		if err := a.emitJSON(report); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
	} else {
		a.renderImportReport(&report)
	}
	if report.Summary.Imported+report.Summary.Updated == 0 && report.Summary.Skipped > 0 {
		return 1
	}
	return 0
}

func (a *App) purgeRegistry() int {
	files, err := a.accountSnapshotFiles()
	if err != nil {
		return a.failCode("import_failed", "Check storage permissions.", "could not read account snapshots")
	}
	if len(files) == 0 {
		return a.failCode("import_failed", "Store an account first.", "no account snapshots to rebuild from")
	}

	old, err := a.Store.Load()
	if err != nil {
		a.warn("could not read the previous registry; rebuilding without aliases: %v", err)
		old = nil
	}
	byID := map[string]registry.Account{}
	bySubject := map[string]registry.Account{}
	if old != nil {
		for _, account := range old.Accounts {
			byID[account.ID] = account
			if account.Subject != "" {
				bySubject[account.Subject] = account
			}
		}
	}

	report := importReport{Files: []transferEvent{}, Imported: []registry.Account{}, Rebuilt: true}
	reg := &registry.Registry{Version: registry.Version}
	if old != nil && old.Config != nil {
		reg.Config = old.Config
	}
	seen := map[string]bool{}
	for _, file := range files {
		event := transferEvent{Path: file}
		raw, err := os.ReadFile(file)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "unreadable"
			report.Files = append(report.Files, event)
			continue
		}
		plain, err := a.Store.DecodeToken(raw)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "unreadable"
			report.Files = append(report.Files, event)
			continue
		}
		account, err := registry.AccountFromRaw(plain)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "invalid credential"
			report.Files = append(report.Files, event)
			continue
		}
		if err := a.Store.SaveToken(account.ID, plain); err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "could not encrypt credential"
			report.Files = append(report.Files, event)
			continue
		}
		if seen[account.ID] {
			event.Outcome = outcomeSkipped
			event.Reason = "duplicate"
			report.Files = append(report.Files, event)
			continue
		}
		seen[account.ID] = true
		if oldAccount, ok := byID[account.ID]; ok {
			carryAccount(&account, oldAccount)
		} else if account.Subject != "" {
			if oldAccount, ok := bySubject[account.Subject]; ok && oldAccount.AuthMethod == account.AuthMethod && oldAccount.ProjectID == account.ProjectID {
				carryAccount(&account, oldAccount)
			}
		}
		reg.Accounts = append(reg.Accounts, account)
		accountCopy := account
		event.Outcome = outcomeImported
		event.Account = &accountCopy
		report.Files = append(report.Files, event)
		report.Imported = append(report.Imported, account)
	}
	summarizeImport(&report)

	if len(reg.Accounts) == 0 {
		if a.JSON {
			if err := a.emitJSON(report); err != nil {
				return a.failCode("output_error", "", "could not write JSON output")
			}
		} else {
			a.renderImportReport(&report)
		}
		return 1
	}

	active := ""
	if read, err := a.Storage.Read(); err == nil {
		temporary := &registry.Registry{Accounts: reg.Accounts}
		if match, err := temporary.Match(read.Raw); err == nil && match != nil {
			active = match.ID
		}
	}
	if active == "" && old != nil && old.Active != "" && reg.FindByID(old.Active) != nil {
		active = old.Active
	}
	if active == "" {
		active = reg.Accounts[0].ID
	}
	reg.Active = active
	if old != nil {
		switch {
		case old.Previous != "" && old.Previous != active && reg.FindByID(old.Previous) != nil:
			reg.Previous = old.Previous
		case old.Active != "" && old.Active != active && reg.FindByID(old.Active) != nil:
			reg.Previous = old.Active
		}
	}
	if reg.Previous == active {
		reg.Previous = ""
	}
	if err := a.Store.Save(reg); err != nil {
		return a.registryFailure(err, "could not save the rebuilt registry")
	}
	report.Active = reg.Active
	report.Previous = reg.Previous
	if a.JSON {
		if err := a.emitJSON(report); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
	} else {
		a.renderImportReport(&report)
	}
	return 0
}

func (a *App) accountSnapshotFiles() ([]string, error) {
	entries, err := os.ReadDir(a.Store.AccountsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !strings.HasPrefix(id, "acct_") {
			continue
		}
		files = append(files, filepath.Join(a.Store.AccountsDir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func carryAccount(account *registry.Account, old registry.Account) {
	account.Alias = old.Alias
	if !old.AddedAt.IsZero() {
		account.AddedAt = old.AddedAt
	}
	if !old.LastUsedAt.IsZero() {
		account.LastUsedAt = old.LastUsedAt
	}
}

func (a *App) cmdExport(args []string) int {
	fs := a.newFlagSet("export")
	dir := fs.String("dir", "", "destination directory (default: current directory)")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts to export")
	}

	var accounts []registry.Account
	if fs.NArg() == 0 {
		accounts = append(accounts, reg.Accounts...)
	} else {
		for _, query := range fs.Args() {
			account, code := a.resolveOne(reg, query, "Select an account to export")
			if code != 0 {
				return code
			}
			accounts = append(accounts, *account)
		}
		accounts = dedupeAccounts(accounts)
	}
	if len(accounts) == 0 {
		return a.failCode("account_not_found", "Pass one or more account queries.", "no accounts selected to export")
	}

	target := *dir
	if target == "" {
		target = "."
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return a.failCode("export_failed", "Check the destination path and permissions.", "could not create the export directory")
	}

	report := exportReport{Directory: target, Files: []transferEvent{}, Exported: []exportedAccount{}}
	for _, account := range accounts {
		accountCopy := account
		event := transferEvent{Account: &accountCopy}
		raw, err := a.Store.LoadToken(account.ID)
		if err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "credential unreadable"
			report.Files = append(report.Files, event)
			continue
		}
		name := sanitizeFilename(valueOr(account.Email, valueOr(account.Alias, account.ID)))
		filename := fmt.Sprintf("%s-%s.json", name, shortID(account.ID))
		path := filepath.Join(target, filename)
		if err := writeExportFile(path, raw); err != nil {
			event.Outcome = outcomeSkipped
			event.Reason = "write failed"
			report.Files = append(report.Files, event)
			continue
		}
		event.Outcome = outcomeExported
		event.Path = path
		report.Files = append(report.Files, event)
		report.Exported = append(report.Exported, exportedAccount{Account: account, Path: path})
	}
	report.Summary.Total = len(accounts)
	for _, event := range report.Files {
		switch event.Outcome {
		case outcomeExported:
			report.Summary.Exported++
		case outcomeSkipped:
			report.Summary.Skipped++
		}
	}

	if a.JSON {
		if err := a.emitJSON(report); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
	} else {
		a.renderExportReport(&report)
	}
	if report.Summary.Exported == 0 && report.Summary.Skipped > 0 {
		return 1
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
