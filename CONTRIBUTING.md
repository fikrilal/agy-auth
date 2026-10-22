# Contributing

Thanks for helping. `agy-auth` is a small Go CLI with an npm distribution layer.

## Setup

- Go 1.24+
- Node.js 18+ for the npm shim/package
- Antigravity CLI installed for manual testing

```shell
git clone https://github.com/fikrilal/agy-auth.git
cd agy-auth
make verify
make build
```

## Harness

`make verify` is the canonical local gate. It runs formatting, custom Go lints,
secret scanning, docs/version checks, contract fixtures, `go vet`, build, and
short tests. `make verify-full` adds race tests and npm packaging; CI runs
`make verify-ci`. There are no skip flags. See [docs/harness.md](docs/harness.md).

`make hooks` installs the pre-commit and commit-msg hooks. Commits use
Conventional Commits, for example `feat(harness): add profiles`.

For non-trivial work, write an execution plan under
[docs/exec-plans/active/](docs/exec-plans/active/) using the
[template](docs/exec-plans/_template.md), and move it to `completed/` when the
change is verified. Add an ADR under [docs/adr/](docs/adr/) when a decision
changes architecture, credentials, public behavior, CI, or release. Plans and
ADRs are review artifacts, not an automated task-authority system.

## Project layout

| Path | Responsibility |
|------|----------------|
| `cmd/agy-auth` | main entrypoint |
| `cmd/harness` | harness CLI entrypoint |
| `internal/agy` | credential JSON, keyring/file backend |
| `internal/registry` | account snapshots, metadata, queries |
| `internal/oauth` | PKCE login, client discovery |
| `internal/backup` | timestamped credential/registry backups and retention |
| `internal/cli` | commands, rendering, picker |
| `internal/harness` | lints, checks, contracts, profiles, doctor |
| `harness/fixtures` | independent credential/registry contracts |
| `.githooks` | opt-in pre-commit and commit-msg hooks |
| `docs/engineering` | architecture, testing, standards, agent workflow |
| `docs/exec-plans` | active/completed plans and template |
| `docs/adr` | architecture decision records |
| `npm/bin` | Node shim for prebuilt binaries |
| `scripts` | cross-compilation |
| `docs` | internals, harness, guides, plans, ADRs, release process |

## Pull requests

- Keep changes focused; add tests for behavior changes.
- Run `make verify`; use `make verify-full` for non-trivial changes.
- Write an execution plan for non-trivial work; add an ADR for durable decisions.
- Do not commit tokens, OAuth client secrets, or real account snapshots.
- Use Conventional Commits (`feat(scope): ...`); do not add co-authored-by or bot trailers.
- Update `README.md`, `docs/internals.md`, or `docs/harness.md` when behavior changes.
- Add a `CHANGELOG.md` entry for user-visible changes.
- If a failure or review comment repeats, add a lint/contract/regression test instead of relying on memory.

## Testing with real credentials

Never point tests at the real keyring. Use a temporary `AGY_AUTH_HOME` and the file backend:

```shell
TMP=$(mktemp -d)
AGY_AUTH_HOME="$TMP/config" \
AGY_AUTH_STORAGE=file \
AGY_AUTH_AGY_TOKEN_FILE="$TMP/token" \
./bin/agy-auth list
```

The only real-keyring test is opt-in and uses a throwaway service name:

```shell
AGY_AUTH_KEYRING_TEST=1 go test ./internal/agy -run TestKeyringRoundTrip -v
```

## Security

See `SECURITY.md`. Do not open public issues for credential leaks, token handling flaws, or auth bypasses.
