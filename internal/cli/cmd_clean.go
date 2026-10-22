package cli

import "fmt"

func (a *App) cmdClean(args []string) int {
	fs := a.newFlagSet("clean")
	dryRun := fs.Bool("dry-run", false, "show what would be removed")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("clean", "clean takes no positional arguments")
	}

	report, err := a.Store.Clean(*dryRun)
	if err != nil {
		return a.failCode("clean_failed", "Run `agy-auth status` and check permissions.", "could not clean storage")
	}
	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"dry_run":   *dryRun,
			"backups":   report.Backups,
			"snapshots": report.Snapshots,
			"preserved": report.Preserved,
		}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}

	if report.Preserved {
		fmt.Fprintln(a.Out, "Registry is missing; snapshots were preserved for rebuild.")
	}
	if len(report.Backups) == 0 && len(report.Snapshots) == 0 {
		if !report.Preserved {
			fmt.Fprintln(a.Out, "Nothing to clean.")
		}
		return 0
	}
	prefix := "Removed"
	if *dryRun {
		prefix = "Would remove"
	}
	if len(report.Backups) > 0 {
		fmt.Fprintf(a.Out, "%s %d backup(s):\n", prefix, len(report.Backups))
		for _, name := range report.Backups {
			fmt.Fprintf(a.Out, "  %s\n", name)
		}
	}
	if len(report.Snapshots) > 0 {
		fmt.Fprintf(a.Out, "%s %d orphan snapshot(s):\n", prefix, len(report.Snapshots))
		for _, name := range report.Snapshots {
			fmt.Fprintf(a.Out, "  %s\n", name)
		}
	}
	return 0
}
