# Testing

The test strategy is simple: behavior is tested at the lowest layer that proves it, and no test touches the real keyring or a real account.

## Layers

| Layer | Location | What it proves |
|---|---|---|
| Credential parsing | `internal/agy/token_test.go` | JSON shape, JWT claims, expiry, fingerprints |
| Storage | `internal/agy/storage_test.go` | file backend, permissions, atomic writes, corrupt input |
| Registry | `internal/registry/registry_test.go` | upsert, dedupe, aliases, queries, permissions |
| OAuth | `internal/oauth/oauth_test.go` | PKCE, auth URL, code normalization, token exchange, refresh, discovery |
| Usage | `internal/usage/usage_test.go` | request shape, project/User-Agent, token refresh, grouped summary, full/detail parsing, cache/stale fallback |
| CLI behavior | `internal/cli/cli_test.go` | lifecycle, help parity, JSON output, usage columns, `usage --all` groups, live interval, model-aware auto-switch |
| Contracts | `harness/fixtures` + `harness contracts` | independent credential/registry shapes |
| Keyring | `TestKeyringRoundTrip` (opt-in) | real Secret Service round-trip with a throwaway service |
| TUI/PTY | manual or focused PTY tests | interactive picker behavior and terminal hygiene |

## Isolation

- Use `registry.NewStore(t.TempDir())` and an explicit `agy.Storage{NoKeyring: true, FilePath: ...}`.
- Use `newTestEnv` in `internal/cli` for CLI tests.
- Never call `registry.DefaultStore()` or `agy.DefaultStorage()` in tests.
- Never read or write a real account snapshot.
- The keyring test only runs with `AGY_AUTH_KEYRING_TEST=1` and uses `agy-auth-selftest` / `selftest`, never `gemini` / `antigravity`.

## Harness profiles

```shell
make verify        # short tests, static checks, contracts
make verify-full   # race tests + npm current-target packaging
make verify-ci     # fresh shuffled race tests + all-target npm packaging
```

Use `make verify-full` for changes that touch concurrency, credential storage, registry persistence, or the npm package shape.

## What not to automate

- Real Google OAuth login (requires a browser and a real account).
- Real keyring reads/writes in CI.
- Live quota API calls in CI; `internal/usage` uses `httptest`, and CLI usage tests inject fake providers.
- Live visual assertions that are better reviewed by a human. Real-account `list --active --no-sync`, `usage`, and PTY live checks are manual.
- `login --via-agy` uses a fake `agy` script in tests (success/cancel/failure/invalid-token) and asserts the real credential is unchanged on failure.

## Adding tests

1. Add a focused test next to the behavior.
2. Use a temp directory and the file backend.
3. Assert the failure mode, not only the happy path.
4. For a credential or registry shape change, update the contract fixtures intentionally.
5. Run `make verify-full` and, if packaging changed, `make verify-ci`.

## Related

- [Harness](../harness.md)
- [Architecture](architecture.md)
- [Agent workflow](agent-workflow.md)
