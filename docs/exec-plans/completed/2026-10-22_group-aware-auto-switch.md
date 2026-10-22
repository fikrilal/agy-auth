# Group-aware live auto-switch

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Canonical usage groups](2026-10-22_canonical-usage-groups.md); [Internals](../../internals.md) |

## Objective

Make `switch --live` use the same named quota groups as `agy /usage` when deciding whether the active account is exhausted and which candidate has headroom.

## Context

The previous auto-switch used the shared 5H/weekly summary and any 0% per-model bucket. That could switch when an individual model bucket was empty while the named group was healthy, and it did not explicitly account for tier-specific groups such as `Gemini 4 Argon`.

## Scope

### In scope

- Include the canonical `Groups` in `Full`/live enrichment.
- Prefer fresh named groups for exhaustion, availability, and next-reset selection.
- Keep summary/model fallback only when group data is absent or stale.
- Mark cached groups stale with the rest of the full cache result.
- Add tests proving groups override a 0% detail bucket, exhausted groups trigger switching, and exhausted candidates are skipped.
- Docs/changelog and plan.

### Out of scope

- Changing `usage --all` rendering.
- Removing the full per-model fetch (kept as fallback).
- Different policies per group or per tier.

## Expected paths

- `internal/usage/{usage,groups}.go`
- `internal/usage/usage_test.go`
- `internal/cli/{live,usage,render,cli_test}.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`

## Risks

- Group data can be missing or stale; the summary/detail fallback must remain intact.
- A group with only one window is still valid; missing windows are simply absent.
- The full fetch still makes the per-model request for fallback compatibility.

## Acceptance criteria

- [x] Fresh named groups override a contradictory 0% model bucket.
- [x] Any fresh group bucket at 0% marks the active account exhausted.
- [x] A candidate with any fresh group bucket at 0% is skipped.
- [x] Stale groups fall back safely and never drive a switch.
- [x] Earliest group reset is used for candidate ordering.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Unit tests for group preference, group exhaustion, candidate skip, and stale fallback.
- Real live view smoke test with the current three accounts (no exhaustion expected).

## Rollback

Revert the group preference in the live quota helpers. Summary/model fallback remains available; no credential or registry migration is involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Groups first, summary/detail fallback | Matches the canonical `/usage` semantics without losing resilience |
| 2026-10-22 | Keep per-model fetch | Provides fallback when group data is unavailable |
| 2026-10-22 | Use any group bucket at 0% | Matches the UI’s group-level exhaustion signal |

## Completion

- Commit/tag: `fix(live): prefer named quota groups`
- Verification result: `make verify` and `make verify-full` passed; unit tests prove groups override a contradictory 0% detail bucket, exhausted groups trigger switching, and exhausted candidates are skipped; a real PTY live view opened and quit cleanly.
- Residual risk: the full fetch still makes the per-model request as a fallback; group-only data is preferred when fresh.
