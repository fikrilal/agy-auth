# CLI contract foundation: command specs, generated help, usage errors

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/harness.md` |

## Objective

Create one command specification table that drives top-level help, per-command
help, and usage-error rendering, so command behavior and documentation cannot
drift independently.

## Context

`internal/cli/help.go` currently duplicates command usage in static strings, and
usage errors are inconsistent: some paths print help and return `2`, while
others print a one-line `error:` and return `1`. This is the first Phase 0 slice
from the codex-auth UX analysis and the foundation for the JSON error contract.

## Scope

### In scope

- `internal/cli/spec.go` with command name, aliases, group, synopsis, summary,
  usage lines, description, options, examples, and notes.
- Generated top-level and per-command help.
- `error: ...` + usage block + `hint:` rendering with exit code `2` for usage
  errors.
- Flag parse errors routed through the same renderer; `--help` still exits `0`.
- Command-specific usage validation for extra positionals, `remove --all` plus a
  query, and invalid alias subcommands.
- Tests for help generation and usage-error exit codes.
- Changelog entry.

### Out of scope

- JSON envelope v1 and JSON usage errors (next slice).
- Query/ambiguity semantics and `switch -`.
- Interactive picker changes.
- Backups/clean.
- Any loop-engineering controller.

## Expected paths

- `internal/cli/spec.go`
- `internal/cli/help.go`
- `internal/cli/app.go`
- `internal/cli/cmd_*.go`
- `internal/cli/cli_test.go`
- `CHANGELOG.md`

## Risks

- Help text regressions; mitigate with exact-output tests for `--help`.
- Exit-code changes for usage errors could affect scripts; this is intentional and
  limited to usage errors, not operation failures.
- Large mechanical diff; keep behavior unchanged outside help/usage.

## Acceptance criteria

- [x] One spec table drives top-level help and every `--help` page.
- [x] Usage errors print `error:`, `Usage:`, and `hint:` and exit `2`.
- [x] `--help` exits `0`.
- [x] Extra-positional, invalid-alias-subcommand, and `remove --all` + query cases exit `2`.
- [x] Existing command behavior and tests remain green.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- `agy-auth help` lists every command and alias.
- `agy-auth <command> --help` prints `Usage:` and exits `0`.
- `agy-auth unknown` exits `2` with a hint.

## Rollback

Revert the CLI-contract commit. No credential or storage changes are involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Keep dispatch in `app.go`; specs drive help/usage only | Smaller first slice; avoids a risky command-routing rewrite |
| 2026-10-21 | Treat usage errors as exit `2` | Matches codex-auth and the planned JSON contract |
| 2026-10-21 | Defer JSON usage errors | Keeps this slice reviewable; JSON envelope is the next slice |

## Completion

- Commit/tag: `feat(cli): add command specs and generated help`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: usage exit codes changed from `1` to `2` intentionally; JSON usage errors are deferred to the JSON-envelope slice.
