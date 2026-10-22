# Per-model quota detail

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Usage quota plan](../completed/2026-10-22_usage-quota.md); [Internals](../../internals.md) |

## Objective

Add a `usage` command that shows per-model quota buckets, including Gemini-only buckets that the shared 5H/weekly summary does not expose.

## Context

`list` now shows the shared Claude/GPT 5-hour and weekly windows from `retrieveUserQuotaSummary`. That summary does not cover every model family. `retrieveUserQuota` returns per-model buckets with `remainingFraction` and `resetTime`; this is the missing detail view.

## Scope

### In scope

- `usage.Client.FetchDetail` using `loadCodeAssist` + `retrieveUserQuota`.
- A `usage` command for the live account, the active account, or one query-selected stored account.
- Human table with `MODEL`, `REMAINING`, and `RESETS`.
- JSON payload with account/email, plan, `updated_at`, and model buckets.
- Stable `quota_error` for fetch failures; `credential_not_found` when no credential exists.
- Tests for request shape, parsing/sorting, command output, JSON, query selection, and error handling.
- Real-account smoke test with the live credential.

### Out of scope

- Adding per-model data to `list`; the account-level summary stays the fast path.
- Credits/reset-credit values; the endpoint does not provide them.
- A disk cache for the detail view; it is a one-shot explicit command.
- Multiple accounts in one invocation.

## Expected paths

- `internal/usage/usage.go`
- `internal/usage/usage_test.go`
- `internal/cli/{app,cmd_usage,spec}.go`
- `internal/cli/cli_test.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/testing.md`

## Risks

- The endpoint is undocumented; errors must be non-destructive and clearly labeled.
- Model IDs are provider-specific but are the most useful stable labels available.
- A live account may not match a stored snapshot; the command must still be able to show its usage.

## Acceptance criteria

- [x] `agy-auth usage` resolves the live account, falling back to the active stored account.
- [x] `agy-auth usage <query>` selects one stored account.
- [x] The human table shows every returned model bucket with remaining percent and reset time.
- [x] `--json` returns the existing envelope plus account, plan, updated time, and buckets.
- [x] Missing credentials and API failures return stable error codes and useful hints.
- [x] `make verify` and `make verify-full` pass.
- [x] Real-account `usage` and `usage --json` work without printing tokens.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `./bin/agy-auth usage` against the live account.
- `./bin/agy-auth usage --json` and inspect the model bucket fields.
- Query a stored account by alias or ID in the test environment.

## Rollback

Revert the command and provider method. No registry or credential changes occur; the command is read-only apart from in-memory token refresh.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Use `retrieveUserQuota` for detail | It has the per-model buckets the summary lacks |
| 2026-10-22 | Keep detail out of `list` | Avoids a third request per account on every list refresh |
| 2026-10-22 | Sort models by ID | Gives stable output across requests |
| 2026-10-22 | Do not cache detail | It is an explicit one-shot command; freshness matters more than repeat speed |

## Completion

- Commit/tag: `feat(cli): add live interval and quota detail`
- Verification result: `make verify` and `make verify-full` passed; real-account `usage` showed 19 model buckets, including Gemini 2.5 models at 95% with reset times.
- Residual risk: model IDs are provider-specific; the endpoint is undocumented; one-shot detail lookups are not cached.
