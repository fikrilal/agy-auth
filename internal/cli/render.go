package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
	Number     int        `json:"number"`
	ID         string     `json:"id,omitempty"`
	Active     bool       `json:"active"`
	Stored     bool       `json:"stored"`
	Email      string     `json:"email,omitempty"`
	Alias      string     `json:"alias,omitempty"`
	AuthMethod string     `json:"auth_method,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Expired    bool       `json:"expired"`
	AddedAt    *time.Time `json:"added_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Source     string     `json:"source,omitempty"`
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
		row := *display.Row
		number := "-"
		if row.Number > 0 {
			number = fmt.Sprintf("%02d", row.Number)
		}
		marker := " "
		if row.Active {
			marker = a.green("*")
		}
		account := row.displayName()
		if display.Indent {
			account = "  " + row.groupLabel()
		}
		if !row.Stored {
			account += " (not stored)"
		}
		method := row.AuthMethod
		if method == "" {
			method = "-"
		}
		expiry := "-"
		if row.ExpiresAt != nil {
			expiry = humanUntil(*row.ExpiresAt, now)
			if row.Expired {
				expiry = a.red("expired")
			} else if row.ExpiresAt.Sub(now) < 24*time.Hour {
				expiry = a.yellow(expiry)
			}
		}
		last := "-"
		if row.LastUsedAt != nil {
			last = row.LastUsedAt.Format("2006-01-02")
		}
		tableRows = append(tableRows, tableRow{
			Cells:     []string{number, marker, account, expiry, method, last},
			Highlight: display.Index == highlight,
		})
	}
	a.renderTable(a.Out, width, accountTableColumns, tableRows, newline)
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
