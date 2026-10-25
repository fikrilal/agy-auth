# ADR-0002: Antigravity Desktop support and profile management

- **Status:** Accepted
- **Date:** 2026-10-25
- **Decision makers:** fikrilal
- **Consulted:**
- **Related:** [`docs/exec-plans/completed/2026-10-25_antigravity-desktop-support.md`](../exec-plans/completed/2026-10-25_antigravity-desktop-support.md), [`docs/internals.md`](../internals.md)

## Context

`agy-auth` was designed to snapshot and restore Google OAuth credentials for the `agy` CLI using the OS keyring (`gemini`/`antigravity`) and a fallback file mirror (`~/.gemini/antigravity-cli/antigravity-oauth-token`).

The Antigravity Desktop app is an Electron application that maintains separate session authentication inside its Chromium profile directory (`~/.config/Antigravity/` on Linux) using SQLite cookie databases (`Cookies`) encrypted with Chromium Safe Storage in the OS keyring, LevelDB local storage, and application state (`app_storage.json`). Switching the CLI credential does not switch the Desktop app's user session.

## Decision drivers

- Provide unified multi-account switching for the entire Antigravity suite (CLI and Desktop).
- Prevent database corruption caused by modifying SQLite or LevelDB while Desktop is actively running.
- Provide a smooth, optional relaunch experience (`--relaunch`) for desktop users.
- Keep directory and file permissions strict (`0700`/`0600`) and ensure atomic file operations with backups.

## Considered options

- **Option A: CLI-only (Status Quo)** — Leave Desktop unmanaged. Users must manually log out and log in through the GUI.
- **Option B: Dual-target with default `--target=all` and `--relaunch`** — Manage Desktop session profiles in `~/.config/agy-auth/desktop/<account-id>/`, snapshot auth files (`Cookies`, `Local Storage`, `app_storage.json`, `Preferences`), switch both by default, and offer `--relaunch` to restart Desktop.
- **Option C: In-memory live token injection** — Attempt to inject tokens into the running Electron or language server process over loopback. Rejected because Electron maintains session cookies in native Chromium webviews and does not expose a credential hot-swap API.

## Decision

Adopt **Option B**:
1. Default target is `all` (`--target=all`), switching both CLI credentials and Desktop session profiles.
2. Introduce `internal/desktop` package for profile discovery, whitelist-based snapshotting, and atomic restoration.
3. Add process collision protection: if Desktop is running without `--relaunch`, CLI switches cleanly and Desktop file modification is safely blocked with an informative warning.
4. Support `--relaunch` to gracefully terminate running Desktop instances (`SIGTERM`, 5s wait), swap files, and start Desktop detached.

## Consequences

- **Good:** Single command switches accounts for both CLI and Desktop; zero SQLite corruption risk; fast profile swaps omitting heavy cache files.
- **Neutral:** Swapping desktop profiles requires restarting Antigravity Desktop.
- **Bad:** None. Users who only want CLI switching can specify `--target=cli`.

## Confirmation

- Unit tests in `internal/desktop` and `internal/cli`.
- Architecture lint in `internal/harness/lint`.
- Full verification pass with `make verify-full`.
