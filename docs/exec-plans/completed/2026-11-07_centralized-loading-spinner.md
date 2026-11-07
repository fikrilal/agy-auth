# Centralized loading spinner for quota fetches

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-07 |
| Related | `internal/cli/usage.go`, `internal/cli/cmd_usage.go`, `internal/cli/spinner.go`, `internal/cli/cli_test.go` |

## Objective

Provide immediate visual feedback via a lightweight, clean terminal spinner whenever CLI commands fetch remote quota data (`list`, `list --all`, `usage`, `usage --all`, `switch` picker, and `rotate`), automatically erasing itself before table or picker rendering, and completely disabled during `--json`, live views, and non-TTY execution.

## Context

Fetching quota data involves remote HTTP calls to Google Code Assist (`loadCodeAssist` and `retrieveUserQuotaSummary`) and potential in-memory token refreshes. With multiple accounts, this network I/O takes 2–4 seconds during which the terminal currently appears frozen on a blank line. Centralizing a spinner within the shared fetch functions (`enrichRows` and `fetchUsageGroups`/`cmdUsageAll`) provides immediate feedback across all commands.

## Scope

### In scope

- Create `internal/cli/spinner.go` with a minimal, zero-dependency spinner (`⠋`, `⠙`, `⠹`, `⠸`, `⠼`, `⠴`, `⠦`, `⠧`, `⠇`, `⠏`) that updates on a goroutine ticker (~80ms).
- Provide `startSpinner(msg string) func()` on `*App` returning a cleanup/stop function that erases the spinner line (`\r\033[K`) and restores cursor visibility.
- Suppress spinner completely when `!a.OutTTY`, `a.JSON`, or `a.NoColor`.
- Wire spinner into `enrichRows` in `internal/cli/usage.go`.
- Wire spinner into `cmdUsage` and `cmdUsageAll` in `internal/cli/cmd_usage.go`.
- Handle edge cases: fast cached returns, terminal resize, and error exits without leaving dangling spinner lines.
- Add tests in `internal/cli/cli_test.go` verifying spinner start/stop, line erasure, and suppression on non-TTY / `--json`.

### Out of scope

- `live.go` (live views already have a dedicated 1 Hz TUI frame loop and status line).
- Changing backend network timeouts or concurrency.

## Expected paths

- `internal/cli/spinner.go`
- `internal/cli/usage.go`
- `internal/cli/cmd_usage.go`
- `internal/cli/cli_test.go`

## Risks

- **Lingering spinner text on abnormal exit:** Guarded by `defer stop()` in all callers.
- **Garbage characters on dumb terminals:** Guarded by `a.OutTTY` and checking `TERM != "dumb"`.
- **Race conditions or writing to `a.Out` after table render:** Guarded by `sync.Once` and channel synchronization in spinner stop function.
- **Harness AST Rules:** All terminal writes must go to `a.Out` or `a.Err`, respecting `output-boundary`.

## Acceptance criteria

- [x] Running `list`, `list --all`, `usage`, `usage --all`, `switch`, or `rotate` on interactive TTY shows an animated spinner while fetching.
- [x] Spinner is cleanly cleared from the line (`\r\033[K`) before output is printed.
- [x] Running with `--json` or piping output (`| cat`) never emits spinner characters.
- [x] All tests pass with race detector enabled (`go test -race ./internal/cli`).
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
go test -v -race ./internal/cli -run "TestSpinner"
make verify
make verify-full
```

Manual checks:
- Verify `bin/agy-auth list` on terminal displays spinner while loading and clears cleanly.
- Verify `bin/agy-auth list --all` on terminal displays spinner while loading and clears cleanly.
- Verify `bin/agy-auth list | cat` does not output any spinner characters.
- Verify `bin/agy-auth list --json` output remains strictly valid JSON.

## Rollback

Revert the commits introducing `spinner.go` and its integrations.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-11-07 | Centralize spinner in `enrichRows` and `cmdUsageAll` | Avoids duplicate spinner code across `list`, `switch`, `rotate`, and `usage`. |
| 2026-11-07 | Use `\r\033[K` line erasure on stop | Ensures the spinner leaves zero residual text on the screen when the table renders. |

## Completion

- Commit/tag: Pending commit authorization
- Verification result: `make verify` and `make verify-full` pass; `TestSpinner` with `-race` passes cleanly.
- Residual risk: None. Completely disabled in non-interactive/JSON environments.
