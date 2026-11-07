# Bold Yellow Active Marker and Table Column Alignment

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-08 |
| Related | `internal/cli/render.go`, `internal/cli/cmd_usage.go`, `internal/cli/cli_test.go` |

## Objective

Render the active account marker `*` in bold yellow across all table views (`list`, `list --all`, `usage --all`, `switch` picker, `live`), and align `list --all` / `usage --all` to use a dedicated marker column so all account emails are vertically aligned.

## Context

Previously, `agy-auth list` rendered the active marker in regular green (`a.green("*")`), while `agy-auth list --all` prepended an uncolored `* ` string directly into the `ACCOUNT` column. This caused the active account's asterisk to be white/uncolored in `list --all` and misaligned active vs inactive account emails.

Changing the active marker to bold yellow (`a.bold(a.yellow("*"))`) gives it high contrast against white text, cyan headers, and green quota percentages. Adding a dedicated marker column in `renderUsageAllTable` ensures perfect vertical alignment.

## Scope

### In scope

- Update `accountTableRow` in `internal/cli/render.go` to style `marker` as `a.bold(a.yellow("*"))`.
- Update `renderUsageAllTable` in `internal/cli/cmd_usage.go` to:
  - Add the dedicated marker column (`{Header: "", Min: 1, Max: 1, Priority: 0, Required: true}`).
  - Style the active marker as `a.bold(a.yellow("*"))` and empty `" "` for inactive accounts.
  - Maintain vertical alignment for account labels.
- Add unit test coverage in `internal/cli/cli_test.go` verifying bold yellow marker styling in color mode and unstyled fallback in no-color mode.

### Out of scope

- Bolding or modifying account name typography (per user request).
- Changing JSON outputs or envelopes.

## Acceptance criteria

1. Active accounts show a bold yellow asterisk (`\x1b[1m\x1b[33m*\x1b[0m\x1b[0m`) in `agy-auth list`, `agy-auth list --all`, `agy-auth usage --all`, and `agy-auth switch` picker when colors are enabled.
2. In `list --all` and `usage --all`, account emails are vertically aligned in the `ACCOUNT` column, with the `*` in its own column between `#` and `ACCOUNT`.
3. Plain-text fallback (`NoColor`, `NO_COLOR`, non-TTY) renders a plain `*` without ANSI escape sequences.
4. `make verify` and `make verify-full` pass.
