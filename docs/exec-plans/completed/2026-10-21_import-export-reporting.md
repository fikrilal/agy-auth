# Import/export reporting and `import --purge`

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_backups-and-clean.md` |

## Objective

Make bulk imports and exports report every file, keep invalid files from
aborting a directory import, and add a supported registry rebuild from stored
account snapshots.

## Context

Directory imports currently abort on the first unreadable or invalid file and
print only the applied accounts. Exports abort on the first unreadable
snapshot. There is no supported way to rebuild `registry.json` from
`accounts/*.json` without re-importing a directory and losing aliases.

## Scope

### In scope

- Shared transfer report types:
  - per-file outcome `imported` / `updated` / `skipped` / `exported`;
  - reason for skipped files;
  - import/export summary counts.
- `import`:
  - sorted per-file report;
  - success rows on stdout, skipped rows on stderr;
  - one summary line;
  - directory imports continue after invalid files; a single invalid file exits
    nonzero;
  - `--alias`/`--activate` remain single-file only;
  - preserve the existing `imported` JSON account array and add `files` and
    `summary`.
- `import --purge`:
  - rebuild a fresh registry from `accounts/*.json`;
  - carry aliases and timestamps from the previous registry by ID or subject;
  - prefer a matching live credential for `active`, then the previous active,
    then the first snapshot;
  - carry `previous` where valid;
  - skip invalid/duplicate snapshots and report them;
  - reject `--alias`/`--activate` and positional paths with `--purge`.
- `export`:
  - per-file exported/skipped report;
  - summary line and JSON summary;
  - deterministic filenames remain;
  - continue after an unreadable/unwritable account;
  - all-skipped export exits nonzero.
- Command specs/help, README, internals, changelog, tests.

### Out of scope

- Importing JSON arrays or other credential formats.
- A first-class backup restore command.
- Changing export filenames or adding archive formats.
- Purge from an arbitrary directory path; purge uses the configured
  `accounts/` directory only.
- Atomic export file replacement.

## Expected paths

- `internal/cli/transfer.go`
- `internal/cli/cmd_import.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `internal/registry/registry.go`
- `README.md`
- `docs/internals.md`
- `CHANGELOG.md`

## Risks

- JSON import/export payloads gain `files`/`summary`; existing `imported` and
  `exported` arrays are retained.
- Directory imports now succeed when some files are skipped; a summary and
  stderr rows make that visible.
- Purge must not lose aliases accidentally; carry-forward is keyed by ID then
  subject, and invalid snapshots do not trigger a save.
- A live credential match can override the previous active hint; this follows
  the “live credential is authoritative” rule used by `list`/`status`.

## Acceptance criteria

- [x] Directory import reports each file and continues after invalid files.
- [x] Single invalid import exits nonzero with a skipped report.
- [x] Import summary counts imported/updated/skipped and total.
- [x] `--alias` and `--activate` remain single-file only.
- [x] `import --purge` rebuilds from snapshots and carries aliases/active/previous where possible.
- [x] Purge works with a missing or malformed registry and skips invalid snapshots.
- [x] Export reports per-file exported/skipped results and a summary.
- [x] All-skipped export exits nonzero.
- [x] Help, JSON, and docs are updated.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Directory with two valid files and one invalid: 2 imported, 1 skipped, exit 0.
- Single invalid file: 0 imported, 1 skipped, exit 1.
- Import the same valid file again: updated.
- `import --purge` with aliases/active/previous set: values carried forward.
- Delete `registry.json`, then `import --purge`: rebuilds from snapshots.
- Export with one missing snapshot: one exported, one skipped, summary counts.
- Export with all snapshots missing: exit 1.

## Rollback

Revert this commit. The registry schema is unchanged; purge derives metadata
from existing snapshots.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Directory imports skip invalid files; single-file invalid fails | Matches the reference workflow and keeps bulk recovery usable |
| 2026-10-21 | Keep `imported`/`exported` arrays and add `files`/`summary` | Limits breakage for existing JSON consumers |
| 2026-10-21 | Purge uses only the configured `accounts/` directory | The UX analysis scope is registry recovery, not arbitrary path migration |
| 2026-10-21 | Live credential wins over the old active hint | Live state is authoritative elsewhere |
| 2026-10-21 | Purge saves once, after all snapshots are processed | Avoids intermediate registries and backup churn |
| 2026-10-21 | Export skips failures and exits nonzero only when nothing exported | One bad snapshot should not hide the rest |

## Completion

- Commit/tag: `feat(cli): add import/export reporting and purge`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: import/export JSON gained additive `files`/`summary` fields; export writes are not atomic; purge only rebuilds from the configured `accounts/` directory.
