package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdRemove(args []string) int {
	fs := a.newFlagSet("remove")
	all := fs.Bool("all", false, "remove every stored account")
	logout := fs.Bool("logout", false, "also delete the live agy credential when removing the active account")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	live := fs.Bool("live", false, "interactive live view")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}

	if *live {
		if fs.NArg() > 0 || *all {
			return a.usageFor("remove", "remove --live does not take selectors or --all")
		}
		return a.runLive(liveModeRemove, *logout)
	}
	if *all && fs.NArg() > 0 {
		return a.usageFor("remove", "remove --all cannot be combined with a query")
	}
	if a.JSON && !*all && fs.NArg() == 0 {
		return a.usageFor("remove", "remove --json requires one or more queries or --all")
	}
	if !*all && fs.NArg() == 0 && !a.TTY {
		return a.usageFor("remove", "specify one or more account queries, or pass --all")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts to remove")
	}

	var selected []registry.Account
	switch {
	case *all:
		selected = append(selected, reg.Accounts...)
	case fs.NArg() > 0:
		for _, query := range fs.Args() {
			account, code := a.resolveOne(reg, query, "Select an account to remove")
			if code != 0 {
				return code
			}
			selected = append(selected, *account)
		}
	default:
		fmt.Fprintln(a.Out, "Stored accounts:")
		a.renderAccountRows(a.pickerRows(reg.Accounts, reg.Active, accountNumbers(reg)), a.now())
		answer, err := a.readLine("Remove which accounts? (numbers, aliases, emails, or 'all'): ")
		if err != nil {
			return a.failCode("input_error", "Try again, or pass selectors on the command line.", "could not read the selection")
		}
		if strings.EqualFold(strings.TrimSpace(answer), "all") {
			selected = append(selected, reg.Accounts...)
		} else {
			for _, token := range strings.Split(answer, ",") {
				token = strings.TrimSpace(token)
				if token == "" {
					continue
				}
				account, code := a.resolveOne(reg, token, "Select an account to remove")
				if code != 0 {
					return code
				}
				selected = append(selected, *account)
			}
		}
	}

	selected = dedupeAccounts(selected)
	if len(selected) == 0 {
		if a.JSON {
			return a.failCode("account_not_found", "Pass one or more account queries or --all.", "no accounts selected")
		}
		fmt.Fprintln(a.Out, "Nothing selected.")
		return 0
	}

	if !*yes {
		if !a.TTY {
			return a.failCode("confirmation_required", "Re-run with --yes to confirm.", "refusing to remove %d account(s) without --yes", len(selected))
		}
		answer, err := a.readLine(fmt.Sprintf("Remove %d account(s)? [y/N]: ", len(selected)))
		if err != nil || !isYes(answer) {
			if a.JSON {
				return a.failCode("confirmation_declined", "Re-run with --yes to confirm.", "removal cancelled")
			}
			fmt.Fprintln(a.Out, "Cancelled.")
			return 1
		}
	}

	var current *agy.TokenFile
	var currentRaw []byte
	var matched *registry.Account
	if read, err := a.Storage.Read(); err == nil {
		currentRaw = read.Raw
		current, _ = agy.ParseTokenFile(read.Raw)
		matched, _ = reg.Match(currentRaw)
	} else if !errors.Is(err, agy.ErrNoCredentials) {
		a.warn("%v", err)
	}

	removeIDs := make(map[string]bool, len(selected))
	for _, account := range selected {
		removeIDs[account.ID] = true
	}
	if matched != nil && !*logout && removeIDs[matched.ID] {
		a.warn("agy is still signed in to %s; the live credential was kept (use --logout to remove it)", matched.DisplayName())
	}
	if *logout && current != nil && (matched == nil || removeIDs[matched.ID]) {
		if err := a.Storage.SnapshotCurrent(); err != nil {
			return a.failCode("backup_error", "Check the backup directory permissions.", "could not back up the current agy credential")
		}
	}

	for _, account := range selected {
		if err := a.Store.DeleteToken(account.ID); err != nil {
			return a.errorDoc(1, ErrorDoc{
				Code:    "state_uncertain",
				Message: fmt.Sprintf("could not remove the stored credential for %s", account.DisplayName()),
				Hint:    "Some snapshots may have been deleted; run `agy-auth list` and `agy-auth import` to reconcile.",
			})
		}
	}

	loggedOut := false
	if *logout && current != nil && (matched == nil || removeIDs[matched.ID]) {
		if err := a.Storage.Delete(); err != nil {
			if errors.Is(err, agy.ErrBackup) {
				a.warn("could not back up the live agy credential before logout: %v", err)
			} else {
				a.warn("could not remove the live agy credential: %v", err)
			}
		} else {
			loggedOut = true
			_ = a.Storage.SyncSettings(agy.AuthMethodConsumer, "", "")
			if !a.JSON {
				fmt.Fprintln(a.Out, "Removed the live agy credential.")
			}
		}
	}

	removed := make([]registry.Account, 0, len(selected))
	for _, account := range selected {
		if reg.Remove(account.ID) {
			removed = append(removed, account)
		}
	}
	if err := a.Store.Save(reg); err != nil {
		message := "the snapshots were removed but the registry could not be updated"
		if errors.Is(err, registry.ErrBackup) {
			message = "the snapshots were removed but the registry could not be backed up"
		}
		return a.errorDoc(1, ErrorDoc{
			Code:    "state_uncertain",
			Message: message,
			Hint:    "Run `agy-auth list` to verify the stored accounts.",
		})
	}

	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"removed":    removed,
			"logged_out": loggedOut,
		}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}
	for _, account := range removed {
		fmt.Fprintf(a.Out, "Removed %s (%s).\n", account.DisplayName(), account.ID)
	}
	return 0
}

func dedupeAccounts(accounts []registry.Account) []registry.Account {
	seen := make(map[string]bool, len(accounts))
	result := make([]registry.Account, 0, len(accounts))
	for _, account := range accounts {
		if seen[account.ID] {
			continue
		}
		seen[account.ID] = true
		result = append(result, account)
	}
	return result
}
