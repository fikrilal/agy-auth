package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
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

func (a *App) renderAccountRows(rows []accountRow, now time.Time) {
	headers := []string{"#", "", "ACCOUNT", "METHOD", "TOKEN", "ADDED"}
	table := make([][]string, 0, len(rows))
	for _, row := range rows {
		number := "-"
		if row.Number > 0 {
			number = fmt.Sprintf("%02d", row.Number)
		}
		marker := " "
		if row.Active {
			marker = a.green("*")
		}
		name := row.displayName()
		if !row.Stored {
			name += a.dim(" (not stored)")
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
		added := "-"
		if row.AddedAt != nil {
			added = row.AddedAt.Format("2006-01-02")
		}
		method := row.AuthMethod
		if method == "" {
			method = "-"
		}
		table = append(table, []string{number, marker, name, method, expiry, added})
	}
	writeTable(a.Out, headers, table)
}

func writeTable(out io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i := range headers {
			if i >= len(row) {
				continue
			}
			if width := visibleWidth(row[i]); width > widths[i] {
				widths[i] = width
			}
		}
	}

	writeLine := func(cells []string) {
		var builder strings.Builder
		for i := range headers {
			if i > 0 {
				builder.WriteString("  ")
			}
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			builder.WriteString(cell)
			padding := widths[i] - visibleWidth(cell)
			if padding > 0 {
				builder.WriteString(strings.Repeat(" ", padding))
			}
		}
		fmt.Fprintln(out, strings.TrimRight(builder.String(), " "))
	}

	writeLine(headers)
	for _, row := range rows {
		writeLine(row)
	}
}

// visibleWidth approximates terminal width for the small set of styled cells
// used by agy-auth. ANSI escape sequences and dim() markers are ignored.
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
