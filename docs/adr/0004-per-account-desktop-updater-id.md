# ADR-0004: Per-account Desktop updater id

- **Status:** Accepted
- **Date:** 2026-11-08
- **Decision makers:**
- **Consulted:**
- **Related:** ADR 0002, ADR 0003, `docs/exec-plans/completed/2026-11-08_per-account-desktop-updater-id.md`

## Context

Antigravity Desktop stores one UUID at `.updaterId` in its user-data directory (`~/.config/Antigravity/` on Linux). That file is not part of the session snapshot. ADR 0002 swaps `Cookies`, `Local Storage`, `app_storage.json`, and `Preferences`, so every account on one machine still shared `.updaterId`. It is not the CLI `installation_id` and it is not a credential.

## Decision drivers

- The live file must stay a raw UUID, because that is what Desktop reads.
- The id is not a secret. It does not belong in the encrypted credential snapshot.
- The account already using Desktop should keep the id on disk. A later account needs a different one.
- Desktop `add` and `switch` are the moments the live file changes. `--target=cli` must not touch it.
- The profile swap already refuses to write while Desktop is running unless `--relaunch` is set. The updater id follows that rule.

## Considered options

- **Option A** — Store `desktop/<id>/.updaterId` inside the profile snapshot. Restore it with the profile. Claim an unowned live id, otherwise mint.
- **Option B** — Sidecar next to the credential, like `accounts/<id>.installation_id`.
- **Option C** — Leave `.updaterId` shared.

## Decision

Option A. The copy lives inside the desktop profile, so delete, clean, and backup already cover it. `HasProfile` does not treat the file as a session. A snapshot that lacks the file does not delete the live one. If the account has no stored id, and the live id was bound to the active desktop account or is still unowned after that bind succeeds, the account claims the live id. If the live id cannot be bound, the incoming account gets a new UUID instead. `--target=cli` does not write it.

## Consequences

- **Good:** Each account can report its own Desktop updater id after the next desktop switch. No logout is required.
- **Neutral:** The CLI installation id is unchanged. The Chromium Safe Storage key stays one per OS user.
- **Bad:** A crash after the session restore and before the updater-id write leaves the new account on the previous id until the next successful desktop switch. An unowned id is bound to the registry's active account when CLI and Desktop have been switched separately.

## Confirmation

Desktop tests cover snapshot, restore, and a missing file. CLI tests cover claim, mint, restore, `--target=cli`, and a running Desktop app.

## Links

- [Internals](../internals.md)
- [User guide](../user-guide.md)
- [ADR 0002](0002-antigravity-desktop-support.md)
