package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
	"github.com/fikrilal/agy-auth/internal/usage"
)

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
	if a.UsageDetail == nil {
		return a.failCode("quota_error", "Run `agy-auth list` for the shared quota summary.", "quota detail provider is unavailable")
	}
	detail, err := a.UsageDetail.FetchDetail(context.Background(), tokenFile)
	if err != nil {
		return a.quotaFailure(err, usageLabel(account, tokenFile))
	}

	if a.JSON {
		payload := map[string]any{
			"account":    account,
			"email":      tokenFile.Email(),
			"plan":       detail.Plan,
			"updated_at": detail.UpdatedAt,
			"buckets":    detail.Buckets,
		}
		if err := a.emitJSON(payload); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	fmt.Fprintf(a.Out, "Account: %s\n", usageLabel(account, tokenFile))
	if detail.Plan != "" {
		fmt.Fprintf(a.Out, "Plan: %s\n", detail.Plan)
	}
	if len(detail.Buckets) == 0 {
		fmt.Fprintln(a.Out, "No quota buckets returned.")
		return 0
	}
	fmt.Fprintln(a.Out)
	columns := []tableColumn{
		{Header: "MODEL", Min: 16, Max: 48, Priority: 1, Required: true, Flex: true},
		{Header: "REMAINING", Min: 9, Max: 9, Priority: 2, Right: true},
		{Header: "RESETS", Min: 16, Max: 25, Priority: 3},
	}
	rows := make([]tableRow, 0, len(detail.Buckets))
	for _, bucket := range detail.Buckets {
		rows = append(rows, tableRow{Cells: []string{
			bucket.ModelID,
			a.quotaPercent(bucket.RemainingPercent, false),
			formatQuotaReset(bucket.ResetsAt),
		}})
	}
	a.renderTable(a.Out, a.tableWidth(), columns, rows, "\n")
	return 0
}

type usageAllRow struct {
	accountRow
	Buckets []usage.Model `json:"buckets,omitempty"`
	Lowest  *usage.Model  `json:"lowest,omitempty"`
}

func (a *App) cmdUsageAll() int {
	if a.UsageFull == nil {
		return a.failCode("quota_error", "Run `agy-auth list` for the shared quota summary.", "quota detail provider is unavailable")
	}
	reg, err := a.Store.Load()
	if err != nil {
		return a.registryFailure(err, "could not read the account registry")
	}
	if len(reg.Accounts) == 0 {
		return a.failCode("account_not_found", "Run `agy-auth add` or `agy-auth login` first.", "no stored accounts")
	}
	rows := a.pickerRows(reg.Accounts, reg.Active, accountNumbers(reg))
	failures := a.enrichUsageFull(rows)
	for i := range rows {
		if rows[i].token == nil && rows[i].UsageError == "" {
			rows[i].UsageError = "missing_auth"
			if failures == nil {
				failures = map[string]int{}
			}
			failures["missing_auth"]++
		}
	}
	if !a.JSON {
		a.warnUsageFailures(failures)
	}

	if a.JSON {
		payload := make([]usageAllRow, 0, len(rows))
		for _, row := range rows {
			item := usageAllRow{accountRow: row}
			if row.detail != nil {
				item.Buckets = row.detail.Buckets
				item.Lowest = row.detail.Lowest()
			}
			payload = append(payload, item)
		}
		if err := a.emitJSON(map[string]any{"accounts": payload}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	columns := []tableColumn{
		{Header: "#", Min: 2, Max: 2, Priority: 0, Right: true, Required: true},
		{Header: "ACCOUNT", Min: 12, Max: 40, Priority: 1, Required: true, Flex: true},
		{Header: "PLAN", Min: 4, Max: 8, Priority: 2},
		{Header: "5H", Min: 4, Max: 5, Priority: 3, Right: true},
		{Header: "WEEKLY", Min: 6, Max: 10, Priority: 4, Right: true},
		{Header: "LOWEST", Min: 6, Max: 6, Priority: 5, Right: true},
		{Header: "MODEL", Min: 12, Max: 32, Priority: 6},
		{Header: "RESETS", Min: 16, Max: 16, Priority: 7},
	}
	tableRows := make([]tableRow, 0, len(rows))
	for _, row := range rows {
		label := row.displayName()
		if row.Active {
			label = "* " + label
		}
		lowest := "-"
		model := "-"
		reset := "-"
		if row.detail != nil {
			if value := row.detail.Lowest(); value != nil {
				lowest = a.quotaPercent(value.RemainingPercent, row.detail.Stale)
				model = value.ModelID
				reset = formatQuotaReset(value.ResetsAt)
			}
		}
		tableRows = append(tableRows, tableRow{Cells: []string{
			fmt.Sprintf("%02d", row.Number),
			label,
			a.planCell(row),
			a.usageCell(row, false),
			a.usageCell(row, true),
			lowest,
			model,
			reset,
		}})
	}
	a.renderTable(a.Out, a.tableWidth(), columns, tableRows, "\n")
	return 0
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
		hint = "This account may not expose per-model quota data."
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

func formatQuotaReset(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}
