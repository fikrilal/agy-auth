package cli

import (
	"fmt"
	"path/filepath"

	"github.com/fikrilal/agy-auth/internal/registry"
)

type transferOutcome string

const (
	outcomeImported transferOutcome = "imported"
	outcomeUpdated  transferOutcome = "updated"
	outcomeSkipped  transferOutcome = "skipped"
	outcomeExported transferOutcome = "exported"
)

type transferEvent struct {
	Path    string            `json:"path,omitempty"`
	Outcome transferOutcome   `json:"outcome"`
	Reason  string            `json:"reason,omitempty"`
	Account *registry.Account `json:"account,omitempty"`
}

type importSummary struct {
	Imported int `json:"imported"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
	Total    int `json:"total"`
}

type importReport struct {
	Files     []transferEvent    `json:"files"`
	Imported  []registry.Account `json:"imported"`
	Summary   importSummary      `json:"summary"`
	Activated bool               `json:"activated"`
	Rebuilt   bool               `json:"rebuilt,omitempty"`
	Active    string             `json:"active,omitempty"`
	Previous  string             `json:"previous,omitempty"`
}

type exportSummary struct {
	Exported int `json:"exported"`
	Skipped  int `json:"skipped"`
	Total    int `json:"total"`
}

type exportedAccount struct {
	Account registry.Account `json:"account"`
	Path    string           `json:"path"`
}

type exportReport struct {
	Directory string            `json:"directory"`
	Files     []transferEvent   `json:"files"`
	Exported  []exportedAccount `json:"exported"`
	Summary   exportSummary     `json:"summary"`
}

func summarizeImport(report *importReport) {
	report.Summary = importSummary{Total: len(report.Files)}
	for _, event := range report.Files {
		switch event.Outcome {
		case outcomeImported:
			report.Summary.Imported++
		case outcomeUpdated:
			report.Summary.Updated++
		case outcomeSkipped:
			report.Summary.Skipped++
		}
	}
}

func (a *App) renderImportReport(report *importReport) {
	for _, event := range report.Files {
		label := filepath.Base(event.Path)
		switch event.Outcome {
		case outcomeImported, outcomeUpdated:
			fmt.Fprintf(a.Out, "  %-8s  %s\n", event.Outcome, label)
		case outcomeSkipped:
			fmt.Fprintf(a.Err, "  skipped  %s: %s\n", label, event.Reason)
		}
	}
	if report.Rebuilt {
		fmt.Fprintf(a.Out, "Rebuilt registry: %d imported, %d skipped (total %d snapshots)\n",
			report.Summary.Imported, report.Summary.Skipped, report.Summary.Total)
	} else {
		fmt.Fprintf(a.Out, "Import Summary: %d imported, %d updated, %d skipped (total %d files)\n",
			report.Summary.Imported, report.Summary.Updated, report.Summary.Skipped, report.Summary.Total)
	}
	if report.Activated {
		fmt.Fprintln(a.Out, "Activated the imported account. Restart agy for it to take effect.")
	}
}

func (a *App) renderExportReport(report *exportReport) {
	for _, event := range report.Files {
		switch event.Outcome {
		case outcomeExported:
			fmt.Fprintf(a.Out, "  exported  %s\n", event.Path)
		case outcomeSkipped:
			label := "account"
			if event.Account != nil {
				label = event.Account.DisplayName()
			}
			fmt.Fprintf(a.Err, "  skipped   %s: %s\n", label, event.Reason)
		}
	}
	fmt.Fprintf(a.Out, "Exported %d account(s) to %s", report.Summary.Exported, report.Directory)
	if report.Summary.Skipped > 0 {
		fmt.Fprintf(a.Out, ", %d skipped", report.Summary.Skipped)
	}
	fmt.Fprintln(a.Out)
}
