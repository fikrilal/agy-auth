# Canonical Antigravity usage groups

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Usage all](2026-10-22_usage-all.md); [Internals](../../internals.md) |

## Objective

Replace the invented `LOWEST`/`MODEL` aggregate with the same named quota groups that `agy /usage` shows: Gemini Models, Claude and GPT models, and any tier-specific group such as Ultra.

## Context

The previous `usage --all` view mixed the shared 5H/weekly summary with a minimum across model buckets. That was misleading and did not match Antigravity. The canonical CLI output is available via `agy -p /usage --output-format json` and via `retrieveUserQuotaSummary` when the request uses the `antigravity/cli/<version>` User-Agent. With a generic `antigravity/agy-auth/...` User-Agent the backend returns only the Claude/GPT group.

## Scope

### In scope

- `usage.Groups`, `Group`, and `GroupBucket` types from the canonical summary.
- `Client.FetchGroups` using `loadCodeAssist` + `retrieveUserQuotaSummary`.
- The `antigravity/cli/1.4.5 (agy-auth/...; ...)` User-Agent family.
- `usage` and `usage --all` group tables: `GROUP`, `5H`, `5H RESET`, `WEEKLY`, `WEEKLY RESET`.
- JSON `groups` payloads; remove `LOWEST`/`MODEL` from the usage views.
- Dynamic rendering of tier-specific groups; no hardcoded Gemini/Claude logic beyond window selection.
- Tests for grouped summary parsing, UA, CLI output, JSON, and per-account errors.
- Update docs and changelog.

### Out of scope

- Replacing the live auto-switch's per-model detail path.
- Adding model selection/filtering to the usage table.
- Persisting group data to disk.

## Expected paths

- `internal/usage/{usage,groups}.go`
- `internal/usage/{usage,groups}_test.go`
- `internal/cli/{app,cmd_usage,spec,table,cli_test}.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/testing.md`

## Risks

- The `antigravity/cli` User-Agent gate is undocumented; tests assert the prefix and the `agy-auth` marker.
- Group names and tier-specific groups are backend-driven; render whatever is returned.
- Removing `LOWEST`/`MODEL` changes `usage --json`; this is intentional to match `agy /usage`.

## Acceptance criteria

- [x] `usage` matches `agy /usage` group rows for the live account.
- [x] `usage --all` renders one combined table with account rows and group rows.
- [x] Gemini and Claude/GPT groups are both present; Ultra/tier-specific groups render automatically.
- [x] No `LOWEST`/`MODEL` columns remain.
- [x] JSON returns `groups` with bucket window, percent, and reset time.
- [x] The canonical API path is fast and does not spawn `agy`.
- [x] `make verify` and `make verify-full` pass.
- [x] Real three-account comparison works.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Real `usage` and `usage --all` against three accounts.
- Compare the group names/percentages with `agy -p /usage --output-format json`.

## Rollback

Revert the group parser/CLI changes. The old JSON shape and display return; no registry or credential changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Use `antigravity/cli/<version>` UA | It is the gate that returns the Gemini and Claude/GPT groups |
| 2026-10-22 | Render backend group names directly | Avoids inventing Gemini/Antigravity/Other semantics |
| 2026-10-22 | Keep per-model data for live auto-switch | Auto-switch still benefits from model-level exhaustion detail |

## Completion

- Commit/tag: `fix(cli): match antigravity quota groups`
- Verification result: `make verify` and `make verify-full` passed; real `usage` matched `agy -p /usage` for Gemini and Claude/GPT, and `usage --all` compared three accounts.
- Residual risk: group names and tier-specific groups are backend-driven; the `antigravity/cli/1.4.5` User-Agent marker is intentionally fixed.
