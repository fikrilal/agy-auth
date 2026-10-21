package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdList(args []string) int {
	fs := a.newFlagSet("list")
	activeOnly := fs.Bool("active", false, "only show the active account")
	noSync := fs.Bool("no-sync", false, "do not refresh the stored snapshot of the live account")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("list", "list takes no arguments")
	}

	reg, err := a.Store.Load()
	if err != nil {
		return a.fail("%v", err)
	}

	var currentRaw []byte
	var currentSource agy.Source
	var current *agy.TokenFile
	read, readErr := a.Storage.Read()
	if readErr == nil {
		currentRaw = read.Raw
		currentSource = read.Source
		current, _ = agy.ParseTokenFile(currentRaw)
	} else if !errors.Is(readErr, agy.ErrNoCredentials) {
		a.warn("%v", readErr)
	}

	var matched *registry.Account
	if current != nil {
		matched, _ = reg.Match(currentRaw)
		if matched != nil && !*noSync {
			if matched.Fingerprint != current.Fingerprint() || matched.Email != current.Email() {
				if _, err := a.Store.Upsert(currentRaw, "", false); err != nil {
					a.warn("could not refresh the stored snapshot of the live account: %v", err)
				} else if reloaded, err := a.Store.Load(); err != nil {
					a.warn("could not reload registry: %v", err)
				} else {
					reg = reloaded
					matched, _ = reg.Match(currentRaw)
				}
			}
		}
	}

	now := a.now()
	storedRows := make([]accountRow, 0, len(reg.Accounts))
	for i := range reg.Accounts {
		account := reg.Accounts[i]
		row := accountRow{
			Number:     i + 1,
			ID:         account.ID,
			Active:     matched != nil && matched.ID == account.ID,
			Stored:     true,
			Email:      account.Email,
			Alias:      account.Alias,
			AuthMethod: account.AuthMethod,
			AddedAt:    timePtr(account.AddedAt),
			LastUsedAt: timePtr(account.LastUsedAt),
		}
		if row.Active {
			row.Source = string(currentSource)
		}
		if raw, err := a.Store.LoadToken(account.ID); err == nil {
			if tf, err := agy.ParseTokenFile(raw); err == nil {
				if expiry, ok := tf.ExpiryTime(); ok {
					value := expiry
					row.ExpiresAt = &value
					row.Expired = !expiry.After(now)
				}
				if row.Email == "" {
					row.Email = tf.Email()
				}
				if row.AuthMethod == "" {
					row.AuthMethod = tf.AuthMethod
				}
			}
		}
		storedRows = append(storedRows, row)
	}

	var currentRow *accountRow
	if current != nil {
		if matched != nil {
			for i := range storedRows {
				if storedRows[i].ID == matched.ID {
					copied := storedRows[i]
					if copied.Email == "" {
						copied.Email = current.Email()
					}
					if copied.AuthMethod == "" {
						copied.AuthMethod = current.AuthMethod
					}
					currentRow = &copied
					break
				}
			}
		} else {
			row := accountRow{
				Active:     true,
				Stored:     false,
				Email:      current.Email(),
				AuthMethod: current.AuthMethod,
				Source:     string(currentSource),
			}
			if expiry, ok := current.ExpiryTime(); ok {
				value := expiry
				row.ExpiresAt = &value
				row.Expired = !expiry.After(now)
			}
			currentRow = &row
		}
	}

	displayRows := storedRows
	if matched == nil && currentRow != nil {
		displayRows = append(append([]accountRow{}, storedRows...), *currentRow)
	}
	if *activeOnly {
		if currentRow != nil {
			displayRows = []accountRow{*currentRow}
		} else {
			displayRows = nil
		}
	}

	if len(displayRows) == 0 {
		if a.JSON {
			_ = a.emitJSON(map[string]any{"accounts": []accountRow{}, "current": nil})
			return 0
		}
		if *activeOnly && len(reg.Accounts) > 0 {
			fmt.Fprintln(a.Out, "No active account. Run \"agy-auth switch\" to choose one.")
			return 0
		}
		fmt.Fprintln(a.Out, "No accounts yet.")
		fmt.Fprintln(a.Out, "Sign in with agy and run \"agy-auth add\", or run \"agy-auth login\".")
		return 0
	}

	if a.JSON {
		if *activeOnly {
			storedRows = filterActiveRows(storedRows, matched)
		}
		payload := map[string]any{"accounts": storedRows}
		if currentRow != nil {
			payload["current"] = currentRow
		} else {
			payload["current"] = nil
		}
		if err := a.emitJSON(payload); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}

	if currentRow != nil && !currentRow.Stored {
		fmt.Fprintln(a.Out, a.yellow("The live agy account is not in the registry. Run \"agy-auth add\" to store it."))
		fmt.Fprintln(a.Out)
	}
	a.renderAccountRows(displayRows, now)
	return 0
}

func filterActiveRows(rows []accountRow, matched *registry.Account) []accountRow {
	if matched == nil {
		return []accountRow{}
	}
	for _, row := range rows {
		if row.ID == matched.ID {
			return []accountRow{row}
		}
	}
	return []accountRow{}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func (a *App) cmdAdd(args []string) int {
	fs := a.newFlagSet("add")
	alias := fs.String("alias", "", "short name used by switch/remove queries")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("add", "add takes no positional arguments")
	}

	read, err := a.Storage.Read()
	if errors.Is(err, agy.ErrNoCredentials) {
		return a.fail("no agy credentials found; sign in with `agy` first, or use `agy-auth login`")
	}
	if err != nil {
		return a.fail("%v", err)
	}
	result, err := a.Store.Upsert(read.Raw, *alias, true)
	if err != nil {
		return a.fail("%v", err)
	}

	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"created": result.Created,
			"account": result.Account,
			"source":  read.Source,
		}); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	verb := "Updated"
	if result.Created {
		verb = "Added"
	}
	fmt.Fprintf(a.Out, "%s account %s [%s].\n", verb, result.Account.DisplayName(), result.Account.ID)
	return 0
}

func (a *App) cmdCurrent(args []string) int {
	fs := a.newFlagSet("current")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("current", "current takes no arguments")
	}

	read, err := a.Storage.Read()
	if errors.Is(err, agy.ErrNoCredentials) {
		return a.fail("no agy credentials found")
	}
	if err != nil {
		return a.fail("%v", err)
	}
	tf, err := agy.ParseTokenFile(read.Raw)
	if err != nil {
		return a.fail("%v", err)
	}
	now := a.now()
	row := accountRow{
		Active:     true,
		Stored:     true,
		Email:      tf.Email(),
		AuthMethod: tf.AuthMethod,
		Source:     string(read.Source),
	}
	if expiry, ok := tf.ExpiryTime(); ok {
		value := expiry
		row.ExpiresAt = &value
		row.Expired = !expiry.After(now)
	}
	if a.JSON {
		if err := a.emitJSON(row); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	name := row.Email
	if name == "" {
		name = "(unknown account)"
	}
	fmt.Fprintf(a.Out, "%s\n", name)
	fmt.Fprintf(a.Out, "  source: %s\n", read.Source)
	if row.ExpiresAt != nil {
		fmt.Fprintf(a.Out, "  token:  %s\n", humanUntil(*row.ExpiresAt, now))
	}
	fmt.Fprintf(a.Out, "  method: %s\n", valueOr(row.AuthMethod, "-"))
	return 0
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
