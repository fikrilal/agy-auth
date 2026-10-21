# AGENTS.md

`agy-auth` is a Go CLI that manages multiple Google Antigravity CLI (`agy`) accounts by snapshotting the single credential agy stores in the OS keyring/file and swapping it. It also ships as an npm package with prebuilt binaries.

## Read order

1. `README.md` — user-facing behavior.
2. `docs/README.md` — documentation index.
3. `docs/harness.md` — repository guardrails, profiles, contracts, hooks.
4. `docs/engineering/agent-workflow.md` — planning, verification, and commit workflow.
5. `docs/internals.md` — credential storage, registry, switching, OAuth.
6. `internal/agy` — credential JSON and keyring/file backend.
7. `internal/registry` — snapshots, IDs, aliases, queries.
8. `internal/oauth` — PKCE login and client discovery.
9. `internal/cli` — commands, picker, rendering.

## Harness

- Default local gate: `make verify` (`harness verify --profile fast`).
- Non-trivial changes: `make verify-full`.
- CI runs `make verify-ci`; do not duplicate its step list.
- No skip flags. Do not claim a check passed unless it ran.
- If the same failure or review comment repeats, add a lint/contract/regression test and update `docs/harness.md`.
- `make hooks` installs the pre-commit and commit-msg hooks.
- Production packages must not import `internal/harness`.
- For non-trivial work, write an execution plan under `docs/exec-plans/active/` using the template; move it to `completed/` when done.
- Execution plans are review artifacts, not loop-engineering authority. There is no task controller, path enforcement, repair loop, or handoff automation.

## Decisions

- Add an ADR under `docs/adr/` when a decision changes architecture, credential handling, public CLI behavior, CI, or release.
- Do not rewrite accepted ADRs; add a new one and mark the old one superseded.

## Invariants

- agy keyring item: service `gemini`, account `antigravity`.
- File fallback: `~/.gemini/antigravity-cli/antigravity-oauth-token`.
- Credential JSON: `token{access_token,token_type,refresh_token,expiry}`, `auth_method`, `id_token`. Preserve unknown fields by writing snapshots raw.
- Registry: `~/.config/agy-auth/registry.json` plus `accounts/<id>.json`; directories `0700`, files `0600`.
- Account IDs are a stable hash of the Google `sub` claim; fall back to the refresh-token fingerprint.
- Never commit the Antigravity OAuth client ID or secret. Only SHA-256 fingerprints live in `internal/oauth/oauth.go`.
- Never log or print tokens or the OAuth client secret.
- Writes must be atomic (temp file + rename). Tests must never touch the real keyring.

## Build and test

```shell
make build
make verify
make verify-full
make verify-ci
AGY_AUTH_KEYRING_TEST=1 go test ./internal/agy -run TestKeyringRoundTrip -v
make npm-build
make npm-pack
```

Use a temporary `AGY_AUTH_HOME` and `AGY_AUTH_STORAGE=file` for manual CLI testing.

## Adding or changing commands

1. Add the command in `internal/cli/cmd_*.go`.
2. Register it in `Run` in `internal/cli/app.go`.
3. Add help text in `internal/cli/help.go`.
4. Support `--json` when the output is machine-readable.
5. Add tests in the same package (see `internal/cli/cli_test.go`).

## Storage changes

`internal/agy/storage.go` is the only place that talks to the keyring/file backend. Keep the current behavior: reads try the keyring first, then the file; writes go to both when available.

## OAuth changes

- Direct login uses PKCE `S256` and the public Antigravity client discovered from the installed `agy` binary.
- If Google rotates the client, update the fingerprint constants. See `docs/internals.md` → "Updating OAuth client fingerprints".
- `--via-agy` is the fallback that drives agy's own sign-in.

## Versioning and release

- `package.json` is the version source. `scripts/build-npm.mjs` injects it into the Go binary with ldflags.
- See `docs/release.md`.
- The release workflow runs `harness verify --profile ci`.
- A `vX.Y.Z` tag builds all six binaries, publishes npm when `NPM_TOKEN` is set, and creates a GitHub release.

## Git

- Use Conventional Commits: `type(scope): summary`, for example `feat(harness): add verification profiles`. Scopes are lowercase. The `commit-msg` hook enforces this.
- The user is the only author. Never add `Co-authored-by` trailers, bot attributions, or `CommandCodeBot` lines to commits.
- Only commit when asked.
