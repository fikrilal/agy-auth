# Configurable live refresh interval

| Field | Value |
|---|---|
| Status | completed |
| Owner | fikrilal |
| Date | 2026-10-22 |
| Related | Phase 2 deferred item; [Internals](../../internals.md); [Usage quota plan](../completed/2026-10-22_usage-quota.md) |

## Objective

Make the live-view refresh interval configurable from 5 to 3600 seconds, persisted in `registry.json`, and use the same interval for the quota cache so the two loops stay aligned.

## Context

`list --live`, `switch --live`, and `remove --live` currently hard-code a 60-second refresh. The quota cache is also fixed at 60 seconds. Users with large account lists or slow networks may want a longer interval; users watching a near-exhausted account may want a shorter one.

## Scope

### In scope

- `config live` and `config live --interval <seconds>` with a 5–3600 second range.
- `registry.json.config.live_interval_seconds`, defaulting to 60 when absent or invalid.
- Preserve the config through `import --purge`.
- Apply the configured interval to live refresh scheduling and the usage cache TTL.
- JSON output for get/set.
- Tests for defaults, persistence, validation, JSON, purge preservation, and cache TTL.

### Out of scope

- Per-command intervals or environment overrides.
- Changing the fixed 1 Hz repaint.
- Auto-switch behavior changes.

## Expected paths

- `internal/registry/registry.go`
- `internal/cli/{app,cmd_config,cmd_import,live,spec}.go`
- `internal/cli/cli_test.go`
- `internal/usage/usage.go`
- `internal/usage/usage_test.go`
- `README.md`, `CHANGELOG.md`, `docs/internals.md`

## Risks

- An invalid value read from disk must not destabilize live views; fall back to 60.
- A very short interval increases quota API traffic; the range floor is 5 seconds.
- Existing registries have no `config` object; the field is additive and optional.

## Acceptance criteria

- [x] `config live` prints the effective interval.
- [x] `config live --interval 15` persists and is used by live views.
- [x] Values below 5 or above 3600 are usage errors and do not persist.
- [x] JSON get/set returns `live_interval_seconds`.
- [x] `import --purge` preserves the configured interval.
- [x] The usage cache TTL follows the live interval.
- [x] `make verify` and `make verify-full` pass.

## Verification plan

```shell
make verify
make verify-full
```

Additional checks:

- Set 15 seconds, start a PTY live view, and confirm the first status line says `Refresh in 15s`.
- `import --purge` after setting a value and reload the registry.

## Rollback

Revert the slice. The new registry field is optional; older binaries ignore it. No credential or snapshot changes occur.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-22 | Store seconds as an integer | Keeps `registry.json` readable and avoids duration string parsing |
| 2026-10-22 | Fall back to 60 on invalid stored values | A corrupt setting must not break live views |
| 2026-10-22 | Align the usage cache TTL with the interval | Avoids stale quota data when the user chooses a shorter cadence |

## Completion

- Commit/tag: `feat(cli): add live interval and quota detail`
- Verification result: `make verify` and `make verify-full` passed; a throwaway-home config test returned 60, persisted 15, and a PTY live view showed `Refresh in 14s`.
- Residual risk: values are per-registry and do not use an environment override; short intervals increase quota API traffic.
