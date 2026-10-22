# Changelog

All notable changes to this project are documented here.

## [Unreleased]

### Added

- Repository harness with custom Go AST lints, secret scanning, credential/registry contract fixtures, doctor, git hooks, and `fast`/`full`/`ci` verification profiles.
- Documentation index, engineering guides, execution-plan scaffold, and ADR process.
- Timestamped backups for the live credential and registry, keeping the newest 5 of each with `0600`/`0700` permissions.
- `agy-auth clean [--dry-run]` to prune managed backups and orphan snapshots.
- Per-file import/export reports with `imported`/`updated`/`skipped` outcomes and a summary.
- `agy-auth import --purge` to rebuild `registry.json` from stored snapshots, carrying aliases and active/previous where possible.
- Shared active-first/email ordering and `01`-style row numbers across list, pickers, numeric queries, and JSON list output.
- Width-aware tables that truncate account names and drop lower-priority columns; same-email accounts are grouped under a header with indented, numbered children.

### Changed

- CI and release now run the canonical `harness verify --profile ci` gate.
- Commit messages use Conventional Commits and reject bot co-author trailers.
- CLI help is generated from a single command specification.
- Usage errors print `error:`, `Usage:`, and a `hint:` and exit `2`.
- `--json` responses are versioned envelopes with `schema_version`, `command`, and `data` or `error`; warnings stay on stderr.
- Operation errors use stable codes and hints; raw Go errors and internal paths are no longer shown.
- Ambiguous query errors include candidate accounts.
- `switch --json` and `remove --json` reject interactive selection without a query or selector.
- Query resolution now prefers row number, exact account ID, exact alias, exact email, ID prefix, then substring.
- Ambiguous switches open a candidate picker in a terminal; non-interactive and JSON calls return `ambiguous_query` with candidates.
- Added `switch -` to return to the previously active account; `registry.json` now records `previous`.
- Ambiguous `alias`, `remove`, and `export` selectors now use the same candidate picker as `switch`.
- `remove` resolves and deduplicates all selectors before deleting; snapshot deletion failures return `state_uncertain` without saving registry changes.
- Alias messages distinguish set, already set, updated, and cleared.
- Credential and registry writes are write-if-changed and re-harden permissions.

## [0.1.0] - 2026-10-20

### Added

- Multi-account snapshots for Antigravity CLI (`agy`).
- `list`, `add`, `login`, `switch`, `remove`, `alias`, `current`, `status`, `import`, `export`, `path`.
- Interactive account picker and row/alias/email/ID queries.
- Google OAuth PKCE login with runtime discovery of the Antigravity client from the installed `agy` binary; `--via-agy` fallback.
- Keyring + file-mirror storage with atomic `0600` writes.
- JSON output for machine-readable commands.
- npm distribution with prebuilt Linux, macOS, and Windows binaries for x64 and arm64.
- GitHub Actions CI and release workflows.

[0.1.0]: https://github.com/fikrilal/agy-auth/releases/tag/v0.1.0
