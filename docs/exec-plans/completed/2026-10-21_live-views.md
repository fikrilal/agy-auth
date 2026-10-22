# Live views for list, switch, and remove

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_shared-row-table-picker.md` |

## Objective

Add reliable live views for `list`, `switch`, and `remove` using the data we
already have: live credential, token expiry, and registry. Refreshes must not
block actions, and actions must stay open and show their result.

## Context

The non-live list/picker is complete, but users must rerun commands to see a
rotated live credential or registry changes. This phase adds a raw-mode view
with a viewport, a refresh cadence, and action feedback without depending on
usage/quota data.

## Scope

### In scope

- `internal/cli/live.go`:
  - raw mode, alternate screen, cursor hide/restore;
  - 1 Hz repaint, 60-second background credential/registry refresh;
  - generation counter so stale refreshes do not overwrite mutations;
  - viewport with selection kept visible, PgUp/PgDn, Home/End, arrows/j/k/g/G;
  - status line with source and next refresh/error;
  - action line for switch/remove.
- `list --live`: viewport plus status/help; no action line.
- `switch --live`: selection by account ID; Enter switches and stays open.
- `remove --live`: Space toggles checkboxes; Enter removes checked accounts;
  rows disappear on refresh.
- TTY preflight and `--json` rejection for all three.
- Silent child-command actions so live code reuses switch/remove behavior.
- Tests for TTY guards, viewport clamping, status line, frame rendering,
  navigation/checkboxes, and silent actions.
- README, internals, architecture, changelog, plan.

### Out of scope

- `config live --interval`; the interval is fixed at 60 seconds in this slice.
- Usage/quota columns and auto-switch (Phase 3 research gate).
- Incremental frame painting, mouse capture, type-ahead digits, and a complete
  TUI widget toolkit.
- Automated PTY tests; the controller model and frame renderer are unit-tested,
  with a manual PTY smoke test.
- Windows-specific raw-mode fallback beyond the existing numbered picker
  fallback for non-live commands.

## Expected paths

- `internal/cli/live.go`
- `internal/cli/table.go`
- `internal/cli/render.go`
- `internal/cli/app.go`
- `internal/cli/cmd_list.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/cmd_remove.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `README.md`
- `docs/internals.md`
- `docs/engineering/architecture.md`
- `CHANGELOG.md`

## Risks

- Raw mode must always restore on quit/error; defers handle the normal paths.
- Background reads can race with switch/remove writes; generation checks discard
  stale results, and file writes are atomic.
- Full-screen repaint at 1 Hz is simpler than incremental painting and may flash
  on slow terminals.
- The fixed 60-second interval is not configurable yet.
- Actions are executed through silent child commands, so their JSON output is
  discarded but their exit code and state changes are authoritative.

## Acceptance criteria

- [x] `list --live`, `switch --live`, and `remove --live` require a TTY and reject `--json`.
- [x] The live view repaints at 1 Hz and refreshes data every 60 seconds in the background.
- [x] A generation counter prevents stale refreshes from overwriting actions.
- [x] Viewport keeps the selected row visible and supports paging.
- [x] `switch --live` switches on Enter and stays open with an action line.
- [x] `remove --live` toggles checkboxes with Space and removes checked accounts on Enter.
- [x] Status line reports source and next refresh/error.
- [x] Terminal state is restored on quit.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Non-TTY `--live` exits with a usage error and no raw-mode output.
- `--live --json` emits a JSON usage error.
- 10 rows at height 10 show 4 rows and keep row 10 selected/visible.
- Remove frame shows `[x]`, action line, status line, and help.
- Silent switch changes the registry active ID; silent remove drops the selected ID.
- Manual PTY smoke: open `list --live`, press `q`, terminal restores.

## Rollback

Revert this commit. Non-live list/picker behavior is unchanged; live state is
derived from existing credentials and registry.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Fixed 60-second refresh; config command deferred | Keeps the live slice focused; interval can be made configurable later |
| 2026-10-21 | Full-screen repaint at 1 Hz | Simpler and adequate for a small account list |
| 2026-10-21 | Silent child commands for actions | Reuses switch/remove behavior without duplicating logic |
| 2026-10-21 | Generation counter for refresh results | Mutations must win over in-flight reads |
| 2026-10-21 | Selection stored by account ID | Rows reorder when active changes or data refreshes |
| 2026-10-21 | Table widths computed from all rows | Columns must not jump while scrolling |

## Completion

- Commit/tag: `feat(cli): add live views`
- Verification result: `make verify` and `make verify-full` passed; manual PTY smoke test opened and quit the live view cleanly.
- Residual risk: refresh interval is fixed at 60s; full repaint at 1 Hz; usage/quota columns and auto-switch remain Phase 3; no automated PTY tests.
