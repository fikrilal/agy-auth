# Add `agy-auth rotate` command

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-25 |
| Related | [`docs/internals.md`](../../internals.md), [`docs/user-guide.md`](../../user-guide.md) |

## Objective

Provide a standalone `agy-auth rotate` command that inspects quota across stored accounts, verifies whether the active account is exhausted (or bypassed with `--force`), selects the healthiest account with remaining 5-hour quota, and switches both CLI and Desktop credentials in one shot (with `--relaunch` support).

## Context

Users working on heavy development tasks encounter quota limits on their active Google Antigravity account. While `switch --live` provides an interactive TTY auto-switch, users need a non-interactive, scriptable command (`agy-auth rotate`) that can be invoked directly from the terminal, CI scripts, IDE tasks, or error hooks to rotate to an available account automatically.

## Scope

### In scope

- Command implementation:
  - Implement `cmdRotate` in `internal/cli/cmd_rotate.go`.
  - Extract/share unified `switchAccount` execution logic between `cmd_switch.go` and `cmd_rotate.go`.
  - Candidate selection algorithm evaluating fresh quota, availability, and optimal reset time.
  - Flags: `--target=all|cli|desktop`, `--relaunch`, `--close-desktop`, `--force`, `--json`.
  - Exit behavior:
    - Active healthy & no `--force`: exit 0, report active is healthy.
    - Active exhausted (or `--force`): find healthiest candidate with quota and switch.
    - No candidate with quota: exit 1 with error code `no_candidate_available`.
- Command registration:
  - Register `rotate` in `internal/cli/app.go`.
  - Add `CommandSpec` for `rotate` in `internal/cli/spec.go`.
- Documentation & tests:
  - Comprehensive unit tests covering healthy active, exhausted active, forced switch, no candidate, and desktop relaunch in `internal/cli/cli_test.go`.
  - Update `README.md`, `docs/user-guide.md`, and `docs/internals.md`.
  - Full verification with `make verify-full` and installation via `make install`.

### Out of scope

- Continuous background daemon process monitoring quota outside CLI execution.
- Auto-creating or logging in new Google accounts without user interaction.

## Expected paths

- `internal/cli/cmd_rotate.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/app.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `docs/user-guide.md`
- `docs/internals.md`
- `README.md`

## Risks

- **Unnecessary switching:** Rotating when the current account still has healthy quota would disrupt active workflows.
  *Mitigation:* Check active account quota first; exit 0 unless active is exhausted or `--force` is specified.
- **Quota network failures / timeouts:** Stored accounts might fail quota lookup if offline or rate-limited.
  *Mitigation:* Treat accounts with network/quota errors conservatively and skip them from being selected as candidates.
- **Desktop process collisions:** Running Antigravity Desktop while switching.
  *Mitigation:* Reuse `switchAccount` to honor `--target`, `--relaunch`, and safe process checks.

## Acceptance criteria

- [x] `agy-auth rotate` queries quota across stored accounts.
- [x] If active account is healthy and `--force` is not set, prints healthy notice and exits 0.
- [x] If active account is exhausted (or `--force` is set), finds the healthiest candidate with remaining quota and executes the switch.
- [x] Supports `--target=all|cli|desktop`, `--relaunch`, and `--close-desktop`.
- [x] Supports `--json` with structured response envelope.
- [x] Emits clear error (`no_candidate_available`) if no stored accounts have quota.
- [x] All unit tests pass, `make verify-full` succeeds with 0 errors.

## Verification plan

```shell
make verify-full
```

Additional checks:
- Verify CLI output of `agy-auth rotate --help`.
- Test unit test scenarios in `internal/cli/cli_test.go`.

## Rollback

Revert changes to `internal/cli/` and documentation.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-25 | Add standalone `rotate` command with `--force` flag | Allows both automatic exhaustion handling and explicit forced rotation. |
| 2026-10-25 | Share `switchAccount` between `cmdSwitch` and `cmdRotate` | Prevents credential switching and desktop handling duplication. |

## Completion

- Commit/tag: uncommitted candidate
- Verification result: `make verify-full` passed with 0 findings
- Residual risk: none
