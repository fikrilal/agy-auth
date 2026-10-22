# TUI input and resize polish

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Live views](2026-10-21_live-views.md); [Internals](../../internals.md) |

## Objective

Make the interactive picker and live views robust across real terminals: enhanced key sequences, digit selection, resize handling, incremental frame painting, and an automated PTY regression test.

## Context

The original input loop read 16-byte chunks and treated a lone Esc as quit. Split escape sequences could be misread, modified arrows/CSI-u were unsupported, digits did nothing, and every frame cleared the whole screen. Live views only noticed terminal changes on the next 1 Hz repaint.

## Scope

### In scope

- Shared key decoder for plain/control keys, arrows, SS3, modifiers, CSI-u, PgUp/PgDn, Home/End, and a 50 ms lone-Esc timeout.
- Unknown CSI/mouse sequences ignored without quitting.
- `0-9` row type-ahead with an 800 ms window in live views and the interactive picker.
- 250 ms resize polling for live views; full repaint on dimension change.
- Incremental line painting with absolute cursor writes and stale-tail clearing.
- Automated PTY test for live open/navigate/quit and terminal restore.
- Unit tests for decoding, split sequences, type-ahead, and incremental paint.
- Docs/changelog.

### Out of scope

- Mouse selection.
- A full TUI widget toolkit.
- Changing table content or quota semantics.
- Automated visual assertions beyond PTY open/navigate/quit.

## Expected paths

- `internal/cli/{keys,keys_test,pty_test,live,picker,cli_test}.go`
- `go.mod`, `go.sum`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/testing.md`

## Risks

- Escape parsing is terminal-dependent; unknown sequences must degrade to ignored, not quit.
- Type-ahead can conflict with row-number prefixes; exact matches win, then the first prefix.
- Incremental paint must clear stale tails when rows or widths shrink.
- PTY tests are Unix-only and should skip on unsupported platforms.

## Acceptance criteria

- [x] Arrow/SS3/modified/CSI-u keys move correctly.
- [x] Lone Esc quits after the timeout; split sequences are reassembled.
- [x] Unknown CSI/mouse sequences do not quit.
- [x] `0-9` jump selects the matching global row number; timeout resets the buffer.
- [x] Live resize triggers a full repaint at the new dimensions.
- [x] Unchanged lines are not repainted; changed lines and stale tails are handled.
- [x] PTY test opens the live view, navigates, quits, and sees terminal restore.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `go test ./internal/cli -run TestLivePTY -v`
- Manual live view: type a row number, resize the terminal, and quit with `q`.

## Rollback

Revert the shared decoder, resize ticker, incremental paint, and PTY test. The previous basic key handling returns; no credential or registry changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | 50 ms lone-Esc timeout | Distinguishes Esc from split arrow sequences without noticeable lag |
| 2026-10-22 | Exact row number then first prefix | Makes “1” select row 1 and “12” extend to row 12 |
| 2026-10-22 | 250 ms resize poll | Cross-platform and avoids platform-specific SIGWINCH plumbing |
| 2026-10-22 | Incremental absolute-line paint | Removes full-screen flicker while keeping width reflow simple |

## Completion

- Commit/tag: `feat(cli): polish live tui input and resize`
- Verification result: `make verify` and `make verify-full` passed; `TestLivePTY` opens, sends `j2q`, and sees terminal restore; a manual PTY run with a resize and digit jump exited cleanly.
- Residual risk: PTY coverage is Unix-only; mouse selection and a full TUI widget toolkit remain out of scope.
