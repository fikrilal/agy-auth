# Changelog

All notable changes to this project are documented here.

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
