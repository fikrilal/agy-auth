# Contributing

Thanks for helping. `agy-auth` is a small Go CLI with an npm distribution layer.

## Setup

- Go 1.24+
- Node.js 18+ for the npm shim/package
- Antigravity CLI installed for manual testing

```shell
git clone https://github.com/fikrilal/agy-auth.git
cd agy-auth
go test ./...
go vet ./...
make build
```

## Project layout

| Path | Responsibility |
|------|----------------|
| `cmd/agy-auth` | main entrypoint |
| `internal/agy` | credential JSON, keyring/file backend |
| `internal/registry` | account snapshots, metadata, queries |
| `internal/oauth` | PKCE login, client discovery |
| `internal/cli` | commands, rendering, picker |
| `npm/bin` | Node shim for prebuilt binaries |
| `scripts` | cross-compilation |
| `docs` | internals and release process |

## Pull requests

- Keep changes focused; add tests for behavior changes.
- Run `go test ./...`, `go vet ./...`, and `go test -race ./...`.
- Do not commit tokens, OAuth client secrets, or real account snapshots.
- Do not add co-authored-by or bot trailers; the contributor is the author.
- Update `README.md` or `docs/internals.md` when behavior changes.
- Add a `CHANGELOG.md` entry for user-visible changes.

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
