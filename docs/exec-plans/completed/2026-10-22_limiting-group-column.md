# Limiting group column

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Group-aware auto-switch](2026-10-22_group-aware-auto-switch.md); [Internals](../../internals.md) |

## Objective

Give the collapsed `list`/live `5H` and `WEEKLY` values context by adding a `GROUP` column that names the quota group owning the lower displayed value.

## Context

The summary fetch stores the lowest 5H and weekly percentages across named groups. Without a label, a weekly value of 58% could look like an account-wide number when it is actually the Gemini group. `usage` shows all groups, but the one-row account table still needs a compact hint.

## Scope

### In scope

- Add `Group` to `usage.Window`, populated by `applySummary`.
- Add `Snapshot.LimitGroup()` choosing the group for the lower window.
- Add `GROUP` to the shared account table (list, live, pickers when usage exists).
- Include group names in list JSON window objects.
- Tests for tie/selection, table headers/cells, and JSON.
- Docs/changelog.

### Out of scope

- Expanding `list` into multiple rows per account.
- Changing the `usage` group table.
- Adding per-group columns to live views.

## Expected paths

- `internal/usage/{usage,usage_test}.go`
- `internal/cli/{render,table,cli_test}.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`

## Risks

- A tie between windows falls back to the 5H group; the label is a hint, not an exhaustive breakdown.
- Group names can be long; the column truncates within the width budget.
- Accounts with plan-only data get `-` for the group.

## Acceptance criteria

- [x] `GROUP` names the group for the lower of 5H/weekly.
- [x] Ties prefer a non-empty 5H group.
- [x] List/live/picker tables include `GROUP` when usage exists.
- [x] JSON windows include `group`.
- [x] Existing width tests still pass without overflow.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Real `list` shows `Gemini Models` when Gemini weekly is the lower value.
- `list --json` exposes `usage.weekly.group`.

## Rollback

Revert the column and `Window.Group`; no registry or credential changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Add one `GROUP` hint column | Keeps the one-row-per-account table while explaining the collapsed values |
| 2026-10-22 | Use the lower window | It is the value a user is most likely to question |
| 2026-10-22 | Leave full group detail to `usage` | Avoids duplicating the multi-row view in list |

## Completion

- Commit/tag: `feat(cli): label limiting quota group`
- Verification result: `make verify` and `make verify-full` passed; real `list` showed `GROUP Gemini Models` for the 58% weekly value and `list --json` exposed `usage.weekly.group`.
- Residual risk: the column is a hint for the lower window, not a full group breakdown; use `agy-auth usage` for all groups.
