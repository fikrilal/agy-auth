# Shared row model, numbering, and width-aware table/picker

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_import-export-reporting.md` |

## Objective

Give list, pickers, numeric queries, and JSON list output one canonical row
order and one shared 1-based numbering, and render account tables that fit the
terminal instead of overflowing.

## Context

`list` used registry insertion order while pickers had their own numbering.
Tables used natural column widths, so long emails overflowed narrow terminals.
Same-email accounts were indistinguishable rows with no group context.

## Scope

### In scope

- `registry.SortAccounts` called during `Store.Load`: active first, email
  ascending, alias/ID tiebreaks.
- Shared `accountRow` model and display grouping:
  - labels use `alias(email)`, `email`, `alias`, or short ID;
  - same-email groups get a dim header and indented children;
  - only child rows receive numbers.
- Shared `01`-style numbers across `list`, pickers, numeric queries, and list
  JSON.
- Width-aware table engine:
  - required `#`, marker, and `ACCOUNT` columns;
  - optional `EXPIRES`, `METHOD`, and `LAST` columns added by priority;
  - account column truncation with an ellipsis;
  - terminal width from stdout or a test override.
- Picker uses the shared table for numbered and interactive selection; filtered
  candidate pickers keep global row numbers.
- `remove` interactive prompt and `alias list` use the shared table.
- Tests for ordering, numbering, truncation, grouping, and candidate numbers.
- README, internals, architecture, changelog, and plan updates.

### Out of scope

- Plan/credits/5H/weekly columns; the quota source is still a research gate.
- Live views, viewport/pagination, and 1 Hz refresh (Phase 2).
- Full TUI key handling beyond the existing arrows/j/k/g/G/q/enter set.
- Changing JSON list payloads beyond order and existing `number` fields.

## Expected paths

- `internal/registry/registry.go`
- `internal/registry/registry_test.go`
- `internal/cli/app.go`
- `internal/cli/table.go`
- `internal/cli/render.go`
- `internal/cli/picker.go`
- `internal/cli/cmd_list.go`
- `internal/cli/cmd_remove.go`
- `internal/cli/cmd_alias.go`
- `internal/cli/jsonout.go`
- `internal/cli/cli_test.go`
- `README.md`
- `docs/internals.md`
- `docs/engineering/architecture.md`
- `CHANGELOG.md`

## Risks

- Active-first ordering means row numbers change after a switch; this is
  intentional and makes the live account row `01`.
- Same-email grouping is display-only; JSON keeps a flat account list.
- Narrow terminals may drop `METHOD`/`LAST`; the account column is always kept.
- Candidate pickers show global numbers, so selected numbers can be
  non-contiguous.
- Loading sorts accounts in memory; the next save persists that order.

## Acceptance criteria

- [x] Accounts load active first, then email ascending, with stable tiebreaks.
- [x] List rows, pickers, numeric queries, and list JSON use the same numbers.
- [x] Same-email accounts render a group header and indented numbered children.
- [x] Table lines never exceed the configured terminal width in tests.
- [x] Narrow tables drop lower-priority columns and truncate accounts with an ellipsis.
- [x] Candidate pickers and `ambiguous_query` candidates carry global row numbers.
- [x] `remove` prompt and `alias list` use the shared table.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Two accounts, one active: list shows active as `01`, then email order.
- Switch to another account: it becomes `01` on the next list.
- 34-column row: `METHOD`/`LAST` drop and `ACCOUNT` ends with `…`.
- Two accounts with the same email: header plus indented `01`/`02` children.
- Ambiguous query candidates show their list numbers.

## Rollback

Revert this commit. The registry schema is unchanged; ordering is derived on
load.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Sort in `Store.Load` | Numeric resolution and display must share one order |
| 2026-10-21 | Active first, email ascending, alias/ID tiebreak | Matches the reference display ordering and keeps the live row at `01` |
| 2026-10-21 | Group headers are display-only and unnumbered | Only real account rows are selectable |
| 2026-10-21 | Account column is the flexible/truncated column | It carries the longest user-facing identity |
| 2026-10-21 | Optional columns selected by priority | Preserves the most useful data on narrow terminals |
| 2026-10-21 | Candidate pickers use global row numbers | A number means the same row in list and picker |

## Completion

- Commit/tag: `feat(cli): add shared row table and picker`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: plan/quota columns are deferred; active-first ordering changes row numbers after a switch; candidate numbers can be non-contiguous; no viewport/pagination yet.
