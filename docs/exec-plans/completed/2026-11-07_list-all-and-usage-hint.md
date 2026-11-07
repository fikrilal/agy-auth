# `list --all` and quota group hint

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-07 |
| Related | `internal/cli/cmd_list.go`, `internal/cli/cmd_usage.go`, `internal/cli/spec.go`, `docs/user-guide.md` |

## Objective

Support `agy-auth list --all` to show the full multi-group quota comparison for all stored accounts directly from `list`, and display an inline footer hint on human-facing `list` output clarifying that `GROUP` displays the limiting bottleneck group and how to view all groups.

## Context

`agy-auth list` collapses quota across named groups (`Gemini Models`, `Claude and GPT models`) to the lowest remaining percentage window, labeling the column `GROUP`. Users frequently mistake this for a sticky or buggy state (e.g. "stuck on Gemini Models after using Gemini 4 Argon", or unused accounts tie-breaking to Gemini Models at 100%).

Replacing the default `list` table with a multi-row view would break the 1-account-per-row model required for interactive pickers, arrow navigation, and narrow terminals. Instead, adding `--all` directly to `list` and printing a proactive footer hint eliminates confusion while preserving compact defaults.

## Scope

### In scope

- Add `--all` flag to `agy-auth list` in `internal/cli/cmd_list.go`.
- When `list --all` is invoked, render the full multi-group table (reusing `cmdUsageAll` formatting).
- When `list --all --json` is invoked, return machine-readable all-groups quota data.
- Print a subtle footer hint on human-facing interactive `list` runs when quota is displayed:
  `Tip: Showing limiting quota groups. Run 'agy-auth list --all' to see all groups.`
- Suppress the footer hint in `--json`, `--no-usage`, `--live`, and non-TTY piped environments.
- Update `internal/cli/spec.go` (`CommandSpec` for `list`), `README.md`, and `docs/user-guide.md`.
- Add unit and CLI tests in `internal/cli/cli_test.go` covering `list --all`, `--json`, and hint rendering.

### Out of scope

- Changing the default 1-row-per-account layout of `list`.
- Modifying backend quota fetching or caching logic in `internal/usage`.
- Modifying interactive live views (`list --live`, `switch --live`).

## Expected paths

- `internal/cli/cmd_list.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `README.md`
- `docs/user-guide.md`

## Risks

- **AST Lint Violations:** Printing output must strictly adhere to the `output-boundary` lint (writing to `a.Out` or `a.Err` inside `internal/cli`).
- **Terminal Width/Piping:** Footer hints must never pollute piped scripts or machine-readable `--json` streams.
- **Flag Conflicts:** `--all` combined with `--live` must be cleanly rejected since live views do not support multi-line row expansions.

## Acceptance criteria

- [x] `agy-auth list --all` renders all quota groups across all stored accounts.
- [x] `agy-auth list --all --json` returns structured JSON with all quota groups.
- [x] Default `agy-auth list` on TTY prints the tip footer below the table when quota is visible.
- [x] Piped stdout or `--json` outputs omit the tip footer.
- [x] `list --all` with `--live` is rejected with an actionable error.
- [x] Help text for `list` documents `--all`.
- [x] `make verify` passes cleanly with zero lint or contract violations.

## Verification plan

```shell
go test -v ./internal/cli -run "TestList"
make verify
make verify-full
```

Manual checks:
- Verify `bin/agy-auth list` on terminal shows the hint.
- Verify `bin/agy-auth list | cat` does not show the hint.
- Verify `bin/agy-auth list --json` does not include text hints in JSON.
- Verify `bin/agy-auth list --all` renders full group table.

## Rollback

Revert the commits touching `cmd_list.go`, `spec.go`, `cli_test.go`, and documentation. No database migrations or credential formats are affected.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-11-07 | Add `--all` to `list` rather than changing default layout | Preserves 1-row-per-account compactness for pickers while giving users instant access to uncollapsed quota. |
| 2026-11-07 | Add footer hint on TTY list output | Directly educates users that `GROUP` represents the limiting bottleneck, preventing bug reports. |

## Completion

- Commit/tag: Pending commit authorization
- Verification result: `make verify` and `make verify-full` pass; `TestListAll`, `TestListAllFlagsValidation`, and `TestListQuotaTip` pass.
- Residual risk: None. Defaults remain backward compatible and `--json` envelopes are unchanged.
