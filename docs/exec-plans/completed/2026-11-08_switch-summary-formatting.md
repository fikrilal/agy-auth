# Informative and Polished Switch Summary Card

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-11-08 |
| Related | `internal/cli/cmd_switch.go`, `internal/cli/cmd_rotate.go`, `internal/cli/cli_test.go` |

## Objective

Elevate the after-switch and after-rotate terminal experience from a plain, dry wall of text to a structured, informative summary card with green success checkmark, clear metadata hierarchy, non-intimidating token notice, and a styled restart tip.

## Context

Currently, running `agy-auth switch` outputs plain unstyled text:
```
Switched to jirenio666@gmail.com.
Wrote OS keyring and file mirror.
warning: the access token is expired; agy will refresh it on next start
Restart agy for the new account to take effect (running sessions keep the old credential).
```

This presents several UX issues:
1. No visual success badge (`✔`) or styled account name.
2. The storage write report is dry technical jargon.
3. The expired access token warning sounds like an error/bug, even though Google OAuth tokens naturally expire hourly and agy refreshes them silently on next start.
4. The restart instruction is an unformatted block of text.

## Scope

### In scope

- Create a shared `printSwitchSummary(title string, res *switchResult)` on `*App` used by both `cmdSwitch` and `cmdRotate`.
- Format output with:
  - Green checkmark + bold title (`✔ Switched to <account>.`)
  - Sub-bullets for storage targets (`• Wrote OS keyring and file mirror.`, `• Restored Desktop profile.`, etc.)
  - Styled notice for expired tokens (`! Notice: access token expired; agy will auto-refresh on next run.`)
  - Styled dim tip footer (`Tip: Restart running agy sessions to pick up the new credential.`)
- Add unit tests verifying formatted switch summary output and `NoColor` fallback.

### Out of scope

- Changing JSON output format or envelopes.
- Changing token storage or switching logic.

## Acceptance criteria

1. Non-JSON `switch` and `rotate` output formatted with green checkmark, bulleted target details, friendly token notice, and dim tip footer.
2. In `NoColor` / non-TTY mode, escape sequences are stripped cleanly.
3. Existing test assertions (`Switched to`, `Wrote`, `Restored Antigravity Desktop profile`) continue to pass.
4. `make verify` and `make verify-full` pass.
