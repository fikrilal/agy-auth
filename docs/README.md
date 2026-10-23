# Documentation

Start here, then follow the topic-specific source.

## Users and contributors

- [README](../README.md) — install, quick start, commands, updates
- [User guide](user-guide.md) — storage, quota, OAuth, JSON, environment variables
- [Development](development.md) — build, install from source, verification, packaging
- [Contributing](../CONTRIBUTING.md) — setup, pull requests, safe credential testing
- [Security](../SECURITY.md) — reporting, stored credentials, trust model
- [Changelog](../CHANGELOG.md)

## Engineering

- [Harness](harness.md) — verification profiles, custom lints, contracts, hooks
- [Engineering guides](engineering/README.md) — agent workflow, architecture, testing, code standards
- [Execution plans](exec-plans/README.md) — when and how to write a plan
- [ADRs](adr/README.md) — durable architecture decisions
- [Internals](internals.md) — credential storage, registry, switching, OAuth, quota
- [Release process](release.md) — versioning, npm publishing, GitHub release

## Rule of thumb

- A small fix needs the fast harness profile and a clear diff.
- A non-trivial change needs an execution plan and `make verify-full`.
- A decision that changes architecture, security, or public behavior needs an ADR.
