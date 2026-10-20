package cli

import (
	"fmt"

	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdAlias(args []string) int {
	if len(args) == 0 {
		a.printCommandHelp("alias")
		return 2
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
		return a.fail("unknown alias command %q; use set, clear, or list", args[0])
	}
}

func (a *App) cmdAliasSet(args []string) int {
	fs := a.newFlagSet("alias")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 2 {
		a.printCommandHelp("alias")
		return 2
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	matches := reg.Resolve(fs.Arg(0))
	if len(matches) != 1 {
		return a.resolveFailure(fs.Arg(0), matches)
	}
	if err := reg.SetAlias(matches[0].ID, fs.Arg(1)); err != nil {
		return a.fail("%v", err)
	}
	if err := a.Store.Save(reg); err != nil {
		return a.fail("%v", err)
	}
	account := reg.FindByID(matches[0].ID)
	if a.JSON {
		if err := a.emitJSON(account); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	fmt.Fprintf(a.Out, "Alias for %s is now %q.\n", account.DisplayName(), account.Alias)
	return 0
}

func (a *App) cmdAliasClear(args []string) int {
	fs := a.newFlagSet("alias")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		a.printCommandHelp("alias")
		return 2
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	matches := reg.Resolve(fs.Arg(0))
	if len(matches) != 1 {
		return a.resolveFailure(fs.Arg(0), matches)
	}
	account := reg.FindByID(matches[0].ID)
	previous := account.Alias
	if err := reg.ClearAlias(account.ID); err != nil {
		return a.fail("%v", err)
	}
	if err := a.Store.Save(reg); err != nil {
		return a.fail("%v", err)
	}
	if a.JSON {
		if err := a.emitJSON(reg.FindByID(account.ID)); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	if previous == "" {
		fmt.Fprintf(a.Out, "%s has no alias.\n", account.DisplayName())
	} else {
		fmt.Fprintf(a.Out, "Cleared alias %q from %s.\n", previous, account.Email)
	}
	return 0
}

func (a *App) cmdAliasList(args []string) int {
	if len(args) > 0 {
		return a.fail("alias list takes no arguments")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	if a.JSON {
		if err := a.emitJSON(reg.Accounts); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	if len(reg.Accounts) == 0 {
		fmt.Fprintln(a.Out, "No accounts stored.")
		return 0
	}
	for i, account := range reg.Accounts {
		fmt.Fprintf(a.Out, "%02d  %-40s  %s\n", i+1, account.DisplayName(), valueOr(account.Alias, "-"))
	}
	return 0
}

func (a *App) resolveFailure(query string, matches []registry.Account) int {
	if len(matches) == 0 {
		return a.fail("no account matches %q", query)
	}
	return a.fail("query %q matches multiple accounts: %s", query, selectionSummary(matches))
}
