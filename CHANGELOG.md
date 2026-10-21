# Changelog

All notable changes to this project are documented here.

## [Unreleased]

### Added

- Repository harness with custom Go AST lints, secret scanning, credential/registry contract fixtures, doctor, git hooks, and `fast`/`full`/`ci` verification profiles.
- Documentation index, engineering guides, execution-plan scaffold, and ADR process.

### Changed

- CI and release now run the canonical `harness verify --profile ci` gate.
- Commit messages use Conventional Commits and reject bot co-author trailers.
- CLI help is generated from a single command specification.
- Usage errors print `error:`, `Usage:`, and a `hint:` and exit `2`.

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
