# Isolated `login --via-agy`

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | [Internals](../../internals.md); [Architecture](../../engineering/architecture.md) |

## Objective

Make `login --via-agy` non-destructive: run agy in an isolated temporary home, import the resulting credential only after success, and leave the live active credential untouched on cancel/failure.

## Context

The previous flow saved the current account and then deleted the real live credential before launching agy. If agy exited early, the user canceled, or the login failed, the active slot was empty until a manual switch. The AGY login flow does not need the real home; `HOME` plus file-based credential storage is enough for sign-in.

## Scope

### In scope

- Temporary `HOME`/`USERPROFILE`, `XDG_*`, and `DBUS_SESSION_BUS_ADDRESS`/`XDG_RUNTIME_DIR` clearing for the child agy process.
- Read the temporary `antigravity-oauth-token`, validate it, and only then update the registry and real keyring/file credential.
- Keep the current live credential untouched on cancel, error, missing token, or invalid token.
- Remove the running-agy preflight and destructive delete.
- Tests for success, cancel, error, invalid token, and real credential preservation.
- Docs/spec/changelog updates.

### Out of scope

- Changing the direct OAuth login flow.
- Copying the full agy configuration into the temp home.
- Changing agy’s own login UI.

## Expected paths

- `internal/cli/{cmd_login,cli_test,spec}.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`, `docs/engineering/testing.md`

## Risks

- A fresh temp home may make agy initialize more slowly; login is already interactive.
- `agy` must write the temp credential to its file fallback when DBus/XDG runtime are cleared; the child environment enforces that.
- The temp token is sensitive; it is read within the temporary directory and deleted with the directory.

## Acceptance criteria

- [x] Success imports the new account and writes the real live credential.
- [x] Cancel/error/missing token leaves the real live credential byte-for-byte untouched.
- [x] Invalid temporary credentials are rejected without touching the real credential.
- [x] The temporary directory is removed on every path.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Success and failure tests use a fake `agy` script that writes or omits the temp token.
- Assert the real token file and registry active account are unchanged after failure.

## Rollback

Revert the `loginViaAgy` changes. The previous destructive flow returns; no registry or credential migration is involved.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Use a temporary HOME with file storage | Keeps the real keyring/file credential out of the agy login flow |
| 2026-10-22 | Import only after a valid temp token | Prevent failures from clearing the active account |
| 2026-10-22 | Remove the running-agy prompt | It existed only to protect the destructive delete |

## Completion

- Commit/tag: `fix(login): isolate via-agy flow`
- Verification result: `make verify` and `make verify-full` passed; fake-agy tests cover success, cancel, error, and invalid token, and assert the real credential is byte-for-byte unchanged on failure.
- Residual risk: agy starts a fresh temporary home and may initialize more slowly; live browser sign-in remains a manual check.
