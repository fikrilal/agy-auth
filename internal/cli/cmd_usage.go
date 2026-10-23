package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
	"github.com/fikrilal/agy-auth/internal/usage"
)

const usageGroupTimeout = 60 * time.Second
const usageGroupWorkers = 3

func (a *App) cmdUsage(args []string) int {
	fs := a.newFlagSet("usage")
	all := fs.Bool("all", false, "show every stored account")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 1 {
		return a.usageFor("usage", "usage accepts at most one query")
	}
	if *all && fs.NArg() > 0 {
		return a.usageFor("usage", "usage --all does not take a query")
	}
	if *all {
		return a.cmdUsageAll()
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.registryFailure(err, "could not read the account registry")
	}
	account, tokenFile, code := a.usageTarget(reg, fs.Arg(0))
	if code != 0 {
		return code
	}
	if a.UsageGroups == nil {
		return a.failCode("quota_error", "Try again later.", "quota group provider is unavailable")
	}
	groups, err := a.fetchUsageGroups(tokenFile)
	if err != nil {
		return a.quotaFailure(err, usageLabel(account, tokenFile))
	}

	if a.JSON {
		payload := map[string]any{
			"account":     account,
			"email":       tokenFile.Email(),
			"description": groups.Description,
			"updated_at":  groups.UpdatedAt,
			"groups":      groups.Groups,
		}
		if err := a.emitJSON(payload); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Account: %s\n", usageLabel(account, tokenFile))
	if len(groups.Groups) == 0 {
		fmt.Fprintln(a.Out, "No quota groups returned.")
		return 0
	}
	fmt.Fprintln(a.Out)
	a.renderUsageGroupTable(groups)
	return 0
}

func (a *App) fetchUsageGroups(token *agy.TokenFile) (usage.Groups, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageGroupTimeout)
	defer cancel()
	return a.UsageGroups.FetchGroups(ctx, token)
}

type usageAccount struct {
	row    accountRow
	token  *agy.TokenFile
	groups usage.Groups
	err    error
}

func (a *App) cmdUsageAll() int {
	if a.UsageGroups == nil {
		return a.failCode("quota_error", "Try again later.", "quota group provider is unavailable")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.registryFailure(err, "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts")
	}
	rows := a.pickerRows(reg.Accounts, reg.Active, accountNumbers(reg))
	accounts := make([]usageAccount, len(rows))
	for i := range rows {
		accounts[i].row = rows[i]
		if rows[i].token == nil {
			accounts[i].err = usage.ErrMissingAuth
			continue
		}
		accounts[i].token = rows[i].token
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageGroupTimeout)
	defer cancel()
	jobs := make(chan int)
	workers := usageGroupWorkers
	if len(accounts) < workers {
		workers = len(accounts)
	}
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				if accounts[index].token == nil {
					continue
				}
				groups, err := a.UsageGroups.FetchGroups(ctx, accounts[index].token)
				accounts[index].groups = groups
				accounts[index].err = err
			}
		}()
	}
	for i := range accounts {
		if accounts[i].token != nil {
			jobs <- i
		}
	}
	close(jobs)
	wait.Wait()

	if !a.JSON {
		failures := map[string]int{}
		for i := range accounts {
			if accounts[i].err != nil {
				failures[usage.ShortError(accounts[i].err)]++
			}
		}
		a.warnUsageFailures(failures)
	}

	if a.JSON {
		payload := make([]map[string]any, 0, len(accounts))
		for i := range accounts {
			item := map[string]any{
				"number": accounts[i].row.Number,
				"id":     accounts[i].row.ID,
				"email":  accounts[i].row.Email,
				"alias":  accounts[i].row.Alias,
				"active": accounts[i].row.Active,
			}
			if accounts[i].err == nil {
				item["description"] = accounts[i].groups.Description
				item["updated_at"] = accounts[i].groups.UpdatedAt
				item["groups"] = accounts[i].groups.Groups
			} else {
				item["usage_error"] = usage.ShortError(accounts[i].err)
			}
			payload = append(payload, item)
		}
		if err := a.emitJSON(map[string]any{"accounts": payload}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	a.renderUsageAllTable(accounts)
	return 0
}

func (a *App) renderUsageAllTable(accounts []usageAccount) {
	columns := []tableColumn{
		{Header: "#", Min: 2, Max: 2, Priority: 0, Right: true, Required: true},
		{Header: "ACCOUNT", Min: 16, Max: 36, Priority: 1, Required: true, Flex: true},
		{Header: "GROUP", Min: 20, Max: 32, Priority: 2, Required: true},
		{Header: "5H", Min: 5, Max: 5, Priority: 3, Right: true},
		{Header: "5H RESET", Min: 16, Max: 16, Priority: 4},
		{Header: "WEEKLY", Min: 8, Max: 8, Priority: 5, Right: true},
		{Header: "WEEKLY RESET", Min: 16, Max: 16, Priority: 6},
	}
	rows := make([]tableRow, 0, len(accounts)*3)
	for accountIndex, account := range accounts {
		if accountIndex > 0 {
			rows = append(rows, tableRow{Spacer: true})
		}
		label := account.row.displayName()
		if account.row.Active {
			label = "* " + label
		}
		number := fmt.Sprintf("%02d", account.row.Number)
		if account.err != nil {
			rows = append(rows, tableRow{Cells: []string{
				number, label, "(usage unavailable: " + usage.ShortError(account.err) + ")", "-", "-", "-", "-",
			}})
			continue
		}
		if len(account.groups.Groups) == 0 {
			rows = append(rows, tableRow{Cells: []string{number, label, "(no quota groups returned)", "-", "-", "-", "-"}})
			continue
		}
		for i, group := range account.groups.Groups {
			rowNumber := ""
			rowLabel := ""
			if i == 0 {
				rowNumber = number
				rowLabel = label
			}
			five := groupBucket(group, "5h")
			weekly := groupBucket(group, "weekly")
			rows = append(rows, tableRow{Cells: []string{
				rowNumber,
				rowLabel,
				group.Name,
				a.groupPercent(five),
				formatGroupReset(five),
				a.groupPercent(weekly),
				formatGroupReset(weekly),
			}})
		}
	}
	a.renderTable(a.Out, a.tableWidth(), columns, rows, "\n")
}

func (a *App) renderUsageGroupTable(groups usage.Groups) {
	columns := []tableColumn{
		{Header: "GROUP", Min: 20, Max: 32, Priority: 0, Required: true, Flex: true},
		{Header: "5H", Min: 5, Max: 5, Priority: 1, Right: true},
		{Header: "5H RESET", Min: 16, Max: 16, Priority: 2},
		{Header: "WEEKLY", Min: 8, Max: 8, Priority: 3, Right: true},
		{Header: "WEEKLY RESET", Min: 16, Max: 16, Priority: 4},
	}
	rows := make([]tableRow, 0, len(groups.Groups))
	for _, group := range groups.Groups {
		five := groupBucket(group, "5h")
		weekly := groupBucket(group, "weekly")
		rows = append(rows, tableRow{Cells: []string{
			group.Name,
			a.groupPercent(five),
			formatGroupReset(five),
			a.groupPercent(weekly),
			formatGroupReset(weekly),
		}})
	}
	a.renderTable(a.Out, a.tableWidth(), columns, rows, "\n")
}

func (a *App) groupPercent(bucket *usage.GroupBucket) string {
	if bucket == nil {
		return "-"
	}
	return a.quotaPercent(bucket.RemainingPercent, false)
}

func groupBucket(group usage.Group, window string) *usage.GroupBucket {
	var best *usage.GroupBucket
	for i := range group.Buckets {
		bucket := &group.Buckets[i]
		if normalizeUsageWindow(bucket.Window) != window {
			continue
		}
		if best == nil || bucket.RemainingFraction < best.RemainingFraction {
			best = bucket
		}
	}
	return best
}

func normalizeUsageWindow(window string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "5-hour", "five_hour", "five-hour":
		return "5h"
	case "weekly", "week":
		return "weekly"
	default:
		return ""
	}
}

func formatGroupReset(bucket *usage.GroupBucket) string {
	if bucket == nil || strings.TrimSpace(bucket.ResetTime) == "" {
		return "-"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, bucket.ResetTime); err == nil {
			return parsed.Local().Format("2006-01-02 15:04")
		}
	}
	return bucket.ResetTime
}

func (a *App) usageTarget(reg *registry.Registry, query string) (*registry.Account, *agy.TokenFile, int) {
	if query != "" {
		selected, code := a.resolveOne(reg, query, "Select an account for usage")
		if code != 0 {
			return nil, nil, code
		}
		raw, err := a.Store.LoadToken(selected.ID)
		if err != nil {
			return nil, nil, a.failCode("registry_error", "Run `agy-auth status` to check storage health.", "could not read the stored credential for %s", selected.DisplayName())
		}
		tokenFile, err := agy.ParseTokenFile(raw)
		if err != nil {
			return nil, nil, a.failCode("credential_error", "Remove and re-add the account, or sign in again with `agy-auth login`.", "stored credential for %s is unusable", selected.DisplayName())
		}
		return selected, tokenFile, 0
	}

	read, err := a.Storage.Read()
	if err == nil {
		tokenFile, parseErr := agy.ParseTokenFile(read.Raw)
		if parseErr != nil {
			return nil, nil, a.failCode("credential_error", "Sign in with agy again, or use `agy-auth login`.", "the live agy credential is invalid")
		}
		matched, _ := reg.Match(read.Raw)
		return matched, tokenFile, 0
	}
	if !errors.Is(err, agy.ErrNoCredentials) {
		return nil, nil, a.failCode("credential_error", "Run `agy-auth status` to check keyring and file-mirror access.", "could not read the live agy credential")
	}
	if reg.Active == "" {
		return nil, nil, a.failCode("credential_not_found", "Sign in with `agy` first, or use `agy-auth login`.", "no live or active agy credential")
	}
	selected := reg.FindByID(reg.Active)
	if selected == nil {
		return nil, nil, a.failCode("credential_not_found", "Run `agy-auth list` to check the active account.", "the active account is no longer stored")
	}
	raw, err := a.Store.LoadToken(selected.ID)
	if err != nil {
		return nil, nil, a.failCode("registry_error", "Run `agy-auth status` to check storage health.", "could not read the stored credential for %s", selected.DisplayName())
	}
	tokenFile, err := agy.ParseTokenFile(raw)
	if err != nil {
		return nil, nil, a.failCode("credential_error", "Remove and re-add the account, or sign in again with `agy-auth login`.", "stored credential for %s is unusable", selected.DisplayName())
	}
	return selected, tokenFile, 0
}

func (a *App) quotaFailure(err error, label string) int {
	code := "quota_error"
	hint := "Try again later; `agy-auth list` still shows the shared quota summary."
	switch {
	case errors.Is(err, usage.ErrMissingAuth), errors.Is(err, usage.ErrRefresh), errors.Is(err, usage.ErrAuth):
		code = "credential_error"
		hint = "Sign in again with `agy-auth login`."
	case errors.Is(err, usage.ErrForbidden), errors.Is(err, usage.ErrUnsupported):
		hint = "This account may not expose quota data."
	}
	return a.failCode(code, hint, "could not fetch quota details for %s (%s)", label, usage.ShortError(err))
}

func usageLabel(account *registry.Account, tokenFile *agy.TokenFile) string {
	if account != nil {
		return account.DisplayName()
	}
	if tokenFile != nil {
		if email := tokenFile.Email(); email != "" {
			return email + " (live)"
		}
	}
	return "the live account"
}
