# ADR-0003: Per-account CLI installation id

- **Status:** Accepted
- **Date:** 2026-11-08
- **Decision makers:**
- **Consulted:**
- **Related:** `_WIP/2026-11-08_agy-machine-identity.md`, `docs/exec-plans/completed/2026-11-08_per-account-installation-id.md`

## Context

Antigravity CLI stores one UUID at `~/.gemini/antigravity-cli/installation_id`. That value is the install id `agy` attaches to telemetry. It is not part of the OAuth credential and it is not sent on the Cloud Code quota or model calls. `agy-auth` swapped only the credential, so every account on one machine reported the same install id.

## Decision drivers

- The live file must stay a raw UUID, because that is what `agy` reads.
- The id is not a secret. It does not belong in the encrypted credential snapshot.
- The account that is already signed in should keep the id `agy` created. A later account needs a different one.
- Switch, login, add, and an activated import are the moments the live file changes.

## Considered options

- **Option A** — Sidecar `accounts/<id>.installation_id`, plaintext, mode `0600`.
- **Option B** — New field on the account record in `registry.json`.
- **Option C** — Leave the live file alone.

## Decision

Option A. `switch` and `login` write the selected account's UUID back to the live `installation_id` file with the same atomic `0600` helper used for the credential file. If the account has no stored id, and the live id was bound to the signed-in account or is still unowned after that bind succeeds, the account claims the live id. If the live id cannot be bound, the incoming account gets a new UUID instead. Registry version stays 2. `add --target=desktop` does not write the CLI installation id.

## Consequences

- **Good:** Each account reports its own install id on later telemetry. The original id stays with the account that was using the machine.
- **Neutral:** Cloud Code requests are unchanged. They identify the account by the bearer token. The source IP is unchanged.
- **Bad:** A crash after the credential write and before the id write leaves the new account on the previous id until the next successful switch.

## Confirmation

`TestSwitchSwapsInstallationID` covers capture, mint, and restore. Registry tests cover the sidecar round trip, delete, and clean.

## Links

- [Internals](../internals.md)
- [User guide](../user-guide.md)
