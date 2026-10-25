package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdRotate(args []string) int {
	fs := a.newFlagSet("rotate")
	target := fs.String("target", "all", "switch target: all, cli, or desktop")
	relaunch := fs.Bool("relaunch", false, "relaunch Antigravity desktop after switching")
	closeDesktop := fs.Bool("close-desktop", false, "gracefully close Antigravity desktop before switching")
	force := fs.Bool("force", false, "rotate even if the active account is not exhausted")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("rotate", "rotate does not accept arguments")
	}

	tgt := strings.ToLower(strings.TrimSpace(*target))
	switch tgt {
	case "all", "cli", "desktop":
	default:
		return a.usageFor("rotate", "target must be 'all', 'cli', or 'desktop'")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.failCode("registry_error", "Run `agy-auth status` or `agy-auth path` to check storage.", "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts")
	}

	var currentRaw []byte
	var current *agy.TokenFile
	read, readErr := a.Storage.Read()
	if readErr == nil {
		currentRaw = read.Raw
		current, _ = agy.ParseTokenFile(currentRaw)
	} else if !errors.Is(readErr, agy.ErrNoCredentials) {
		a.warn("%v", readErr)
	}

	var matched *registry.Account
	if current != nil {
		matched, _ = reg.Match(currentRaw)
	}

	activeID := reg.Active
	if matched != nil {
		activeID = matched.ID
	}

	numbers := accountNumbers(reg)
	rows := a.pickerRows(reg.Accounts, activeID, numbers)
	if !a.JSON && a.OutTTY {
		fmt.Fprintln(a.Out, "Checking quota across accounts...")
	}
	a.enrichUsageFull(rows)

	var activeRow *accountRow
	var activeAccount *registry.Account
	for i := range rows {
		if rows[i].ID == activeID {
			activeRow = &rows[i]
			break
		}
	}
	if activeID != "" {
		activeAccount = reg.FindByID(activeID)
	}

	if !*force && activeRow != nil && activeRow.ID != "" && quotaDataFresh(activeRow) && !quotaExhausted(activeRow) {
		if a.JSON {
			if err := a.emitJSON(map[string]any{
				"rotated": false,
				"reason":  "active_healthy",
				"account": activeAccount,
			}); err != nil {
				return a.failCode("output_error", "", "could not write JSON output")
			}
			return 0
		}
		rem := quotaRemaining(activeRow)
		fmt.Fprintf(a.Out, "Active account %s is healthy (%d%% quota remaining). No rotation needed. Use --force to switch anyway.\n", activeRow.displayName(), rem)
		return 0
	}

	candidateRow := selectRotateCandidate(rows, activeID)
	if candidateRow == nil {
		return a.failCode("no_candidate_available", "Wait for quota to reset, or add an account with remaining quota.", "no stored accounts have remaining quota")
	}

	candidateAccount := reg.FindByID(candidateRow.ID)
	if candidateAccount == nil {
		return a.failCode("account_not_found", "Run `agy-auth list` to check stored accounts.", "candidate account %s not found in registry", candidateRow.ID)
	}

	res, code := a.switchAccount(reg, candidateAccount, switchOptions{
		Target:       tgt,
		Relaunch:     *relaunch,
		CloseDesktop: *closeDesktop,
	})
	if code != 0 {
		return code
	}

	reason := "active account exhausted"
	if *force {
		reason = "forced"
	} else if activeRow == nil || activeRow.ID == "" {
		reason = "no active account"
	}

	if a.JSON {
		for _, warning := range res.Warnings {
			a.warn("%s", warning)
		}
		outMap := map[string]any{
			"rotated":            true,
			"forced":             *force,
			"reason":             reason,
			"account":            res.Account,
			"target":             res.Target,
			"cli_switched":       res.CLISwitched,
			"desktop_switched":   res.DesktopSwitched,
			"desktop_relaunched": res.DesktopRelaunched,
		}
		if activeAccount != nil {
			outMap["previous_account"] = activeAccount
		}
		if res.CLISwitched {
			outMap["written"] = res.Report.Written
		}
		if err := a.emitJSON(outMap); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Rotated to %s (%s).\n", res.Account.DisplayName(), reason)
	if res.CLISwitched {
		fmt.Fprintf(a.Out, "Wrote %s.\n", strings.Join(res.Written, " and "))
	}
	if res.DesktopSwitched {
		fmt.Fprintln(a.Out, "Restored Antigravity Desktop profile.")
		if res.DesktopRelaunched {
			fmt.Fprintln(a.Out, "Relaunched Antigravity Desktop.")
		}
	}
	for _, warning := range res.Warnings {
		fmt.Fprintf(a.Err, "warning: %s\n", warning)
	}
	if res.CLISwitched {
		fmt.Fprintln(a.Out, "Restart agy for the new account to take effect (running sessions keep the old credential).")
	}
	return 0
}

func selectRotateCandidate(rows []accountRow, activeID string) *accountRow {
	var best *accountRow
	var bestReset time.Time
	for i := range rows {
		row := &rows[i]
		if row.ID == "" || row.ID == activeID || !quotaAvailable(row) {
			continue
		}
		reset := quotaNextReset(row)
		if best == nil || (!reset.IsZero() && (bestReset.IsZero() || reset.Before(bestReset))) {
			best = row
			bestReset = reset
			continue
		}
		if (reset.IsZero() && bestReset.IsZero()) || (!reset.IsZero() && reset.Equal(bestReset)) {
			if quotaRemaining(row) > quotaRemaining(best) {
				best = row
				bestReset = reset
			}
		}
	}
	return best
}

func quotaRemaining(row *accountRow) int {
	if row == nil {
		return 0
	}
	if groupDataUsable(row) {
		min := 100
		for _, group := range row.groups.Groups {
			for _, bucket := range group.Buckets {
				if bucket.RemainingPercent < min {
					min = bucket.RemainingPercent
				}
			}
		}
		return min
	}
	if row.Usage != nil && !row.Usage.Stale {
		if row.Usage.FiveHour != nil {
			return row.Usage.FiveHour.RemainingPercent
		}
		if row.Usage.Weekly != nil {
			return row.Usage.Weekly.RemainingPercent
		}
	}
	if row.detail != nil && !row.detail.Stale {
		lowest := row.detail.Lowest()
		if lowest != nil {
			return lowest.RemainingPercent
		}
	}
	return 0
}
