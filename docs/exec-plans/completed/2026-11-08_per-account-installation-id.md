# Per-account installation id

| Field | Value |
|---|---|
| Status | completed |
| Owner | |
| Date | 2026-11-08 |
| Related | `_WIP/2026-11-08_agy-machine-identity.md`, ADR 0003 |

## Objective

Each stored account keeps its own Antigravity CLI installation id, and `switch` / `login` write that id back to `installation_id` beside the live credential.

## Context

`agy` keeps one UUID at `~/.gemini/antigravity-cli/installation_id` and attaches it to telemetry. `agy-auth` swaps the credential only, so every account reports the same install id. Findings are in `_WIP/2026-11-08_agy-machine-identity.md`.

## Scope

### In scope

- Snapshot the id next to the account credential.
- On CLI switch, login, add, and activated import, restore that account's id.
- Keep an existing live id with the account that is using it. Mint a new id only when the account has none and the live id belongs to someone else.
- Tests, internals, user guide, ADR.

### Out of scope

- Swapping `jetski_state.pbtxt`, Desktop `.updaterId`, or the rest of the CLI home.
- Changing the User-Agent.
- Putting the id in the encrypted credential JSON or the registry schema.

## Expected paths

- `internal/agy/installation.go`
- `internal/registry/registry.go`
- `internal/cli/installation.go`
- `internal/cli/cmd_switch.go`
- `internal/cli/cmd_login.go`
- `internal/cli/cmd_list.go`
- `internal/cli/cmd_import.go`
- `docs/internals.md`
- `docs/user-guide.md`
- `docs/adr/0003-per-account-installation-id.md`

## Risks

- A switch that writes the credential and then fails to write the id leaves the new account on the previous id until the next successful switch.
- `clean` must not delete a sidecar that still belongs to a registry account, and must not delete snapshots when `registry.json` is missing.

## Acceptance criteria

- [x] Adding the live account stores the current installation id and leaves the live file unchanged.
- [x] Switching to an account with no id mints a different id and writes it live.
- [x] Switching back restores the first account's id.
- [x] Removing an account removes its sidecar. `clean` removes orphan sidecars and keeps sidecars for registered accounts.
- [x] `make verify-full` passes.

## Verification plan

```shell
make verify-full
```

Additional checks:

- `go test ./internal/agy ./internal/registry ./internal/cli`

## Rollback

Stop writing the sidecar and stop restoring it on switch. Existing `accounts/<id>.installation_id` files are unused and can be deleted. The live `installation_id` file remains whatever was written last.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-11-08 | Store a raw UUID sidecar, not a registry field | The id is not a credential. Registry version stays 2. |
| 2026-11-08 | Claim the live id for the account that currently holds the credential | The first account keeps the install id `agy` already created. |

## Completion

- Commit/tag: not committed
- Verification result: `make verify-full` passed on 2026-11-08
- Residual risk: a crash after the credential write and before the installation-id write leaves the new account on the previous id until the next successful switch. The source IP is unchanged.
