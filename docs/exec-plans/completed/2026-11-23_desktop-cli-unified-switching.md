# Unified CLI and Antigravity Desktop 2.x switching

| Field | Value |
|---|---|
| Status | completed |
| Owner | gray.fang |
| Date | 2026-11-23 |
| Related | [ADR-0002](../../adr/0002-antigravity-desktop-support.md), [ADR-0004](../../adr/0004-per-account-desktop-updater-id.md), [Internals](../../internals.md) |

## Objective

Make combined CLI + Antigravity Desktop switching (`--target=all`, `--relaunch`, `--close-desktop`) reliable on macOS, Linux, and Windows, aligned with Antigravity Desktop 2.x's embedded `language_server` credential and state storage.

## Context

`agy-auth` already ships Desktop profile snapshotting and restoration, but inspection against Antigravity Desktop 2.34.0 on macOS revealed four gaps:

1. `findDefaultProcesses()` used case-sensitive `pgrep -f antigravity`, missing `/Applications/Antigravity.app/Contents/MacOS/Antigravity` on macOS, matching only child `language_server` (or unrelated processes), and failing on Windows.
2. Antigravity Desktop 2.x authenticates through its embedded `language_server` using OS Keyring (`gemini`/`antigravity`) and a separate file fallback (`~/.gemini/jetski-standalone-oauth-token`), plus `credential-user-scope` and `~/.gemini/antigravity/installation_id`.
3. Accounts added via `agy-auth login` (without a captured Desktop profile) were silently skipped during Desktop switching and `--relaunch`, leaving Desktop running with stale in-memory credentials, `credential-user-scope`, `app_storage.json` last-login metadata, `.updaterId`, and Desktop `installation_id`.
4. `agy-auth add` did not guard against mismatched Desktop vs CLI emails, `agy-auth login` did not sync/relaunch Desktop, and `agy-auth switch --live` did not forward `--target`, `--relaunch`, or `--close-desktop`.

## Scope

### In scope

- `internal/agy/storage.go`: Add optional `DesktopFilePath` (`~/.gemini/jetski-standalone-oauth-token`, overridable via `AGY_AUTH_DESKTOP_TOKEN_FILE`) with atomic `WriteDesktopToken` and `DeleteDesktopToken`.
- `internal/desktop`:
  - Accurate cross-platform process detection (macOS `ps`, Linux `/proc`, Windows `tasklist`) prioritizing the main Electron process and tracking helper / embedded `language_server` children.
  - Platform-aware termination (`SIGTERM`/`SIGKILL` on Unix, `taskkill` on Windows) waiting for Desktop children to exit, plus macOS `.app` LaunchServices relaunch (`open -a`).
  - Snapshot/restore/clear `credential-user-scope` (including Linux lowercase `~/.config/antigravity/credential-user-scope`), read/sync `app_storage.json` onboarding username/GCP metadata, and resolve Desktop `DataDir` (`~/.gemini/antigravity`).
- `internal/registry`: Per-account Desktop `installation_id` (`desktop/<id>/installation_id`) storage and ownership lookup.
- `internal/cli`:
  - Unified Desktop sync in `switch`, `rotate`, and `login` (including accounts without a prior Desktop snapshot when Desktop is installed/running).
  - Email-mismatch safety guard and target validation in `add`.
  - Forward `--target`, `--relaunch`, `--close-desktop` in `switch --live`.
  - Support `--relaunch` and `--close-desktop` on `login`; clean up Desktop fallback token on `remove --logout`; expose Desktop paths in `path` and `status`.
- Update docs (`docs/internals.md`, `docs/user-guide.md`) and unit tests across `internal/{agy,desktop,registry,cli}`.

### Out of scope

- Standalone Desktop-only account creation without any CLI credential.
- Live hot-swapping credentials inside a running Electron / `language_server` process without restart.
- Modifying Antigravity Desktop binaries or ASAR bundles.

## Expected paths

- `internal/agy/storage.go`, `internal/agy/storage_test.go`
- `internal/desktop/desktop.go`, `internal/desktop/detach_unix.go`, `internal/desktop/detach_windows.go`, `internal/desktop/desktop_test.go`
- `internal/registry/registry.go`, `internal/registry/desktop_updater_test.go`
- `internal/cli/{app,cmd_list,cmd_login,cmd_path,cmd_remove,cmd_status,cmd_switch,desktop_updater,live,spec}.go`
- `internal/cli/{cli_test,desktop_updater_test}.go`
- `docs/internals.md`, `docs/user-guide.md`

## Risks

- **Process false positives:** Over-broad process matching could kill unrelated processes. *Mitigation:* Match only Desktop bundle/binary paths and exclude `os.Getpid()`.
- **Test/manual isolation:** Writing to default `~/.gemini/jetski-standalone-oauth-token` or `~/.gemini/antigravity/installation_id` during tests or isolated manual runs. *Mitigation:* Leave paths empty when overridden or constructed with explicit test directories.
- **Data corruption in `app_storage.json`:** Preserve all existing keys and write string values (`"true"`/`"false"`) atomically with `0600`.

## Acceptance criteria

- [x] macOS process parser accurately identifies `Antigravity.app` main PID ahead of helpers and embedded `language_server`, ignoring `agy` CLI and unrelated commands.
- [x] Desktop fallback token file (`jetski-standalone-oauth-token`), `credential-user-scope`, `app_storage.json` last-login fields, `.updaterId`, and Desktop `installation_id` stay synchronized during unified switches and logins.
- [x] `switch --relaunch` works for accounts both with and without a captured Desktop profile snapshot when Desktop is installed/running.
- [x] `add` warns/blocks on CLI-vs-Desktop email mismatch instead of cross-contaminating profiles.
- [x] `switch --live` forwards `--target`, `--relaunch`, and `--close-desktop`.
- [x] `make verify-full` passes cleanly with zero findings and race-free tests.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Run isolated manual verification with `AGY_AUTH_HOME`, `AGY_AUTH_STORAGE=file`, temporary `AGY_DESKTOP_HOME`, and verify macOS live process detection via `agy-auth status`.

## Rollback

Revert commits via git. Existing `accounts/*.json` snapshots, `registry.json` (schema unchanged), and `desktop/<id>/` snapshots remain backward compatible.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-11-23 | Keep `DesktopFilePath` separate from CLI `FilePath` | Preserves `--target=desktop` / `--target=cli` isolation and existing test contracts while keeping Desktop 2.x file fallback synced on unified switch. |
| 2026-11-23 | Use `ps -axo pid=,args=` on macOS and `open -a` for `.app` relaunch | Fixes case-sensitive `pgrep` bug without needing AppleEvents/TCC Automation permissions. |
| 2026-11-23 | Store Desktop `installation_id` under `desktop/<id>/installation_id` | Mirrors ADR-0004 `.updaterId` layout so delete/clean/backup automatically cover it. |
| 2026-11-23 | Disable host Desktop discovery when `AGY_AUTH_AGY_TOKEN_FILE` is overridden without Desktop overrides | Prevents isolated CLI tests (`pty_test.go`) and sandboxed manual CLI runs from modifying the host's live Antigravity Desktop directory. |

## Completion

- Commit/tag: uncommitted working tree (user commits when requested)
- Verification result: `make verify-full` passed (format, custom lints, secret scan, docs/version, contracts, `go vet`, `go build`, unit + `-race` tests, current npm target build, node shim smoke test, `npm pack --dry-run`); `make build` and isolated `status`/`path` smoke checks passed with `AGY_AUTH_NO_UPDATE_CHECK=1`.
- Residual risk: Live `--relaunch` end-to-end against a running macOS `Antigravity.app` instance should be smoke-tested once the user opens Antigravity Desktop; Windows `tasklist`/`taskkill` paths are unit-tested via parser tests rather than executed on a Windows host.
