# Per-account Desktop updater id

| Field | Value |
|---|---|
| Status | completed |
| Owner | |
| Date | 2026-11-08 |
| Related | `docs/adr/0004-per-account-desktop-updater-id.md`, ADR 0002, ADR 0003 |

## Objective

Each stored account keeps the Antigravity Desktop `.updaterId` it reports, and a desktop `switch` or `add` writes that id back to the live Desktop user-data directory.

## Context

Desktop profile snapshots copy session files only. `.updaterId` stays one UUID for the OS user, so every account shares it. The file is a 36-byte UUID, not a credential. The CLI installation id is already per account and must stay independent.

## Scope

### In scope

- Copy `.updaterId` with the desktop profile snapshot and restore it on desktop switch.
- Keep the signed-in account's current id. Mint a new UUID for an account that has none when the live id is already taken or cannot be bound.
- Leave the file alone for `--target=cli`, and while Desktop is running without `--relaunch`.
- Tests, ADR, and the user-facing docs.

### Out of scope

- `jetski_state.pbtxt`, the rest of the Desktop user-data directory, and the CLI installation id.
- Changing the Chromium Safe Storage key.

## Expected paths

- `internal/desktop/desktop.go`
- `internal/registry/registry.go`
- `internal/cli/desktop_updater.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/cmd_list.go`
- `docs/adr/0004-per-account-desktop-updater-id.md`

## Risks

- A crash after the session files are restored and before `.updaterId` is written leaves the new account on the previous id until the next successful desktop switch.
- If the CLI active account and the Desktop session have diverged, an unowned live id is bound to the active account when that account has a desktop profile.

## Acceptance criteria

- [x] `add` for `all` or `desktop` stores the live id on that account when no other account has it.
- [x] Desktop `switch` restores a stored id and mints one for an account that has none.
- [x] `--target=cli` does not change `.updaterId`.
- [x] A running Desktop app blocks the updater-id write the same way it blocks the profile swap.
- [x] A snapshot that has no `.updaterId` does not delete the live file.

## Verification plan

```shell
make verify-full
```

## Rollback

Stop copying and restoring `.updaterId`. Existing `desktop/<id>/.updaterId` files are unused and can be deleted. The live file remains whatever was written last.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-11-08 | Store the id inside the desktop profile directory | It is removed with the profile, backed up with the profile, and does not need a registry schema change. |
| 2026-11-08 | Claim or mint like the CLI installation id | The account already using the machine keeps its id. Later accounts do not reuse it. |

## Completion

- Commit/tag: not committed
- Verification result: `make verify-full` passed on 2026-11-08
- Residual risk: a crash after the session restore and before the updater-id write leaves the new account on the previous id until the next successful desktop switch. If CLI and Desktop were switched separately, an unowned live id is bound to the active account when that account has a desktop profile. Restart Desktop after switching.
