# Query precedence, ambiguous switch picker, and `switch -`

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_error-taxonomy-v1.md` |

## Objective

Make query resolution deterministic, let ambiguous switch queries choose from
their candidates in a terminal, and add previous-account tracking with
`switch -`.

## Context

`Resolve` mixed exact aliases, emails, and ID prefixes into one bucket, so a
precise query could be widened by an unrelated substring match. Ambiguous
queries only returned an error, and there was no way to toggle back to the
account that was active before the last switch.

## Scope

### In scope

- Resolver precedence: row number, exact account ID, exact alias, exact email,
  account ID prefix, then email/alias/ID substring.
- `Registry.Previous` in `registry.json`, updated by switch and activating
  upserts (`add`, `login`, `import --activate`).
- Clear `previous` when that account is removed or no longer present on load.
- `switch -` resolves the previous account and returns `previous_not_found`
  when there is none.
- Ambiguous `switch`:
  - terminal: candidate picker;
  - non-interactive/`--json`: `ambiguous_query` with candidates.
- Tests for precedence, previous tracking/toggling, ambiguous picker, non-TTY
  candidates, and removal cleanup.
- README, internals, changelog, and plan updates.

### Out of scope

- Candidate pickers for `alias`, `remove`, or `export`; those still return
  `ambiguous_query`. Atomic multi-selector `remove` is the next slice.
- `switch -` support in other commands.
- Backups/clean and live usage views.
- Changing the candidate picker UI itself.

## Expected paths

- `internal/registry/registry.go`
- `internal/registry/registry_test.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/registry_test.go` test helpers in `internal/cli/cli_test.go`
- `README.md`
- `docs/internals.md`
- `CHANGELOG.md`

## Risks

- Query precedence changes which account wins when several forms overlap; the
  new order is stricter and covered by tests.
- `previous` is an additive registry field; old registries load unchanged.
- Stale `previous` values are cleared on load and removal.
- Terminal-only candidate selection adds one interactive path; non-TTY and
  JSON behavior stays deterministic.

## Acceptance criteria

- [x] Row numbers, exact IDs, aliases, emails, ID prefixes, and substrings resolve deterministically.
- [x] Ambiguous switch opens a candidate picker in a terminal.
- [x] Ambiguous switch returns `ambiguous_query` with candidates when non-interactive or JSON.
- [x] `switch -` toggles to the previously active account.
- [x] Missing/stale previous returns `previous_not_found`.
- [x] Removing the previous account clears it.
- [x] Old registries load without `previous`.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Add two accounts, `switch second --json`, then `switch - --json`.
- `switch example.com --json` with two matching emails returns two candidates.
- `switch example.com` in a terminal shows the numbered candidate picker.
- Remove the previous account, then `switch - --json` returns `previous_not_found`.
- Load a registry with a stale `previous`; it is cleared.

## Rollback

Revert this commit. `previous` is additive; no credential format or migration is
required.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Numeric lookup stays first | Row numbers are the shortest stable user reference |
| 2026-10-21 | Exact alias wins before ID prefix/substring | Prevents a precise alias from becoming ambiguous |
| 2026-10-21 | Exact email may return multiple accounts | One Google email can have multiple stored plans/accounts |
| 2026-10-21 | Track previous on activating upserts too | `add`/`login`/`import --activate` change the active account |
| 2026-10-21 | Candidate picker only for switch in this slice | Keeps the change reviewable; alias/remove follows with atomic remove |

## Completion

- Commit/tag: `feat(cli): add query precedence and switch -`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: candidate pickers for `alias`/`remove`/`export` and atomic multi-selector remove remain follow-up work.
