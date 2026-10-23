package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/usage"
)

func (a *App) emitJSON(data any) error {
	return a.writeJSON(Envelope{
		SchemaVersion: jsonSchemaVersion,
		Command:       a.commandName(),
		Data:          data,
	})
}

func (a *App) emitJSONError(exit int, doc ErrorDoc) int {
	envelope := Envelope{
		SchemaVersion: jsonSchemaVersion,
		Command:       a.commandName(),
		Error:         &doc,
	}
	if err := a.writeJSON(envelope); err != nil {
		fmt.Fprintf(a.Err, "error: %s\n", doc.Message)
	}
	return exit
}

func (a *App) writeJSON(value any) error {
	encoder := json.NewEncoder(a.Out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func (a *App) commandName() string {
	if a.Command == "" {
		return "unknown"
	}
	return a.Command
}

type accountRow struct {
	Number     int             `json:"number"`
	ID         string          `json:"id,omitempty"`
	Active     bool            `json:"active"`
	Stored     bool            `json:"stored"`
	Email      string          `json:"email,omitempty"`
	Alias      string          `json:"alias,omitempty"`
	AuthMethod string          `json:"auth_method,omitempty"`
	ExpiresAt  *time.Time      `json:"expires_at,omitempty"`
	Expired    bool            `json:"expired"`
	AddedAt    *time.Time      `json:"added_at,omitempty"`
	LastUsedAt *time.Time      `json:"last_used_at,omitempty"`
	Source     string          `json:"source,omitempty"`
	Plan       string          `json:"plan,omitempty"`
	Usage      *usage.Snapshot `json:"usage,omitempty"`
	UsageError string          `json:"usage_error,omitempty"`

	token  *agy.TokenFile
	detail *usage.Detail
	groups *usage.Groups
}

func (r accountRow) displayName() string {
	switch {
	case r.Alias != "" && r.Email != "":
		return r.Alias + " (" + r.Email + ")"
	case r.Email != "":
		return r.Email
	case r.Alias != "":
		return r.Alias
	case r.ID != "":
		return shortID(r.ID)
	default:
		return "(unknown)"
	}
}

func (r accountRow) groupLabel() string {
	switch {
	case r.Alias != "":
		return r.Alias
	case r.ID != "":
		return shortID(r.ID)
	default:
		return "(unknown)"
	}
}

type displayAccountRow struct {
	Header string
	Row    *accountRow
	Index  int
	Indent bool
}

func groupDisplayRows(rows []accountRow) []displayAccountRow {
	result := make([]displayAccountRow, 0, len(rows))
	for i := 0; i < len(rows); {
		end := i + 1
		email := rows[i].Email
		if email != "" {
			for end < len(rows) && strings.EqualFold(rows[end].Email, email) {
				end++
			}
		}
		if end-i > 1 {
			result = append(result, displayAccountRow{Header: email})
			for j := i; j < end; j++ {
				result = append(result, displayAccountRow{Row: &rows[j], Index: j, Indent: true})
			}
		} else {
			result = append(result, displayAccountRow{Row: &rows[i], Index: i})
		}
		i = end
	}
	return result
}

func (a *App) renderAccountRows(rows []accountRow, now time.Time) {
	a.renderAccountTable(rows, now, -1, "\n")
}

func (a *App) renderAccountTable(rows []accountRow, now time.Time, highlight int, newline string) {
	width := a.tableWidth()
	columns, withUsage := accountTableLayout(rows)
	displayRows := groupDisplayRows(rows)
	tableRows := make([]tableRow, 0, len(displayRows))
	for _, display := range displayRows {
		if display.Header != "" {
			header := display.Header
			if visibleWidth(header) > width {
				header = truncatePlain(header, width)
			}
			tableRows = append(tableRows, tableRow{Group: a.dim(header)})
			continue
		}
		tableRow := a.accountTableRow(*display.Row, now, nil, display.Indent, withUsage)
		tableRow.Highlight = display.Index == highlight
		tableRows = append(tableRows, tableRow)
	}
	a.renderTable(a.Out, width, columns, tableRows, newline)
}

func (a *App) accountTableRow(row accountRow, _ time.Time, checked *bool, indent, withUsage bool) tableRow {
	number := "-"
	if row.Number > 0 {
		number = fmt.Sprintf("%02d", row.Number)
	}
	marker := " "
	if row.Active {
		marker = a.green("*")
	}
	account := row.displayName()
	if indent {
		account = "  " + row.groupLabel()
	}
	if checked != nil {
		prefix := "[ ] "
		if *checked {
			prefix = "[x] "
		}
		account = prefix + account
	}
	if !row.Stored {
		account += " (not stored)"
	}
	cells := []string{number, marker, account}
	if withUsage {
		cells = append(cells,
			a.planCell(row),
			a.usageCell(row, false),
			a.usageCell(row, true),
			a.limitGroupCell(row),
			a.resetCell(row, false),
			a.resetCell(row, true),
		)
	}
	return tableRow{Cells: cells}
}

func (a *App) planCell(row accountRow) string {
	if row.Plan == "" {
		return "-"
	}
	return row.Plan
}

func (a *App) limitGroupCell(row accountRow) string {
	if row.Usage == nil {
		return "-"
	}
	if group := row.Usage.LimitGroup(); group != "" {
		return group
	}
	return "-"
}

func (a *App) resetCell(row accountRow, weekly bool) string {
	if row.Usage == nil {
		return "-"
	}
	window := row.Usage.FiveHour
	if weekly {
		window = row.Usage.Weekly
	}
	if window == nil || window.ResetsAt == nil {
		return "-"
	}
	return window.ResetsAt.Local().Format("2006-01-02 15:04")
}

func (a *App) usageCell(row accountRow, weekly bool) string {
	snapshot := row.Usage
	if snapshot == nil {
		return "-"
	}
	window := snapshot.FiveHour
	if weekly {
		window = snapshot.Weekly
	}
	if window == nil {
		return "-"
	}
	return a.quotaPercent(window.RemainingPercent, snapshot.Stale)
}

func (a *App) quotaPercent(percent int, stale bool) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	value := fmt.Sprintf("%d%%", percent)
	if stale {
		value += "*"
	}
	switch {
	case percent <= 20:
		return a.red(value)
	case percent <= 50:
		return a.yellow(value)
	default:
		return a.green(value)
	}
}

// visibleWidth approximates terminal width for the small set of styled cells
// used by agy-auth. ANSI escape sequences are ignored.
func visibleWidth(value string) int {
	width := 0
	inEscape := false
	for _, r := range value {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		width++
	}
	return width
}
