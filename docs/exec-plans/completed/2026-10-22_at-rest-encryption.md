# At-rest encryption and schema migration

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Internals](../../internals.md); optional CPA import remains deferred |

## Objective

Remove plaintext refresh tokens from account snapshots and active-credential backups, and add a versioned registry migration path. The optional CPA/CLIProxyAPI importer is deferred because no user has requested that third-party format.

## Context

Snapshots were raw agy credential JSON at `0600`, and active-credential backups were raw JSON at `0600`. File permissions are not encryption, especially when files are synced or copied. The registry had only an implicit version-1 shape and no ordered migration framework.

## Scope

### In scope

- AES-256-GCM snapshot vault with a separate `encryption.key` (`0600`) and exclusive lock during first creation.
- Encrypt `accounts/*.json` on write and decrypt transparently on read.
- Encrypt `backups/active-*.json`; leave metadata-only registry backups plaintext.
- Lazy migration of plaintext snapshots; migration of plaintext active backups during registry upgrade.
- Registry schema version 2 with ordered migrations, `config.snapshot_encryption`, automatic persistence, and newer-version rejection.
- Import handles plaintext or encrypted snapshots; export remains deliberately plaintext.
- Tests for ciphertext, key permissions/missing key, migration, backup encryption, import/purge/export.
- Docs/changelog/fixture updates.

### Out of scope

- OS-keyring-backed encryption keys.
- Passphrase-based export archives.
- Optional CPA/CLIProxyAPI import/export.
- Encrypting metadata-only registry backups.

## Expected paths

- `internal/agy/{snapshot,snapshot_test}.go`
- `internal/backup/{backup,backup_test}.go`
- `internal/registry/{registry,registry_test}.go`
- `internal/cli/{app,cmd_import,cmd_path,cli_test}.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/testing.md`, `harness/fixtures/registry.valid.json`

## Risks

- Losing `encryption.key` makes encrypted snapshots unrecoverable; document separate key backup.
- A corrupt/missing key must fail closed, not silently rewrite credentials.
- Migration writes during load; preserve the previous registry as a backup and avoid saving when validation fails.
- Export is intentionally plaintext and must be documented as a secret.
- Registry version bumps can break old binaries; they reject newer versions and retain v1 migration.

## Acceptance criteria

- [x] New snapshots and active backups contain no plaintext refresh/access tokens.
- [x] `LoadToken` round-trips and migrates legacy plaintext files.
- [x] Encryption key is `0600`, directory `0700`, with exclusive creation lock.
- [x] Missing key fails decryption instead of returning ciphertext.
- [x] Registry v1 migrates to v2 and persists `snapshot_encryption`.
- [x] Newer registry versions are rejected.
- [x] Active backups are encrypted; registry backups remain metadata-only.
- [x] Import accepts encrypted snapshots; export writes plaintext JSON.
- [x] `path` reports the encryption key path.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Copy the real config to a temporary `AGY_AUTH_HOME`, run `list`, and confirm snapshots are ciphertext and `encryption.key` exists.
- Export from the temporary home and confirm the exported file parses as a credential.

## Rollback

Keep `encryption.key`; reverting the code leaves encrypted snapshots unreadable by old binaries. Restore from exported plaintext credentials or remove snapshots and re-add accounts. Registry v2 is readable by this version only; downgrade requires restoring a registry backup or rebuilding with `import --purge`.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | AES-256-GCM with local key file | Stdlib-only, authenticated encryption, no external age/gpg dependency |
| 2026-10-22 | Encrypt snapshots and active backups, not registry backups | Registry backups contain metadata, not credentials |
| 2026-10-22 | Lazy snapshot migration | Avoid scanning/rewriting every account on unrelated commands |
| 2026-10-22 | Registry v2 migration in `Load` | Persist upgrades once and keep old binaries from reading unsupported data |
| 2026-10-22 | Defer CPA/CLIProxyAPI import | Optional third-party format; no request or stable contract yet |

## Completion

- Commit/tag: `feat(security): encrypt snapshots and migrate registry`
- Verification result: `make verify` and `make verify-full` passed. A copy of the real config migrated to registry v2 with a `0600` key, 3/3 snapshots ciphertext, no plaintext tokens, and valid plaintext export.
- Residual risk: losing `encryption.key` makes snapshots unrecoverable; exports are intentionally plaintext. Optional CPA/CLIProxyAPI import remains deferred.
