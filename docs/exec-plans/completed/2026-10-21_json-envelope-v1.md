# JSON envelope v1 and JSON usage errors

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/engineering/agent-workflow.md` |

## Objective

Make every `--json` response a stable, versioned document: one envelope with
`schema_version`, `command`, and either `data` or `error`. Usage errors must
also honor `--json`, and warnings must stay on stderr.

## Context

JSON output is currently an ad-hoc map or struct per command. There is no
schema version, errors are human text, and `switch`/`login` put warnings inside
the JSON document. Scripts cannot reliably distinguish success from failure or
parse warnings separately.

## Scope

### In scope

- `Envelope{schema_version, command, data, error}` and `ErrorDoc{code, message, hint, candidates}`.
- `emitJSON` wraps every existing success payload under `data`.
- `App.Command` carries the canonical command name for aliases (`ls` → `list`,
  `use` → `switch`, `rm` → `remove`, `paths` → `path`).
- `usage`, `usageTop`, and `fail` emit an error envelope when `--json` is set;
  exit codes stay `2` for usage and `1` for operation errors.
- Pre-scan behavior is already provided by `extractGlobals`; cover flags after
  positionals and unknown commands with tests.
- Remove `warnings` from `switch` and `login` JSON payloads; write them to stderr.
- Envelope tests for `version`, `path`, `list`, usage errors, operation errors,
  warning separation, and exactly one JSON document on stdout.
- README JSON/exit-code documentation and changelog entry.

### Out of scope

- Specific operation error codes (`account_not_found`, `ambiguous_query`,
  `registry_error`, `state_uncertain`); this slice uses `operation_failed` as a
  temporary code.
- Ambiguity candidate payloads.
- Interactive JSON guards (`switch --json` without a query, `remove --json`
  without selectors, `--live` + `--json`).
- A harness JSON contract fixture; the Go tests are the contract for now.

## Expected paths

- `internal/cli/jsonout.go`
- `internal/cli/render.go`
- `internal/cli/app.go`
- `internal/cli/spec.go`
- `internal/cli/help.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/cmd_login.go`
- `internal/cli/cli_test.go`
- `README.md`
- `CHANGELOG.md`

## Risks

- This is a breaking change for `--json` consumers; it is intentional and
  versioned at `schema_version: 1`.
- Existing tests unmarshal top-level payloads; update them to unwrap `data`.
- Errors must remain exactly one JSON document on stdout; the test helper
  decodes twice and requires EOF.

## Acceptance criteria

- [x] Every successful `--json` command emits `schema_version: 1`, `command`, and `data`.
- [x] `--json` usage and operation errors emit `schema_version`, `command`, and `error`.
- [x] Exit codes remain `0` success, `1` operation error, `2` usage error.
- [x] `--json` works after positionals and for unknown commands/flags.
- [x] Warnings never appear inside JSON; they go to stderr.
- [x] stdout contains exactly one JSON document plus a newline.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `agy-auth version --json` has `command: "version"`.
- `agy-auth path --json` has `command: "path"`.
- `agy-auth list extra --json` exits `2` with `error.code: "usage"`.
- `agy-auth current --json` with no credentials exits `1` with `error.code: "operation_failed"`.
- `agy-auth switch` warnings appear on stderr, not in `data`.

## Rollback

Revert this commit. The envelope is additive to non-JSON behavior; no credential
or registry changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Use `data` for command payloads instead of top-level typed fields | Preserves existing payloads and lets all commands migrate in one pass |
| 2026-10-21 | JSON error documents go to stdout | Keeps one machine-readable document on stdout; warnings remain stderr |
| 2026-10-21 | Use `operation_failed` until the error taxonomy slice | Avoids blocking the envelope on error-code design |
| 2026-10-21 | Canonicalize aliases in `command` | Consumers get stable names regardless of the invoked alias |

## Completion

- Commit/tag: `feat(cli): add versioned JSON envelope`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: operation errors use the temporary `operation_failed` code; the stable error taxonomy, ambiguity candidates, and interactive JSON guards remain follow-up work.
