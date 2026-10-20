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
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	if len(reg.Accounts) == 0 {
		return a.fail("no stored accounts to remove")
	}

	var selected []registry.Account
	switch {
	case *all:
		selected = append(selected, reg.Accounts...)
	case fs.NArg() > 0:
		for _, query := range fs.Args() {
			matches := reg.Resolve(query)
			if len(matches) != 1 {
				return a.resolveFailure(query, matches)
			}
			selected = append(selected, matches[0])
		}
	default:
		if !a.TTY {
			return a.fail("specify one or more account queries, or pass --all")
		}
		fmt.Fprintln(a.Out, "Stored accounts:")
		for i, account := range reg.Accounts {
			marker := " "
			if account.ID == reg.Active {
				marker = "*"
			}
			fmt.Fprintf(a.Out, "  %s %02d) %s\n", marker, i+1, account.DisplayName())
		}
		answer, err := a.readLine("Remove which accounts? (numbers, aliases, emails, or 'all'): ")
		if err != nil {
			return a.fail("%v", err)
		}
		if strings.EqualFold(strings.TrimSpace(answer), "all") {
			selected = append(selected, reg.Accounts...)
		} else {
			for _, token := range strings.Split(answer, ",") {
				token = strings.TrimSpace(token)
				if token == "" {
					continue
				}
				matches := reg.Resolve(token)
				if len(matches) != 1 {
					return a.resolveFailure(token, matches)
				}
				selected = append(selected, matches[0])
			}
		}
	}

	selected = dedupeAccounts(selected)
	if len(selected) == 0 {
		fmt.Fprintln(a.Out, "Nothing selected.")
		return 0
	}

	if !*yes {
		if !a.TTY {
			return a.fail("refusing to remove %d account(s) without --yes", len(selected))
		}
		answer, err := a.readLine(fmt.Sprintf("Remove %d account(s)? [y/N]: ", len(selected)))
		if err != nil || !isYes(answer) {
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
	loggedOut := false
	if *logout && current != nil && (matched == nil || removeIDs[matched.ID]) {
		if err := a.Storage.Delete(); err != nil {
			a.warn("could not remove the live agy credential: %v", err)
		} else {
			loggedOut = true
			fmt.Fprintln(a.Out, "Removed the live agy credential.")
		}
	}

	removed := make([]registry.Account, 0, len(selected))
	for _, account := range selected {
		if matched != nil && matched.ID == account.ID && !*logout {
			a.warn("agy is still signed in to %s; the live credential was kept (use --logout to remove it)", account.DisplayName())
		}
		if err := a.Store.DeleteToken(account.ID); err != nil {
			a.warn("%v; keeping the registry entry", err)
			continue
		}
		if reg.Remove(account.ID) {
			removed = append(removed, account)
		}
	}
	if err := a.Store.Save(reg); err != nil {
		return a.fail("%v", err)
	}

	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"removed":    removed,
			"logged_out": loggedOut,
		}); err != nil {
			return a.fail("%v", err)
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
