# Antigravity Desktop support

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-25 |
| Related | [`docs/adr/0002-antigravity-desktop-support.md`](../../adr/0002-antigravity-desktop-support.md), [`docs/internals.md`](../../internals.md) |

## Objective

Provide unified multi-account switching for the Antigravity Desktop App alongside the `agy` CLI, with default `--target=all` switching, process collision safety, and `--relaunch` support.

## Context

`agy-auth` currently only swaps OS keyring credentials and fallback token files for the CLI. The Antigravity Desktop App runs as an Electron application with separate profile storage in `~/.config/Antigravity/` (`Cookies`, `Local Storage/leveldb`, `app_storage.json`, `Preferences`). Users cannot switch accounts in the Desktop app without manual logout/login workflows.

## Scope

### In scope

- `internal/desktop` package:
  - Cross-platform profile directory resolution (`~/.config/Antigravity`, macOS Application Support, Windows AppData).
  - Process detection for running `antigravity` Electron instances.
  - Whitelist-based session profile snapshot (`Cookies`, `Local Storage`, `app_storage.json`, `Preferences`).
  - Atomic profile restore with automatic backup creation (`backups/desktop-<timestamp>`).
  - Graceful process termination and background relaunch (`--relaunch`).
- `internal/registry` extension:
  - Track desktop profile availability (`has_desktop`) and desktop snapshots per account.
- `internal/cli` commands:
  - Add `--target=all|cli|desktop` (defaulting to `all`).
  - Add `--relaunch` to `switch`.
  - Process safety check in `switch`: if Desktop is running without `--relaunch`, switch CLI and warn/block Desktop mutation.
  - Update `status`, `add`, `list`, `spec.go`.
- Comprehensive unit tests, harness verification (`make verify-full`), and installation to machine (`make install`).

### Out of scope

- Modifying or patching Electron binaries or ASAR packages.
- Live-swapping session state inside running Electron memory without restarting the Desktop app.
- Supporting multiple concurrent running Desktop instances with different accounts.

## Expected paths

- `internal/desktop/{desktop,desktop_test}.go`
- `internal/registry/{registry,registry_test}.go`
- `internal/cli/{app,cmd_switch,cmd_status,cmd_add,spec,cli_test}.go`
- `docs/internals.md`, `docs/user-guide.md`, `README.md`

## Risks

- **Concurrent database corruption:** Modifying SQLite `Cookies` or LevelDB while Desktop is open can corrupt user session state.
  *Mitigation:* Check process table; refuse mutation unless stopped or `--relaunch` is explicitly passed.
- **Snapshot bloat:** Unfiltered directory copies can copy hundreds of megabytes of caches.
  *Mitigation:* Strict file/folder whitelist (`Cookies*`, `Local Storage`, `app_storage.json`, `Preferences`).
- **Relaunch zombie/hang:** Spawning or killing Desktop processes could hang the CLI or leak child processes.
  *Mitigation:* Timeout-bounded `SIGTERM` (5s), fallback kill, detached daemon process spawn.

## Acceptance criteria

- [x] `internal/desktop` discovers profile path, detects running process, snapshots auth files, and restores atomically.
- [x] `agy-auth switch <account>` defaults to `--target=all`.
- [x] If Desktop is running and `--relaunch` is absent, CLI switches cleanly and Desktop swap is safely deferred with an actionable warning.
- [x] `agy-auth switch <account> --relaunch` cleanly closes running Desktop, swaps profile, and relaunches the app detached.
- [x] `--target=cli` and `--target=desktop` restrict switching to the requested surface.
- [x] Atomic backups created in `backups/desktop-<timestamp>` before replacement.
- [x] `make verify-full` passes cleanly with zero lint, test, or contract failures.
- [x] Binary built and installed to machine (`~/.local/bin/agy-auth`).

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:
- Verify CLI only switch: `agy-auth switch <account> --target=cli`
- Verify Desktop safety gate when Desktop is running
- Verify `--relaunch` terminates and restarts Desktop

## Rollback

Revert code changes via git. Delete any created `~/.config/agy-auth/desktop/` directories if needed; existing CLI snapshots in `~/.config/agy-auth/accounts/` remain untouched.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-25 | Default target is `all` | Provides single-command multi-account switching for the entire Antigravity suite. |
| 2026-10-25 | Adopt `--relaunch` | Offers frictionless restart of Desktop when switching accounts from the terminal. |
| 2026-10-25 | Process safety check | Prevents corrupting SQLite/LevelDB when Desktop is running without `--relaunch`. |

## Completion

- Verification result: `make verify-full` passed with 0 findings, race tests clean, package dry run verified.
- Residual risk: User running custom packaging of Antigravity in non-standard directories can set `AGY_DESKTOP_HOME` and `AGY_DESKTOP_BIN`.
