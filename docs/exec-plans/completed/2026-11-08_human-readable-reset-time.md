# Human-Readable Reset Time (Option 4)

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-08 |
| Related | `internal/cli/util.go`, `internal/cli/render.go`, `internal/cli/cmd_usage.go`, `internal/cli/cli_test.go` |

## Objective

Replace verbose ISO-like reset timestamps (`2026-11-08 11:03`) with a concise, human-readable "Time + Relative Duration" format (Option 4) across all CLI table displays (`list`, `list --all`, `usage`, `usage --all`, `switch` picker).

## Format Specification

- **If target is today (same local calendar day)**:
  - If target > now: `15:04 (Xh Ym)` or `15:04 (Xm)` (e.g. `11:03 (4h 35m)`)
  - If target <= now: `ready`
- **If target is within next 7 days**:
  - `Mon 15:04 (Xd)` (e.g. `Sat 10:13 (6d)`, `Mon 00:17 (1d)`)
- **If target is further than 7 days**:
  - `02 Jan 15:04 (Xd)` (e.g. `15 Nov 06:27 (7d)`)
- **If zero / nil**:
  - `-`

## Scope

### In scope

- Add `formatResetTime(target time.Time, now time.Time) string` in `internal/cli/util.go`.
- Use `formatResetTime` in `resetCell` in `internal/cli/render.go`.
- Use `formatResetTime` in `formatGroupReset` in `internal/cli/cmd_usage.go`.
- Update column width allowances in `tableColumn` if needed.
- Update `internal/cli/cli_test.go` (including `TestUsageCommand` at line 2652) and add dedicated unit tests for `formatResetTime`.

### Out of scope

- JSON outputs (must remain machine-readable RFC3339).
- Modifying usage API fetching or storage logic.

## Acceptance criteria

1. In `agy-auth list`, `agy-auth list --all`, and `agy-auth usage`, reset columns show concise time and relative countdown (e.g. `11:03 (4h 35m)`, `Sat 10:13 (6d)`).
2. Expired resets show `ready`.
3. Missing resets show `-`.
4. `make verify` and `make verify-full` pass.
