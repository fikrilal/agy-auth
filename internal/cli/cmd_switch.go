package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/desktop"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdSwitch(args []string) int {
	fs := a.newFlagSet("switch")
	live := fs.Bool("live", false, "interactive live view")
	target := fs.String("target", "all", "switch target: all, cli, or desktop")
	relaunch := fs.Bool("relaunch", false, "relaunch Antigravity desktop after switching")
	closeDesktop := fs.Bool("close-desktop", false, "gracefully close Antigravity desktop before switching")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 1 {
		return a.usageFor("switch", "switch accepts at most one query")
	}
	if *live {
		if fs.NArg() > 0 {
			return a.usageFor("switch", "switch --live does not take a query")
		}
		return a.runLive(liveModeSwitch, false)
	}
	if a.JSON && fs.NArg() == 0 {
		return a.usageFor("switch", "switch --json requires a query")
	}

	tgt := strings.ToLower(strings.TrimSpace(*target))
	switch tgt {
	case "all", "cli", "desktop":
	default:
		return a.usageFor("switch", "target must be 'all', 'cli', or 'desktop'")
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
		selected, code := a.resolveOneWithUsage(reg, fs.Arg(0), "Select an account to activate")
		if code != 0 {
			return code
		}
		account = selected
	} else {
		selected, err := a.chooseAccountWithUsage(reg.Accounts, reg.Active, "Select an account to activate", accountNumbers(reg))
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(a.Err, "cancelled")
			return 1
		}
		if err != nil {
			return a.failCode("selection_failed", "Run `agy-auth list` and pass a query instead.", "could not select an account")
		}
		account = selected
	}

	now := a.now()
	var warnings []string
	var report agy.WriteReport
	var written []string
	cliSwitched := false

	if tgt == "all" || tgt == "cli" {
		raw, err := a.Store.LoadToken(account.ID)
		if err != nil {
			if tgt == "cli" {
				return a.failCode("registry_error", "Run `agy-auth status` to check storage health.", "could not read the stored credential for %s", account.DisplayName())
			}
			warnings = append(warnings, fmt.Sprintf("no CLI credential stored for %s; skipped CLI switch", account.DisplayName()))
		} else {
			tf, err := agy.ParseTokenFile(raw)
			if err != nil {
				if tgt == "cli" {
					return a.failCode("credential_error", "Remove and re-add the account, or sign in again with `agy-auth login`.", "stored credential for %s is unusable", account.DisplayName())
				}
				warnings = append(warnings, fmt.Sprintf("stored CLI credential for %s is unusable", account.DisplayName()))
			} else {
				if tf.Expired(now) {
					if !tf.HasRefreshToken() {
						if tgt == "cli" {
							return a.failCode("credential_error", "Sign in again with `agy-auth login`.", "%s has an expired access token and no refresh token", account.DisplayName())
						}
						warnings = append(warnings, fmt.Sprintf("%s has an expired access token and no refresh token", account.DisplayName()))
					} else {
						warnings = append(warnings, "the access token is expired; agy will refresh it on next start")
					}
				} else if !tf.HasRefreshToken() {
					warnings = append(warnings, "this snapshot has no refresh token; it will stop working when the access token expires")
				}

				report = a.Storage.Write(raw)
				if !report.OK() {
					if errors.Is(report.Error(), agy.ErrBackup) {
						return a.failCode("backup_error", "Check the backup directory permissions.", "could not back up the current agy credential")
					}
					return a.failCode("credential_write_failed", "Run `agy-auth status` to check keyring and file-mirror access.", "could not write the agy credential")
				}
				if err := report.Error(); err != nil {
					warnings = append(warnings, err.Error())
				}
				cliSwitched = true
				for _, source := range report.Written {
					if source == agy.SourceKeyring {
						written = append(written, "OS keyring")
					} else {
						written = append(written, "file mirror")
					}
				}
			}
		}
	}

	desktopSwitched := false
	desktopRelaunched := false
	if a.Desktop != nil && (tgt == "all" || tgt == "desktop") {
		deskProfile := a.Store.DesktopProfilePath(account.ID)
		hasDeskProfile := desktop.HasProfile(deskProfile)
		if !hasDeskProfile {
			if tgt == "desktop" {
				return a.failCode("desktop_profile_not_found", "Run `agy-auth add` while logged into Antigravity Desktop to capture its profile.", "no desktop profile stored for %s", account.DisplayName())
			}
		} else {
			running, pid, _ := a.Desktop.IsRunning()
			if running {
				if *relaunch || *closeDesktop {
					if err := a.Desktop.Terminate(pid, 5*time.Second); err != nil {
						warnings = append(warnings, fmt.Sprintf("failed to stop Antigravity Desktop (PID %d): %v", pid, err))
					}
				} else {
					if tgt == "desktop" {
						return a.failCode("desktop_running", "Close Antigravity Desktop first, or run with --relaunch.", "Antigravity Desktop is currently running (PID %d)", pid)
					}
					warnings = append(warnings, fmt.Sprintf("Antigravity Desktop is currently running (PID %d); desktop profile was not modified to prevent database corruption. Run with --relaunch to restart Desktop.", pid))
				}
			}

			if stillRunning, _, _ := a.Desktop.IsRunning(); !stillRunning {
				if err := a.Desktop.Restore(deskProfile); err != nil {
					if tgt == "desktop" {
						return a.failCode("desktop_restore_failed", "Check directory permissions for Antigravity desktop profile.", "could not restore desktop profile: %v", err)
					}
					warnings = append(warnings, fmt.Sprintf("could not restore desktop profile: %v", err))
				} else {
					desktopSwitched = true
					if *relaunch {
						if err := a.Desktop.Relaunch(); err != nil {
							warnings = append(warnings, fmt.Sprintf("could not relaunch Antigravity Desktop: %v", err))
						} else {
							desktopRelaunched = true
						}
					}
				}
			}
		}
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

	if a.JSON {
		for _, warning := range warnings {
			a.warn("%s", warning)
		}
		outMap := map[string]any{
			"account":            account,
			"target":             tgt,
			"cli_switched":       cliSwitched,
			"desktop_switched":   desktopSwitched,
			"desktop_relaunched": desktopRelaunched,
		}
		if cliSwitched {
			outMap["written"] = report.Written
		}
		if err := a.emitJSON(outMap); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Switched to %s.\n", account.DisplayName())
	if cliSwitched {
		fmt.Fprintf(a.Out, "Wrote %s.\n", strings.Join(written, " and "))
	}
	if desktopSwitched {
		fmt.Fprintln(a.Out, "Restored Antigravity Desktop profile.")
		if desktopRelaunched {
			fmt.Fprintln(a.Out, "Relaunched Antigravity Desktop.")
		}
	}
	for _, warning := range warnings {
		fmt.Fprintf(a.Err, "warning: %s\n", warning)
	}
	if cliSwitched {
		fmt.Fprintln(a.Out, "Restart agy for the new account to take effect (running sessions keep the old credential).")
	}
	return 0
}
