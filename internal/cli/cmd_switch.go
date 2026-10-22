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
	if a.JSON && fs.NArg() == 0 {
		return a.usageFor("switch", "switch --json requires a query")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts")
	}

	var account *registry.Account
	if fs.NArg() == 1 && fs.Arg(0) == "-" {
		if reg.Previous == "" {
			return a.failCode("previous_not_found", "Switch to another account first, then use `agy-auth switch -`.", "no previous account")
		}
		account = reg.FindByID(reg.Previous)
		if account == nil {
			return a.failCode("previous_not_found", "Switch to another account first, then use `agy-auth switch -`.", "the previous account is no longer stored")
		}
	} else if fs.NArg() == 1 {
		selected, code := a.resolveOne(reg, fs.Arg(0), "Select an account to activate")
		if code != 0 {
			return code
		}
		account = selected
	} else {
		selected, err := a.chooseAccount(reg.Accounts, reg.Active, "Select an account to activate")
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(a.Err, "cancelled")
			return 1
		}
		if err != nil {
			return a.failCode("selection_failed", "Run `agy-auth list` and pass a query instead.", "could not select an account")
		}
		account = selected
	}

	raw, err := a.Store.LoadToken(account.ID)
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` to check storage health.", "could not read the stored credential for %s", account.DisplayName())
	}
	tf, err := agy.ParseTokenFile(raw)
	if err != nil {
		return a.failCode("credential_error", "Remove and re-add the account, or sign in again with `agy-auth login`.", "stored credential for %s is unusable", account.DisplayName())
	}
	now := a.now()
	var warnings []string
	if tf.Expired(now) {
		if !tf.HasRefreshToken() {
			return a.failCode("credential_error", "Sign in again with `agy-auth login`.", "%s has an expired access token and no refresh token", account.DisplayName())
		}
		warnings = append(warnings, "the access token is expired; agy will refresh it on next start")
	} else if !tf.HasRefreshToken() {
		warnings = append(warnings, "this snapshot has no refresh token; it will stop working when the access token expires")
	}

	report := a.Storage.Write(raw)
	if !report.OK() {
		if errors.Is(report.Error(), agy.ErrBackup) {
			return a.failCode("backup_error", "Check the backup directory permissions.", "could not back up the current agy credential")
		}
		return a.failCode("credential_write_failed", "Run `agy-auth status` to check keyring and file-mirror access.", "could not write the agy credential")
	}
	if err := report.Error(); err != nil {
		warnings = append(warnings, err.Error())
	}

	for i := range reg.Accounts {
		if reg.Accounts[i].ID == account.ID {
			reg.Accounts[i].LastUsedAt = now.UTC()
		}
	}
	if reg.Active != "" && reg.Active != account.ID {
		reg.Previous = reg.Active
	}
	if reg.Previous == account.ID {
		reg.Previous = ""
	}
	reg.Active = account.ID
	if err := a.Store.Save(reg); err != nil {
		message := "the credential was written but the registry could not be updated"
		if errors.Is(err, registry.ErrBackup) {
			message = "the credential was written but the registry could not be backed up"
		}
		return a.errorDoc(1, ErrorDoc{
			Code:    "state_uncertain",
			Message: message,
			Hint:    "Run `agy-auth list` to verify the active account before retrying.",
		})
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
		for _, warning := range warnings {
			a.warn("%s", warning)
		}
		if err := a.emitJSON(map[string]any{
			"account": account,
			"written": report.Written,
		}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
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
