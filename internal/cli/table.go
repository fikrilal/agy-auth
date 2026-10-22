package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/term"
)

type tableColumn struct {
	Header   string
	Min      int
	Max      int
	Priority int
	Right    bool
	Required bool
}

type tableRow struct {
	Cells     []string
	Group     string
	Highlight bool
}

func accountTableLayout(rows []accountRow) ([]tableColumn, bool) {
	base := []tableColumn{
		{Header: "#", Min: 2, Max: 2, Priority: 0, Right: true, Required: true},
		{Header: "", Min: 1, Max: 1, Priority: 0, Required: true},
		{Header: "ACCOUNT", Min: 12, Max: 48, Priority: 1, Required: true},
	}
	status := []tableColumn{
		{Header: "EXPIRES", Min: 7, Max: 14, Priority: 2},
		{Header: "METHOD", Min: 6, Max: 10, Priority: 3},
		{Header: "LAST", Min: 10, Max: 10, Priority: 4},
	}
	for _, row := range rows {
		if row.Plan == "" && row.Usage == nil {
			continue
		}
		columns := append([]tableColumn{}, base...)
		columns = append(columns,
			tableColumn{Header: "PLAN", Min: 4, Max: 8, Priority: 2},
			tableColumn{Header: "5H", Min: 4, Max: 5, Priority: 3, Right: true},
			tableColumn{Header: "WEEKLY", Min: 6, Max: 10, Priority: 4, Right: true},
		)
		for _, column := range status {
			column.Priority += 3
			columns = append(columns, column)
		}
		return columns, true
	}
	return append(base, status...), false
}

func (a *App) tableWidth() int {
	if a.Width > 0 {
		return a.Width
	}
	if a.StdoutFile != nil {
		if width, _, err := term.GetSize(int(a.StdoutFile.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return 100
}

func (a *App) tableHeight() int {
	if a.Height > 0 {
		return a.Height
	}
	if a.StdoutFile != nil {
		if _, height, err := term.GetSize(int(a.StdoutFile.Fd())); err == nil && height > 0 {
			return height
		}
	}
	return 24
}

func (a *App) renderTable(out io.Writer, width int, columns []tableColumn, rows []tableRow, newline string) {
	a.renderTableRows(out, width, columns, rows, rows, newline)
}

func (a *App) renderTableRows(out io.Writer, width int, columns []tableColumn, widthRows []tableRow, rows []tableRow, newline string) {
	widths, selected := fitTableColumns(width, columns, widthRows)
	headers := make([]string, len(columns))
	for i := range columns {
		headers[i] = columns[i].Header
	}
	a.writeTableRow(out, columns, selected, widths, headers, false, newline)
	for _, row := range rows {
		if row.Group != "" {
			fmt.Fprint(out, row.Group+newline)
			continue
		}
		a.writeTableRow(out, columns, selected, widths, row.Cells, row.Highlight, newline)
	}
}

func fitTableColumns(width int, columns []tableColumn, rows []tableRow) ([]int, []int) {
	natural := make([]int, len(columns))
	for i, column := range columns {
		natural[i] = visibleWidth(column.Header)
		if natural[i] < column.Min {
			natural[i] = column.Min
		}
		if column.Max > 0 && natural[i] > column.Max {
			natural[i] = column.Max
		}
	}
	for _, row := range rows {
		if row.Group != "" {
			continue
		}
		for i := range columns {
			if i >= len(row.Cells) {
				continue
			}
			cellWidth := visibleWidth(row.Cells[i])
			if cellWidth > natural[i] {
				natural[i] = cellWidth
			}
			if columns[i].Max > 0 && natural[i] > columns[i].Max {
				natural[i] = columns[i].Max
			}
		}
	}

	chosen := make([]bool, len(columns))
	widths := make([]int, len(columns))
	for i, column := range columns {
		if column.Required {
			chosen[i] = true
			widths[i] = column.Min
		}
	}
	optional := make([]int, 0, len(columns))
	for i, column := range columns {
		if !column.Required {
			optional = append(optional, i)
		}
	}
	sort.Slice(optional, func(i, j int) bool {
		return columns[optional[i]].Priority < columns[optional[j]].Priority
	})
	for _, index := range optional {
		trialChosen := append([]bool(nil), chosen...)
		trialWidths := append([]int(nil), widths...)
		trialChosen[index] = true
		trialWidths[index] = natural[index]
		if chosenTotal(trialChosen, trialWidths) <= width {
			chosen = trialChosen
			widths = trialWidths
		}
	}

	accountIndex := 2
	if accountIndex < len(columns) {
		if total := chosenTotal(chosen, widths); total < width {
			grow := width - total
			if remaining := natural[accountIndex] - widths[accountIndex]; grow > remaining {
				grow = remaining
			}
			if grow > 0 {
				widths[accountIndex] += grow
			}
		} else if total > width {
			over := total - width
			if widths[accountIndex] > over+1 {
				widths[accountIndex] -= over
			} else {
				widths[accountIndex] = 1
			}
		}
	}

	selected := make([]int, 0, len(columns))
	for i := range columns {
		if chosen[i] {
			selected = append(selected, i)
		}
	}
	return widths, selected
}

func chosenTotal(chosen []bool, widths []int) int {
	total := 0
	count := 0
	for i := range chosen {
		if !chosen[i] {
			continue
		}
		if count > 0 {
			total += 2
		}
		total += widths[i]
		count++
	}
	return total
}

func (a *App) writeTableRow(out io.Writer, columns []tableColumn, selected []int, widths []int, cells []string, highlight bool, newline string) {
	var builder strings.Builder
	for position, index := range selected {
		if position > 0 {
			builder.WriteString("  ")
		}
		cell := ""
		if index < len(cells) {
			cell = cells[index]
		}
		cell = truncatePlain(cell, widths[index])
		padding := widths[index] - visibleWidth(cell)
		if columns[index].Right {
			if padding > 0 {
				builder.WriteString(strings.Repeat(" ", padding))
			}
			builder.WriteString(cell)
		} else {
			builder.WriteString(cell)
			if padding > 0 {
				builder.WriteString(strings.Repeat(" ", padding))
			}
		}
	}
	line := strings.TrimRight(builder.String(), " ")
	if highlight {
		line = a.bold(line)
	}
	fmt.Fprint(out, line+newline)
}

func truncatePlain(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(value) <= width {
		return value
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
