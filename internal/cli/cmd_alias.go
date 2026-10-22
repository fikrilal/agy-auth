package cli

import (
	"errors"
	"fmt"

	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdAlias(args []string) int {
	if len(args) == 0 {
		return a.usageFor("alias", "alias requires set, clear, or list")
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		a.printCommandHelp("alias")
		return 0
	}
	switch args[0] {
	case "set":
		return a.cmdAliasSet(args[1:])
	case "clear":
		return a.cmdAliasClear(args[1:])
	case "list":
		return a.cmdAliasList(args[1:])
	default:
		return a.usageFor("alias", "unknown alias command %q; use set, clear, or list", args[0])
	}
}

func (a *App) cmdAliasSet(args []string) int {
	fs := a.newFlagSet("alias")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 2 {
		return a.usageFor("alias", "alias set requires a selector and alias")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	account, code := a.resolveOne(reg, fs.Arg(0), "Select an account")
	if code != 0 {
		return code
	}
	previous := account.Alias
	display := account.DisplayName()
	if err := reg.SetAlias(account.ID, fs.Arg(1)); err != nil {
		return a.usageFor("alias", "%s", err)
	}
	if err := a.Store.Save(reg); err != nil {
		return a.registryFailure(err, "could not save the account registry")
	}
	updated := reg.FindByID(account.ID)
	if a.JSON {
		if err := a.emitJSON(updated); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}
	switch {
	case previous == "":
		fmt.Fprintf(a.Out, "Set alias %q for %s.\n", updated.Alias, display)
	case previous == updated.Alias:
		fmt.Fprintf(a.Out, "Alias for %s is already %q.\n", updated.DisplayName(), updated.Alias)
	default:
		fmt.Fprintf(a.Out, "Updated alias for %s from %q to %q.\n", display, previous, updated.Alias)
	}
	return 0
}

func (a *App) cmdAliasClear(args []string) int {
	fs := a.newFlagSet("alias")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return a.usageFor("alias", "alias clear requires a selector")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	account, code := a.resolveOne(reg, fs.Arg(0), "Select an account")
	if code != 0 {
		return code
	}
	display := account.DisplayName()
	previous := account.Alias
	if err := reg.ClearAlias(account.ID); err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` to check storage health.", "could not clear the alias")
	}
	if err := a.Store.Save(reg); err != nil {
		return a.registryFailure(err, "could not save the account registry")
	}
	updated := reg.FindByID(account.ID)
	if a.JSON {
		if err := a.emitJSON(updated); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}
	if previous == "" {
		fmt.Fprintf(a.Out, "%s has no alias.\n", display)
	} else {
		fmt.Fprintf(a.Out, "Cleared alias %q for %s.\n", previous, display)
	}
	return 0
}

func (a *App) cmdAliasList(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		a.printCommandHelp("alias")
		return 0
	}
	if len(args) > 0 {
		return a.usageFor("alias", "alias list takes no arguments")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	if a.JSON {
		if err := a.emitJSON(reg.Accounts); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}
	if len(reg.Accounts) == 0 {
		fmt.Fprintln(a.Out, "No accounts stored.")
		return 0
	}
	a.renderAccountRows(a.pickerRows(reg.Accounts, reg.Active, accountNumbers(reg)), a.now())
	return 0
}

func (a *App) resolveFailure(query string, matches []registry.Account) int {
	return a.resolveFailureWithNumbers(query, matches, nil)
}

func (a *App) resolveFailureWithNumbers(query string, matches []registry.Account, numbers map[string]int) int {
	if len(matches) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth list` to see stored accounts.", "no account matches %q", query)
	}
	candidates := make([]Candidate, 0, len(matches))
	for _, account := range matches {
		candidate := Candidate{
			ID:    account.ID,
			Email: account.Email,
			Alias: account.Alias,
		}
		if numbers != nil {
			candidate.Number = numbers[account.ID]
		}
		candidates = append(candidates, candidate)
	}
	return a.errorDoc(1, ErrorDoc{
		Code:       "ambiguous_query",
		Message:    fmt.Sprintf("query %q matches multiple accounts: %s", query, selectionSummary(matches)),
		Hint:       "Use a row number, exact email, alias, or account ID prefix.",
		Candidates: candidates,
	})
}

func (a *App) resolveOne(reg *registry.Registry, query, title string) (*registry.Account, int) {
	matches := reg.Resolve(query)
	switch len(matches) {
	case 0:
		return nil, a.resolveFailureWithNumbers(query, matches, accountNumbers(reg))
	case 1:
		return &matches[0], 0
	default:
		if a.TTY && !a.JSON {
			selected, err := a.chooseAccount(matches, reg.Active, title, accountNumbers(reg))
			if errors.Is(err, errCancelled) {
				fmt.Fprintln(a.Err, "cancelled")
				return nil, 1
			}
			if err != nil {
				return nil, a.failCode("selection_failed", "Run `agy-auth list` and pass a query instead.", "could not select an account")
			}
			return selected, 0
		}
		return nil, a.resolveFailureWithNumbers(query, matches, accountNumbers(reg))
	}
}
