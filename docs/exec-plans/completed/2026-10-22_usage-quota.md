# Usage and quota data

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | `_WIP/codex-auth-ux-analysis.md` Phase 3; [Architecture](../../engineering/architecture.md); [Internals](../../internals.md) |

## Objective

Show the account plan and the shared 5-hour/weekly quota in `list` and the live views, with a 60-second cache, and let `switch --live` move away from an exhausted active account when a healthy alternative exists.

## Context

Phase 3 was gated on whether Antigravity exposes quota data that can be reconstructed from a stored credential. The gate passed with the live account:

- `POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist` with the cached project ID returns `currentTier`, `paidTier`, and `cloudaicompanionProject`.
- `POST .../v1internal:retrieveUserQuotaSummary` with `{ "project": "<project>" }` returns the shared `5h` and `weekly` buckets, remaining fractions, and reset times.
- `POST .../v1internal:retrieveUserQuota` returns per-model buckets; the summary RPC is the better source for account-level columns.
- Requests without an `antigravity*` `User-Agent` return an empty `loadCodeAssist` response and `403 PERMISSION_DENIED` from the quota summary. Requests without the project ID behave the same way.
- The project ID is in `~/.gemini/antigravity-cli/cache/default_project_id.txt`; `ANTIGRAVITY_PROJECT_ID` and `GEMINI_DEFAULT_PROJECT` are fallbacks.
- Expired access tokens can be refreshed with the existing discovered Antigravity OAuth client. The refresh token stays in the credential document; no new storage is needed.

## Scope

### In scope

- `internal/usage` with a provider, typed snapshot/window values, stable short error codes, and a concurrency-safe TTL cache.
- Token refresh through `internal/oauth` when an account's access token is expired; never print or persist tokens in the usage path.
- `loadCodeAssist` plan extraction and `retrieveUserQuotaSummary` 5H/weekly extraction.
- Add `PLAN`, `5H`, and `WEEKLY` columns to non-live `list` and the live views when data exists. Keep the old columns unchanged when usage is unavailable.
- `list --no-usage` to skip network quota lookups.
- Add `plan`, nested `usage` (`five_hour`, `weekly`, `updated_at`, `stale`), and `usage_error` to list JSON rows.
- `switch --live` auto-switch only when the active account is at 0% on a non-stale window and a healthy stored candidate exists. Choose the candidate with the earliest reset. Suppress auto-switch for stale data and after one automatic or manual switch in the session.
- Mocked HTTP tests for request shape, refresh, partial failure, cache TTL/stale fallback, column rendering, JSON, and auto-switch selection.
- Real-account smoke test with `--no-sync` against the local agy credential.

### Out of scope

- `CREDITS` / reset-credit columns; the summary response used here does not expose them.
- Per-model quota columns; the account-level summary is the contract.
- Persisting quota to `registry.json` or a disk cache; the cache is in-memory and process-scoped.
- Auto-switch outside `switch --live`.
- `config live --interval`.

## Expected paths

Informational only; this is not enforced by a task controller.

- `internal/usage/`
- `internal/oauth/oauth.go`
- `internal/cli/{app,render,table,live,picker,cmd_list,spec}.go`
- `internal/cli/cli_test.go`
- `internal/usage/usage_test.go`
- `internal/oauth/oauth_test.go`
- `internal/harness/lint/lint.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/architecture.md`, `docs/engineering/testing.md`

## Risks

- A network lookup on every `list` can be slow or fail offline. Mitigate with a short timeout, bounded concurrency, `--no-usage`, and non-fatal per-row errors.
- Refreshing an account's access token is a credential operation. Keep it in memory, call the existing OAuth client discovery, and never log tokens.
- The quota endpoint is undocumented and can change. Isolate it behind `usage.Provider`, keep errors non-fatal, and display cached values as stale rather than erasing them.
- Auto-switch can surprise users. Restrict it to `switch --live`, require fresh data on both sides, and perform at most one switch per session.
- The backend gates on an `antigravity*` User-Agent. Do not use a generic `agy-auth` UA; tests assert the prefix.

## Acceptance criteria

- [x] A stored account with a valid or refreshable token yields plan and 5H/weekly values.
- [x] Expired access tokens are refreshed without writing a new snapshot or exposing token values.
- [x] A 403/401/timeout leaves the row usable, records a short `usage_error`, and keeps cached values marked stale.
- [x] `list` renders `PLAN`, `5H`, and `WEEKLY` only when at least one row has data; narrow terminals still drop lower-priority columns and never exceed the width.
- [x] `list --json` keeps the existing envelope and adds `plan`, `usage`, and `usage_error`.
- [x] `list --no-usage` performs no quota HTTP requests.
- [x] `switch --live` auto-switches only on fresh exhausted data, chooses the earliest reset among healthy candidates, and does not loop.
- [x] Unit tests use `httptest` and fake providers only; no real account in CI.
- [x] `make verify` and `make verify-full` pass.
- [x] Real-account `list --active --no-sync` shows the live plan and quota without changing the registry.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `go test ./internal/usage ./internal/oauth ./internal/cli`
- Real account: build and run `./bin/agy-auth list --active --no-sync` with the local agy credential; verify `PLAN`, `5H`, and `WEEKLY` without printing tokens.
- Force a fake 403 and a fake timeout in tests; confirm the row still renders and JSON has `usage_error`.
- `AGY_AUTH_HOME` remains untouched by the live smoke test (`--no-sync`).

## Rollback

Revert the slice. The new columns and JSON fields are additive; no registry migration is introduced. Quota cache is in-memory, so a rollback leaves no on-disk state. Refreshed access tokens are not written to snapshots, so no credential data is changed by this feature.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Use `retrieveUserQuotaSummary`, not `retrieveUserQuota` | The summary directly exposes the shared 5h/weekly limits; per-model buckets do not map cleanly to account columns |
| 2026-10-22 | Require an `antigravity*` User-Agent | The backend returns empty/403 for generic clients; tests assert the prefix |
| 2026-10-22 | Keep quota cache in memory | Avoids a registry schema change and disk-secret risk; live refresh is already process-scoped |
| 2026-10-22 | Do not auto-switch on stale data | A cached 0% is not proof the account is exhausted now |
| 2026-10-22 | Refresh tokens in memory only | Usage is a read path; persisting refreshed tokens would add a credential mutation and backup requirement |

## Completion

- Commit/tag: `feat(cli): add usage quota and auto-switch`
- Verification result: `make verify` and `make verify-full` passed. Real-account `list --active --no-sync` showed `PLAN Pro`, `5H 100%`, and `WEEKLY 100%`; a PTY `list --live` smoke test rendered the quota columns for three accounts and quit cleanly.
- Residual risk: the summary covers the shared Claude/GPT limits, not Gemini-only model buckets; the quota cache is process-local; auto-switch runs at most once per session and only on fresh data; the endpoint is undocumented and can change.
