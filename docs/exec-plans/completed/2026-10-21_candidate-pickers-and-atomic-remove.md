# Shared candidate pickers and atomic remove resolution

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_query-selection-and-previous.md` |

## Objective

Use one single-select resolution path with a candidate picker for `switch`,
`alias`, `remove`, and `export`, and make `remove` resolve and deduplicate all
selectors before mutating anything.

## Context

`switch` had its own ambiguous-query picker, while `alias`, `remove`, and
`export` still failed on ambiguity. `remove` resolved selectors in a loop and
started deleting before it knew every selector was valid, so a later failure
could leave partial work.

## Scope

### In scope

- `resolveOne` shared by `switch`, `alias set/clear`, `remove`, and `export`:
  - terminal: candidate picker over the matches;
  - non-interactive/`--json`: `ambiguous_query` with candidates.
- `remove` resolves all selectors and deduplicates them before confirmation or
  deletion.
- `remove` deletes all snapshot files before deleting the live credential and
  before saving the registry.
- A snapshot deletion failure returns `state_uncertain` without committing
  registry changes.
- Alias messages distinguish set, already set, updated, and cleared.
- Tests for picker paths, atomic resolution, duplicate/ambiguous selectors, and
  alias messages.
- README, internals, changelog, and plan updates.

### Out of scope

- Backups and `clean`.
- A true filesystem transaction for snapshot deletion; failures are reported
  as `state_uncertain` with reconciliation guidance.
- Import/export reporting expansion.
- Live usage views.

## Expected paths

- `internal/cli/cmd_alias.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/cmd_remove.go`
- `internal/cli/cmd_import.go`
- `internal/cli/cli_test.go`
- `README.md`
- `docs/internals.md`
- `CHANGELOG.md`

## Risks

- More commands can now prompt in a terminal; non-TTY and JSON behavior remains
  deterministic.
- Deletion is not transactional. Preflight/resolve happens first, but a delete
  failure after some files are removed returns `state_uncertain`.
- Alias output text changes; JSON output is unchanged.

## Acceptance criteria

- [x] `switch`, `alias`, `remove`, and `export` share one resolver.
- [x] Ambiguous queries open a candidate picker in a terminal.
- [x] Ambiguous queries return `ambiguous_query` with candidates when non-interactive or JSON.
- [x] `remove` resolves and deduplicates every selector before deleting.
- [x] A failed later selector leaves all snapshots and registry entries intact.
- [x] Snapshot deletion failures return `state_uncertain` without saving registry changes.
- [x] Alias messages distinguish set/already/updated/cleared.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Two matching accounts + `alias set example.com name` in a TTY: picker appears.
- Two matching accounts + `remove example.com --yes` in a TTY: picker appears.
- `remove valid missing --yes --json`: `account_not_found`, both accounts intact.
- Duplicate selectors remove one account.
- Alias set twice says "already"; changing says "Updated alias".

## Rollback

Revert this commit. No credential or registry format changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | One `resolveOne` helper for all single-select commands | Consistent ambiguity behavior and less duplicated logic |
| 2026-10-21 | Resolve and dedupe before confirmation/deletion | Prevents a later invalid selector from leaving partial work |
| 2026-10-21 | Delete snapshots before live credential and registry save | Keeps registry mutation last and makes failures explicit |
| 2026-10-21 | Deletion failures are `state_uncertain` | Filesystem deletion is not transactional; callers must reconcile |
| 2026-10-21 | Alias messages distinguish set/already/updated/cleared | Idempotent commands should report what actually changed |

## Completion

- Commit/tag: `feat(cli): add candidate pickers and atomic remove`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: snapshot deletion is not transactional; backups/clean and import/export reporting remain follow-up work.
