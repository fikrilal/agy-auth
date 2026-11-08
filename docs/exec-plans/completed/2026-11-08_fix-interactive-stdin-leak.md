# Fix Interactive Key Reader Stdin Leak

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-08 |
| Related | `internal/cli/keys.go`, `internal/cli/poll_unix.go`, `internal/cli/poll_windows.go`, `internal/cli/picker.go`, `internal/cli/live.go`, `internal/cli/keys_test.go`, `internal/cli/cli_test.go` |

## Objective

Prevent interactive TUI operations (like `agy-auth switch` account picker and live view) from leaking background stdin reader goroutines that intercept subsequent terminal prompts (such as `Update now? [y/N]`).

## Context

When running `agy-auth switch` without arguments, an interactive terminal picker is shown (`chooseAccountInteractiveMode`). This launches `go readKeyEvents(a.StdinFile, keys)`.
Inside `readKeyEvents`, a background goroutine calls `file.Read(buf)` in an infinite loop on `a.StdinFile` (`os.Stdin`).
When the user chooses an account by pressing Enter, `chooseAccountInteractiveMode` returns and `cmdSwitch` finishes. However, the background goroutine reading `os.Stdin` continues running in the background.

When a deferred update check (`maybeNotifyUpdate`) runs and prompts `Update now? [y/N] `, `a.readLine` attempts to read from `os.Stdin`.
When the user types `y` and presses Enter, the leaked background goroutine intercepts the input from `os.Stdin`. `readLine` receives nothing and hangs forever.

## Scope

### In scope

- Add clean cancellation lifecycle to `readKeyEvents` via `stop <-chan struct{}`.
- Implement platform-aware `waitForReadable(file *os.File, timeout time.Duration)`:
  - Unix (`poll_unix.go`): uses `unix.Poll` on the file descriptor with a short timeout.
  - Windows (`poll_windows.go`): uses `windows.WaitForSingleObject` on the console/file handle.
- Update `readKeyEvents` to only read from `file` when `waitForReadable` reports data, periodically checking `stop`.
- Update `picker.go` and `live.go` to close `stop` and wait for the reader to terminate before restoring terminal state.
- Add regression tests verifying that stdin readers exit promptly on stop and that subsequent stdin reads succeed.

### Out of scope

- Changing update check logic or network URLs.
- Changing CLI flags or JSON envelopes.

## Acceptance criteria

1. Exiting the interactive picker (`chooseAccountInteractiveMode`) terminates all reader goroutines within 50ms.
2. Subsequent calls to `a.readLine` (such as `Update now? [y/N]`) read `os.Stdin` without interception.
3. Live view (`runLive`) also cleanly cancels its key reader goroutines upon exit.
4. Cross-platform builds (Linux, Darwin, Windows) compile cleanly.
5. All tests in `internal/cli` and `make verify-full` pass.
