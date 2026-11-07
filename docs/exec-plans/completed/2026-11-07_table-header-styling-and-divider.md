# Table Header Styling and Horizontal Divider

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-07 |
| Related | `internal/cli/table.go`, `internal/cli/cli_test.go` |

## Objective

Match `codex-auth`'s visual hierarchy across all `agy-auth` tables (`list`, `list --all`, `usage`, `usage --all`, `switch`, `live`) by styling header column titles with cyan and inserting a dim horizontal divider line (`---`) between the header and data rows.

## Context

Currently, `renderTableRows` in `internal/cli/table.go` prints header titles in unstyled default text and directly follows with data rows. In contrast, `codex-auth` renders headers in cyan (`\033[36m`) and separates the header from table rows with a horizontal hyphen divider (`--------------------`) styled with dim (`\033[2m`).

Because all table outputs in `agy-auth` share `renderTableRows`, updating `renderTableRows` establishes consistent visual structure across all commands.

## Scope

### In scope

- Update `renderTableRows` in `internal/cli/table.go` to:
  - Render header column labels with `a.cyan`.
  - Print a horizontal hyphen divider line matching the visible width of the header row, styled with `a.dim`.
  - Ensure plain text fallback without ANSI escapes when `!a.OutTTY`, `a.NoColor`, or `NO_COLOR` is set.
- Update `TestRenderAccountTableInteractiveHighlight` in `internal/cli/cli_test.go` to account for the divider line at index 1 and assert divider presence.
- Add test coverage for header cyan coloring and divider line rendering with and without color.

### Out of scope

- Changing JSON output format or envelopes (tables are human-only).
- Changing column widths or column selection algorithms.

## Expected paths

- `internal/cli/table.go`
- `internal/cli/cli_test.go`

## Risks

- **Width overflow on narrow terminals:** Guarded by deriving the divider length from `visibleWidth(line)`, which already respects `app.Width`.
- **Breaking test assertions assuming data row starts on line 1:** Addressed by updating `TestRenderAccountTableInteractiveHighlight` where line indices are tested directly.

## Acceptance criteria

1. In TTY mode with colors enabled, table headers are printed in cyan (`\033[36m`) and followed by a dim hyphen divider line (`\033[2m---...\033[0m`).
2. When piped or when `NoColor` / `NO_COLOR` is active, header text is uncolored and the divider is plain hyphens (`---...`).
3. The divider line length matches the exact visible width of the rendered header row.
4. `make verify` and `make verify-full` pass.
