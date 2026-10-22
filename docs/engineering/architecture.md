# Architecture

`agy-auth` is a small Go CLI with an npm distribution layer. The core invariant is that `agy` owns one active credential; `agy-auth` snapshots that credential and swaps the selected snapshot back into agy's slot.

## Package map

```text
cmd/agy-auth/          CLI entrypoint
cmd/harness/           repository harness entrypoint

internal/cli/          commands, rendering, picker, App IO
internal/agy/          credential JSON, keyring/file backend
internal/registry/     account snapshots, metadata, queries
internal/oauth/        PKCE login, client discovery
internal/backup/       timestamped credential/registry backups

npm/bin/               Node shim for prebuilt binaries
scripts/               cross-compilation
harness/               contract fixtures
docs/                  guides, plans, ADRs, internals
```

## Import direction

```text
cmd/agy-auth -> internal/cli -> internal/{agy,registry,oauth,backup}
internal/registry -> internal/{agy,backup}
internal/oauth    -> internal/agy
internal/agy      -> internal/backup + standard library + go-keyring
internal/backup   -> standard library
internal/harness  -> internal/{agy,registry} (dev tool only)
```

Production packages must not import `internal/harness`. The harness may import `internal/agy` and `internal/registry` for contract checks. The `architecture` lint enforces this graph; a new package requires an explicit policy update.

## Credential flow

```text
agy credential (OS keyring or file)
        │  add / list
        ▼
~/.config/agy-auth/accounts/<id>.json
        │  switch
        ▼
keyring + file mirror -> restart agy
```

- Keyring item: service `gemini`, account `antigravity`.
- File fallback: `~/.gemini/antigravity-cli/antigravity-oauth-token`.
- `internal/agy/storage.go` is the only place that talks to the keyring.
- Credential and registry writes use the atomic temp-file + rename helper with `0600`/`0700` permissions.
- `internal/agy` and `internal/registry` snapshot the previous value before changing it; `internal/backup` owns timestamped names, retention, and pruning.
- Account identity is the Google `sub` claim; the refresh-token fingerprint is the fallback.

See [Internals](../internals.md) for the full document shapes, registry schema, switching rules, and OAuth discovery.

## CLI

`internal/cli/app.go` owns command dispatch, global flags, and the `App` writers. `cmd/agy-auth/main.go` only wires standard streams and exits. Library packages return errors; only `App` renders user-facing output.

The picker and rendering live in `internal/cli/picker.go`, `table.go`, `render.go`, and `util.go`. `live.go` owns the raw-mode live views, refresh/generation state, and switch/remove actions. Account rows are ordered by `registry.SortAccounts` during load (active first, then email), and both list and picker share the same 1-based numbers. The width-aware table truncates the account column and drops lower-priority columns; keep future TUI work on the same row model and selection by account ID.

## Packaging

`scripts/build-npm.mjs` cross-compiles six binaries into `npm/vendor/`. `npm/bin/agy-auth.js` dispatches by platform/arch. `package.json` is the version source; the build injects it into the Go binary with ldflags.

## Related

- [Internals](../internals.md)
- [Harness](../harness.md)
- [Testing](testing.md)
- [Agent workflow](agent-workflow.md)
