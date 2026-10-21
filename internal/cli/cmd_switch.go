package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdSwitch(args []string) int {
	fs := a.newFlagSet("switch")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 1 {
		return a.usageFor("switch", "switch accepts at most one query")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}
	if len(reg.Accounts) == 0 {
		return a.fail("no stored accounts; run \"agy-auth add\" or \"agy-auth login\" first")
	}

	var account *registry.Account
	if fs.NArg() == 1 {
		matches := reg.Resolve(fs.Arg(0))
		switch len(matches) {
		case 0:
			return a.fail("no account matches %q", fs.Arg(0))
		case 1:
			account = &matches[0]
		default:
			return a.fail("query %q matches multiple accounts: %s", fs.Arg(0), selectionSummary(matches))
		}
	} else {
		selected, err := a.chooseAccount(reg.Accounts, reg.Active, "Select an account to activate")
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(a.Err, "cancelled")
			return 1
		}
		if err != nil {
			return a.fail("%v", err)
		}
		account = selected
	}

	raw, err := a.Store.LoadToken(account.ID)
	if err != nil {
		return a.fail("%v", err)
	}
	tf, err := agy.ParseTokenFile(raw)
	if err != nil {
		return a.fail("stored credential for %s is unusable: %v", account.DisplayName(), err)
	}
	now := a.now()
	var warnings []string
	if tf.Expired(now) {
		if !tf.HasRefreshToken() {
			return a.fail("%s has an expired access token and no refresh token; sign in again with \"agy-auth login\"", account.DisplayName())
		}
		warnings = append(warnings, "the access token is expired; agy will refresh it on next start")
	} else if !tf.HasRefreshToken() {
		warnings = append(warnings, "this snapshot has no refresh token; it will stop working when the access token expires")
	}

	report := a.Storage.Write(raw)
	if !report.OK() {
		return a.fail("could not write the credential: %v", report.Error())
	}
	if err := report.Error(); err != nil {
		warnings = append(warnings, err.Error())
	}

	for i := range reg.Accounts {
		if reg.Accounts[i].ID == account.ID {
			reg.Accounts[i].LastUsedAt = now.UTC()
		}
	}
	reg.Active = account.ID
	if err := a.Store.Save(reg); err != nil {
		return a.fail("credential was written but the registry could not be updated: %v", err)
	}

	written := make([]string, 0, len(report.Written))
	for _, source := range report.Written {
		if source == agy.SourceKeyring {
			written = append(written, "OS keyring")
		} else {
			written = append(written, "file mirror")
		}
	}

	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"account":  account,
			"written":  report.Written,
			"warnings": warnings,
		}); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Switched to %s.\n", account.DisplayName())
	fmt.Fprintf(a.Out, "Wrote %s.\n", strings.Join(written, " and "))
	for _, warning := range warnings {
		fmt.Fprintf(a.Err, "warning: %s\n", warning)
	}
	fmt.Fprintln(a.Out, "Restart agy for the new account to take effect (running sessions keep the old credential).")
	return 0
}
