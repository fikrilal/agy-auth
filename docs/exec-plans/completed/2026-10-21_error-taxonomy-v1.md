# Stable error taxonomy and interactive JSON guards

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_json-envelope-v1.md` |

## Objective

Replace the temporary `operation_failed` code with a stable error taxonomy,
sanitize user-facing messages, include ambiguity candidates, and reject
interactive JSON invocations that cannot produce a deterministic result.

## Context

The JSON envelope is in place, but most operation failures still used one
generic code and repeated raw Go errors. Ambiguous queries returned only prose,
and `switch --json`/`remove --json` could enter an interactive path. Scripts
need stable codes, safe messages, and deterministic behavior.

## Scope

### In scope

- `failCode(code, hint, message, ...)` and `errorDoc(exit, ErrorDoc)` helpers.
- Stable codes: `usage`, `account_not_found`, `ambiguous_query`,
  `credential_not_found`, `credential_error`, `credential_write_failed`,
  `registry_error`, `state_uncertain`, `oauth_error`, `import_failed`,
  `invalid_credential`, `export_failed`, `confirmation_required`,
  `confirmation_declined`, `selection_failed`, `input_error`, `agy_not_found`,
  `state_conflict`, `output_error`.
- Sanitized messages and actionable hints for registry, credential, OAuth,
  import/export, selection, and output failures.
- `ambiguous_query` candidates with account ID, email, and alias.
- `switch --json` without a query and `remove --json` without selectors/`--all`
  return usage errors before any interactive path.
- JSON-mode stdout hygiene for `remove --logout`, cancellation, and empty
  selection.
- Tests for codes, hints, candidates, sanitization, guards, and human hints.
- README and changelog updates.

### Out of scope

- Query ordering/semantics changes and `switch -`.
- Ambiguity candidate pickers; candidates are data only in this slice.
- Atomic multi-selector `remove`.
- Backups/clean.
- `--live` guards; there is no live mode yet.
- A harness JSON schema contract fixture.

## Expected paths

- `internal/cli/app.go`
- `internal/cli/jsonout.go`
- `internal/cli/cmd_*.go`
- `internal/cli/cli_test.go`
- `README.md`
- `CHANGELOG.md`

## Risks

- Alias validation now exits `2` (usage) instead of `1`.
- Human-readable messages change; scripts should use the JSON codes.
- Warnings may still include diagnostic details; only error messages are
  sanitized in this slice.
- Broad call-site migration; tests cover the high-risk paths and the full suite
  covers behavior.

## Acceptance criteria

- [x] Operation failures use a stable code instead of `operation_failed`.
- [x] User-facing error messages do not include raw Go errors or registry paths.
- [x] Ambiguous queries include candidates with ID, email, and alias.
- [x] Interactive JSON guards return `usage`/`2` before loading state.
- [x] JSON stdout remains one document in `remove --logout` and cancellation paths.
- [x] Existing behavior and tests remain green.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `current --json` with no credentials: `credential_not_found`, exit `1`, hint present.
- `switch --json` without a query: `usage`, exit `2`.
- `remove --json` without selectors: `usage`, exit `2`.
- Corrupt `registry.json` + `list --json`: `registry_error`, no path in message.
- Ambiguous query: `ambiguous_query` with two candidates.
- `alias set` with an invalid alias: usage error, exit `2`.

## Rollback

Revert this commit. No credential or registry format changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Alias validation uses `usage`/`2` | It is input validation, not an operation failure |
| 2026-10-21 | Candidates contain ID/email/alias only | Enough to resolve without exposing fingerprints |
| 2026-10-21 | Warnings keep diagnostic detail | Warnings are stderr-only and useful for support |
| 2026-10-21 | Remove the generic `operation_failed` fallback | Every known failure site is classified |

## Completion

- Commit/tag: `feat(cli): add stable error codes and JSON guards`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: warnings may include diagnostic details on stderr; query semantic changes, `switch -`, and atomic remove remain follow-up work.
