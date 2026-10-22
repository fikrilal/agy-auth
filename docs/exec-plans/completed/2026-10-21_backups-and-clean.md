# Backups, retention, and `clean`

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainer |
| Date | 2026-10-21 |
| Related | `_WIP/codex-auth-ux-analysis.md`, `docs/exec-plans/completed/2026-10-21_candidate-pickers-and-atomic-remove.md` |

## Objective

Add a local recovery tier before credential and registry changes, keep it
bounded, and provide `agy-auth clean [--dry-run]` for managed backups and
orphan snapshots.

## Context

`switch`, `login`, `import --activate`, `remove --logout`, and registry saves
mutated credential state without a rollback point. There was no retention, no
way to prune stale data, and no cleanup for snapshots no longer referenced by
`registry.json`.

## Scope

### In scope

- `internal/backup` package:
  - managed `active-<timestamp>.json` and `registry-<timestamp>.json` names;
  - `0600` files in a `0700` directory;
  - retention of 5 per kind;
  - identical-content dedupe;
  - prune and dry-run APIs;
  - strict managed-name whitelist.
- `agy.Storage`:
  - snapshot current keyring/file credential values before replacing or
    deleting them;
  - skip backups for identical credentials;
  - write-if-changed for the file mirror and permission re-hardening;
  - `ErrBackup` so callers can return `backup_error`.
- `registry.Store`:
  - back up the previous registry before writing changed bytes;
  - write-if-changed and permission re-hardening;
  - `Clean(dryRun)` report with backups, orphan snapshots, and preservation
    when `registry.json` is missing;
  - `ErrBackup` propagation.
- CLI:
  - `agy-auth clean [--dry-run]` with help, JSON envelope, and error codes;
  - map backup failures to `backup_error` and clean failures to `clean_failed`;
  - back up the live credential before `remove --logout` deletes it;
  - add `clean` to command-help coverage.
- Architecture/harness:
  - allow the new `internal/backup` package in the import graph.
- Tests, README, internals, SECURITY, contributor docs, changelog.

### Out of scope

- Offsite or encrypted backup archives; backups are local and contain the same
  secrets as snapshots.
- `import --purge`, schema migrations, or live usage.
- A true transaction across filesystem deletes and registry writes.
- Restoring a backup through a first-class command; recovery is manual for now.

## Expected paths

- `internal/backup/backup.go`
- `internal/backup/backup_test.go`
- `internal/agy/storage.go`
- `internal/agy/storage_test.go`
- `internal/registry/registry.go`
- `internal/registry/registry_test.go`
- `internal/cli/app.go`
- `internal/cli/cmd_clean.go`
- `internal/cli/cmd_*.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `internal/harness/lint/lint.go`
- `README.md`
- `docs/internals.md`
- `docs/engineering/architecture.md`
- `SECURITY.md`
- `AGENTS.md`
- `CONTRIBUTING.md`
- `CHANGELOG.md`

## Risks

- Backups contain live tokens; they must remain `0600` inside `0700` and are
  local-only.
- A backup failure blocks the credential/registry change; this is intentional.
- Clean can delete data; it must only touch managed backup names and valid
  orphan snapshot names, and must preserve snapshots when the registry is
  missing or malformed.
- Registry saves happen often; write-if-changed and backup dedupe prevent churn.
- The architecture lint must be updated for the new package.

## Acceptance criteria

- [x] Live credential is backed up before replacement or deletion, including both keyring and file-mirror values when they differ.
- [x] Registry is backed up before a changed save; identical saves only re-harden permissions.
- [x] Backups are `0600` in a `0700` directory, deduplicated, and retained to 5 per kind.
- [x] `clean --dry-run` reports without deleting.
- [x] `clean` prunes managed backups and orphan snapshots, preserving unknown files.
- [x] Snapshots are preserved when `registry.json` is missing or malformed.
- [x] Backup failures return `backup_error`; clean failures return `clean_failed`.
- [x] `clean` has generated help and JSON output.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Two credential writes with different contents create one backup of the prior value.
- Six registry backups leave five after a save/prune.
- `clean --dry-run` leaves the files in place; `clean` removes only the reported set.
- Registry missing: `clean` reports preservation and leaves snapshots.
- Unknown files in `accounts/` and `backups/` are untouched.
- Remove `--logout` backs up the live credential before deleting it.

## Rollback

Revert this commit. Backups are additive files under `backups/`; removing the
feature does not change registry or credential formats.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-21 | Keep 5 of each backup kind, not 5 total | Credential and registry histories are independently useful |
| 2026-10-21 | Snapshot both keyring and file values when they differ | The mirror can drift; either value may be the recovery target |
| 2026-10-21 | Refuse the change when backup fails | A silent overwrite without a recovery point is worse than a blocked command |
| 2026-10-21 | Clean only managed names and valid account IDs | Prevent accidental deletion in the config directory |
| 2026-10-21 | Preserve snapshots when the registry is missing or malformed | Snapshots are the source for rebuilding |
| 2026-10-21 | `clean --dry-run` shares the same code path | Reports exactly what the real run would do |

## Completion

- Commit/tag: `feat(cli): add backups and clean`
- Verification result: `make verify` and `make verify-full` passed.
- Residual risk: backups are local plaintext credentials; restore is manual; snapshot deletion remains non-transactional.
