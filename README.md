# agy-auth

`agy-auth` is an unofficial multi-account switcher for [Google Antigravity CLI](https://antigravity.google/docs/cli/overview) (`agy`), modeled on [codex-auth](https://github.com/Loongphy/codex-auth).

Antigravity CLI keeps one credential at a time. `agy-auth` snapshots accounts, switches the live credential, and lets you keep personal, work, or test accounts side by side.

## Features

- Store any number of `agy` accounts.
- Switch by row number, alias, email, or account ID prefix.
- Add the account that is currently signed in (`add`).
- Add another account through Google OAuth with PKCE (`login`) without touching the current credential first.
- Import and export raw credential JSON.
- Machine-readable output with `--json`.
- Uses the same credential storage as `agy`: the OS keyring with a file fallback for headless hosts.
- No telemetry. Tokens only leave the machine for Google OAuth and, if you use `--via-agy`, agy itself.

## Install

### npm (recommended)

```shell
npm install -g @fikrilal/agy-auth
agy-auth version
```

Run without a global install:

```shell
npx @fikrilal/agy-auth list
```

> The unscoped npm name `agy-auth` belongs to an unrelated project. Install the scoped package above, which provides the `agy-auth` command.

Until the package is published, install directly from this repository:

```shell
git clone https://github.com/fikrilal/agy-auth.git
cd agy-auth
make npm-build
npm install -g .
```

### Go

```shell
go install github.com/fikrilal/agy-auth/cmd/agy-auth@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

### From source

```shell
git clone https://github.com/fikrilal/agy-auth.git
cd agy-auth
make install          # installs ~/.local/bin/agy-auth
# or: make build      # builds ./bin/agy-auth
```

## Requirements

- Antigravity CLI installed and signed in for `add`/`switch`.
- Linux, macOS, or Windows. Linux needs a Secret Service provider (GNOME Keyring, KWallet, etc.) for keyring access; otherwise the file fallback is used.
- The npm package bundles prebuilt binaries for Linux, macOS, and Windows on x64 and arm64, so Node.js 18+ is enough. Building from source needs Go 1.24+.

## Quick start

```shell
# Sign in with agy as usual.
agy

# Save the account that is currently signed in.
agy-auth add --alias personal

# Add a second account through Google OAuth.
agy-auth login --alias work

# Inspect and switch.
agy-auth list
agy-auth switch work
```

After switching, restart `agy`. An already-running `agy` process keeps the old credential in memory.

## Commands

| Command | Description |
|---------|-------------|
| `agy-auth list [--active] [--no-sync]` | List stored accounts and the live account |
| `agy-auth add [--alias <alias>]` | Save the account currently signed in to agy |
| `agy-auth login [--alias <alias>] [--no-browser] [--via-agy]` | Add an account through Google OAuth |
| `agy-auth switch [<query>]` | Switch interactively or by query |
| `agy-auth remove [<query>...] [--all] [--logout] [--yes]` | Remove stored accounts |
| `agy-auth alias set <query> <alias>` | Set an alias |
| `agy-auth alias clear <query>` | Clear an alias |
| `agy-auth alias list` | List aliases |
| `agy-auth current` | Print the live agy account |
| `agy-auth status` | Show storage paths, token expiry, and keyring/file health |
| `agy-auth import <file-or-dir> [--alias <alias>] [--activate]` | Import credential JSON |
| `agy-auth export [--dir <directory>] [<query>...]` | Export credential JSON |
| `agy-auth path` | Print storage locations |
| `agy-auth version` | Print the version |

Queries accept a row number from `list`, an alias, an exact email, an email substring, or an account ID prefix.

### Global options

- `--json` — machine-readable output.
- `--no-color` — disable ANSI colors.

## How it works

Antigravity CLI stores a JSON document in the OS keyring under service `gemini`, account `antigravity`. On hosts without a usable keyring, it uses:

```
~/.gemini/antigravity-cli/antigravity-oauth-token
```

The document looks like this:

```json
{
  "token": {
    "access_token": "...",
    "token_type": "Bearer",
    "refresh_token": "...",
    "expiry": "2026-10-20T23:02:46.46381726+07:00"
  },
  "auth_method": "consumer",
  "id_token": "eyJ..."
}
```

`agy-auth` keeps snapshots and metadata in:

```
~/.config/agy-auth/
  registry.json
  accounts/<account-id>.json
```

`switch` writes the selected snapshot to both the OS keyring and the file fallback, then updates the registry. `agy` refreshes expired access tokens itself with the stored refresh token.

See [docs/internals.md](docs/internals.md) for the exact storage and OAuth details.

## OAuth login

`agy-auth login` uses the same public OAuth client that Antigravity CLI uses. The client ID and secret are discovered from your installed `agy` binary at runtime; this repository does not embed them. If discovery fails, set `AGY_AUTH_CLIENT_ID` and `AGY_AUTH_CLIENT_SECRET`, or use `--via-agy`. The command prints an authorization URL (and opens a browser when possible), then asks for the authorization code shown at `antigravity.google/oauth-callback`.

The flow uses PKCE (`S256`), `access_type=offline`, and `prompt=consent` so Google returns a refresh token. The account is added and activated without deleting the current credential first.

If you prefer to let `agy` handle sign-in:

```shell
agy-auth login --via-agy
```

This saves the current account, clears the live credential, launches `agy`, and captures the new credential after agy exits.

## JSON output

`list`, `add`, `login`, `switch`, `remove`, `current`, `status`, `import`, `export`, `path`, and `version` support `--json`.

Example:

```json
{
  "accounts": [
    {
      "number": 1,
      "id": "acct_cb5897f277",
      "active": true,
      "stored": true,
      "email": "user@example.com",
      "alias": "personal",
      "auth_method": "consumer",
      "expires_at": "2026-10-20T23:02:46.46381726+07:00",
      "expired": false,
      "added_at": "2026-10-20T15:58:44Z"
    }
  ],
  "current": {
    "number": 1,
    "id": "acct_cb5897f277",
    "active": true,
    "stored": true,
    "email": "user@example.com"
  }
}
```

## Environment variables

| Variable | Purpose |
|----------|---------|
| `AGY_AUTH_HOME` | Override the registry directory |
| `AGY_AUTH_STORAGE=file` | Disable keyring access and use only the file backend |
| `AGY_AUTH_AGY_TOKEN_FILE` | Override the agy fallback token path |
| `AGY_AUTH_CLIENT_ID` | Override the OAuth client ID |
| `AGY_AUTH_CLIENT_SECRET` | Override the OAuth client secret |

## Limitations

- **Restart agy after switching.** Existing sessions do not pick up the new credential.
- `agy-auth` is unofficial and can break when Antigravity changes its credential format or OAuth client.
- Usage/quota display is not implemented. The quota endpoints require additional account context that is not reliably reproducible from the stored credential.
- The direct OAuth flow targets personal Google accounts (`auth_method: consumer`). For enterprise or custom sign-in flows, sign in with `agy` first and use `agy-auth add`.
- Account snapshots are protected with `0600` file permissions. The keyring is protected by your OS login keyring.
- On Linux, a locked or unresponsive keyring falls back to the file mirror after a short timeout.

## Packaging and publishing

The npm package is `@fikrilal/agy-auth`. `npm pack` cross-compiles all six
platform binaries into `npm/vendor/` and the Node shim in `npm/bin/agy-auth.js`
dispatches to the right one. The GitHub release workflow publishes the package
when an `NPM_TOKEN` secret is configured.

See [docs/release.md](docs/release.md) for the release process.

## Development

```shell
make test
make vet
make build
make npm-build
make npm-pack
```

The keyring integration test is opt-in because it writes to the real OS keyring:

```shell
AGY_AUTH_KEYRING_TEST=1 go test ./internal/agy -run TestKeyringRoundTrip -v
```

It uses a throwaway service name (`agy-auth-selftest`) and never touches the `gemini`/`antigravity` entry.

## Disclaimer

This project is not affiliated with Google. Use it at your own risk. By using the default OAuth login you send OAuth requests to Google using the same public client as Antigravity CLI. Review Google's terms and your account policies before using it with a work or enterprise account.
