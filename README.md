# agy-auth

[![npm](https://img.shields.io/npm/v/@fikrilal/agy-auth?label=npm)](https://www.npmjs.com/package/@fikrilal/agy-auth)
[![CI](https://github.com/fikrilal/agy-auth/actions/workflows/ci.yml/badge.svg)](https://github.com/fikrilal/agy-auth/actions/workflows/ci.yml)
[![license](https://img.shields.io/github/license/fikrilal/agy-auth)](LICENSE)

`agy-auth` is an unofficial multi-account switcher for [Google Antigravity CLI](https://antigravity.google/docs/cli/overview) (`agy`), modeled on [codex-auth](https://github.com/Loongphy/codex-auth).

Antigravity CLI keeps one credential at a time. `agy-auth` snapshots accounts, switches the live credential, and lets you keep personal, work, or test accounts side by side.

## Features

- Store any number of `agy` accounts.
- Switch by row number, alias, email, or account ID prefix.
- Toggle back to the previous account with `switch -`.
- Candidate picker for ambiguous queries in a terminal.
- Width-aware tables with active-first numbering and same-email grouping.
- Plan and shared 5-hour/weekly quota columns from the Antigravity Code Assist quota API, with a 60-second cache (or the live interval) and non-fatal errors.
- Per-model quota details with `agy-auth usage`, including Gemini buckets that the shared summary does not expose; `usage --all` compares every stored account.
- Live views for list, switch, and remove with viewport paging and a configurable 5–3600 second refresh; `switch --live` can move away from an exhausted shared window or model bucket.
- Skip quota lookups with `list --no-usage`; set the live cadence with `config live --interval`.
- Add the account that is currently signed in (`add`).
- Add another account through Google OAuth with PKCE (`login`) without touching the current credential first.
- Import and export raw credential JSON.
- Machine-readable output with `--json`.
- Timestamped backups before credential and registry changes, with `clean` for retention and orphan cleanup.
- Uses the same credential storage as `agy`: the OS keyring with a file fallback for headless hosts.
- No telemetry. Tokens only leave the machine for Google OAuth, the Google Code Assist quota API, and, if you use `--via-agy`, agy itself.

## Install

### npm (recommended)

The package is published as `@fikrilal/agy-auth` on npm.

```shell
npm install -g @fikrilal/agy-auth
agy-auth version
```

Run without a global install:

```shell
npx @fikrilal/agy-auth list
```

> The unscoped npm name `agy-auth` belongs to an unrelated project. Install the scoped package above, which provides the `agy-auth` command.

For development, install from a local checkout:

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

# Toggle back to the account that was active before the last switch.
agy-auth switch -
```

After switching, restart `agy`. An already-running `agy` process keeps the old credential in memory.

## Commands

| Command | Description |
|---------|-------------|
| `agy-auth list [--active] [--no-sync] [--no-usage] [--live]` | List stored accounts, plan, and quota |
| `agy-auth add [--alias <alias>]` | Save the account currently signed in to agy |
| `agy-auth login [--alias <alias>] [--no-browser] [--via-agy]` | Add an account through Google OAuth |
| `agy-auth switch [<query>|-] [--live]` | Switch interactively, by query, or back to the previous account |
| `agy-auth remove [<query>...] [--all] [--logout] [--yes] [--live]` | Remove stored accounts |
| `agy-auth alias set <query> <alias>` | Set an alias |
| `agy-auth alias clear <query>` | Clear an alias |
| `agy-auth alias list` | List aliases |
| `agy-auth current` | Print the live agy account |
| `agy-auth status` | Show storage paths, token expiry, and keyring/file health |
| `agy-auth usage [<query>] [--all]` | Show or compare per-model quota details |
| `agy-auth config live [--interval <seconds>]` | Show or set the live refresh interval |
| `agy-auth import <file-or-dir> [--alias <alias>] [--activate]` | Import credential JSON with per-file reporting |
| `agy-auth import --purge` | Rebuild `registry.json` from stored account snapshots |
| `agy-auth export [--dir <directory>] [<query>...]` | Export credential JSON with a summary |
| `agy-auth path` | Print storage locations |
| `agy-auth clean [--dry-run]` | Prune managed backups and orphan snapshots |
| `agy-auth version` | Print the version |

Queries resolve by row number, exact account ID, exact alias, exact email, account ID prefix, then email/alias/ID substring.

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
  backups/active-<timestamp>.json
  backups/registry-<timestamp>.json
```

`switch` writes the selected snapshot to both the OS keyring and the file fallback, then updates the registry. `agy` refreshes expired access tokens itself with the stored refresh token.

Before replacing or deleting the live credential, `agy-auth` snapshots the current value under `backups/` (`0600`, keep 5). `registry.json` is backed up when its contents change. `clean` prunes managed backups beyond retention and snapshots not referenced by the registry; if `registry.json` is missing, snapshots are preserved for rebuild.

Queries resolve in this order: row number, exact account ID, exact alias, exact email, account ID prefix, then email/alias/ID substring. An ambiguous query opens a candidate picker in a terminal for `switch`, `alias`, `remove`, and `export`; non-interactive and `--json` calls return `ambiguous_query` with candidates. `switch -` selects the account that was active before the last switch.

Rows are ordered active first, then email. The same `01`-style numbers are used by `list`, pickers, and numeric queries. Same-email accounts share a dim header and only the indented child rows are numbered. Tables adapt to terminal width, truncating the account column and dropping lower-priority columns.

`list --live`, `switch --live`, and `remove --live` open an interactive view with a 1 Hz repaint and a configurable 5–3600 second credential/registry/quota refresh (default 60). `switch --live` switches on Enter and stays open; `remove --live` uses Space to check rows and Enter to remove them. `switch --live` also auto-switches when the active account is at 0% on a fresh 5-hour/weekly window or any fresh model bucket, choosing the healthy stored account with the earliest reset. `--live` requires a terminal and cannot be combined with `--json`.

`list` fetches the current plan and shared quota from the Antigravity Code Assist backend. The quota source uses the project ID cached by agy, an `antigravity` User-Agent, and the account's access token; expired access tokens are refreshed in memory through the same OAuth client used by `login`. Results are cached for 60 seconds (or the live interval while a live view is running). Quota failures are non-fatal: the table shows `-` (or cached values marked `*`) and JSON rows carry `usage_error`. Use `list --no-usage` to skip the lookup.

`agy-auth usage` is the per-model view: it uses `retrieveUserQuota` for the live account, the active stored account, or one query-selected account. It shows each model's remaining percentage and reset time, including Gemini buckets that the shared summary omits. `usage --all` compares all stored accounts by plan, shared windows, lowest model headroom, model, and reset; the full result is cached for the live interval. `config live --interval` stores the live cadence in `registry.json`; `import --purge` preserves it.

`remove` resolves and deduplicates every selector before deleting anything. If a later selector fails, no account is removed.

`import` reports each file as imported, updated, or skipped; skipped rows go to stderr, and every run ends with a summary. `--purge` rebuilds `registry.json` from `accounts/*.json`, carries aliases and active/previous where possible, and uses the live credential to choose the active account when it matches. `export` prints per-file paths plus a summary.

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

`list`, `add`, `login`, `switch`, `remove`, `current`, `status`, `usage`, `config`, `import`, `export`, `path`, and `version` support `--json`. Every response is one versioned envelope on stdout; warnings go to stderr.

Example (`agy-auth list --json`):

```json
{
  "schema_version": 1,
  "command": "list",
  "data": {
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
        "added_at": "2026-10-20T15:58:44Z",
        "plan": "Pro",
        "usage": {
          "five_hour": {
            "remaining_percent": 75,
            "resets_at": "2026-10-22T18:25:48Z"
          },
          "weekly": {
            "remaining_percent": 50,
            "resets_at": "2026-10-29T13:25:48Z"
          },
          "updated_at": "2026-10-22T13:25:47Z"
        }
      }
    ],
    "current": {
      "number": 1,
      "id": "acct_cb5897f277",
      "active": true,
      "stored": true,
      "email": "user@example.com",
      "plan": "Pro",
      "usage": {
        "five_hour": {
          "remaining_percent": 75,
          "resets_at": "2026-10-22T18:25:48Z"
        },
        "weekly": {
          "remaining_percent": 50,
          "resets_at": "2026-10-29T13:25:48Z"
        },
        "updated_at": "2026-10-22T13:25:47Z"
      }
    }
  }
}
```

Usage and operation errors use the same envelope:

```json
{
  "schema_version": 1,
  "command": "list",
  "error": {
    "code": "usage",
    "message": "list takes no arguments",
    "hint": "Run `agy-auth list --help` for help and examples."
  }
}
```

`command` is always the canonical command name, even when an alias is used.

`plan` and `usage` are omitted when quota data is unavailable. `usage_error` carries a short stable code (`missing_auth`, `auth`, `forbidden`, `unsupported`, `timeout`, `network`, `invalid_response`, `refresh_failed`, or `http_<status>`); `usage.stale: true` marks cached values retained after a failed refresh.

`usage --json` returns `account`, `email`, `plan`, `updated_at`, and a `buckets` array with `model_id`, `remaining_percent`, `resets_at`, and `token_type`. `usage --all --json` returns an `accounts` array with the same fields plus `usage`, `lowest`, and `usage_error`. `config live --json` returns `live_interval_seconds`.

Stable error codes include `usage`, `account_not_found`, `ambiguous_query`, `previous_not_found`, `credential_not_found`, `credential_error`, `credential_write_failed`, `backup_error`, `registry_error`, `state_uncertain`, `oauth_error`, `quota_error`, `import_failed`, `invalid_credential`, `export_failed`, `clean_failed`, `confirmation_required`, `confirmation_declined`, `selection_failed`, `input_error`, `agy_not_found`, `state_conflict`, and `output_error`.

`ambiguous_query` errors include a `candidates` array with account IDs, emails, and aliases. Hints are included when there is a concrete recovery step.

`import` and `export` JSON responses include a `files` array with per-file outcomes and a `summary` object. `import` also keeps the `imported` account array, and `export` keeps the `exported` array.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Operation failed (no stored account, corrupt credential, OAuth failure, and similar) |
| `2` | Usage error (unknown command or flag, wrong argument count, missing selector) |

Usage errors print `error:`, the command's `Usage:` block, and a `hint:` on stderr.

## Environment variables

| Variable | Purpose |
|----------|---------|
| `AGY_AUTH_HOME` | Override the registry directory |
| `AGY_AUTH_STORAGE=file` | Disable keyring access and use only the file backend |
| `AGY_AUTH_AGY_TOKEN_FILE` | Override the agy fallback token path |
| `AGY_AUTH_CLIENT_ID` | Override the OAuth client ID |
| `AGY_AUTH_CLIENT_SECRET` | Override the OAuth client secret |
| `AGY_AUTH_PROJECT_ID` | Override the Code Assist project ID used for quota lookups |

## Limitations

- **Restart agy after switching.** Existing sessions do not pick up the new credential.
- `agy-auth` is unofficial and can break when Antigravity changes its credential format or OAuth client.
- Plan, shared quota, and per-model quota display depend on undocumented Code Assist endpoints. If an endpoint or project context changes, the command reports a stable quota error or unavailable row instead of failing the account list; `--no-usage` skips the list lookup entirely.
- Per-model IDs are provider-specific and may change when Antigravity updates its catalog.
- The direct OAuth flow targets personal Google accounts (`auth_method: consumer`). For enterprise or custom sign-in flows, sign in with `agy` first and use `agy-auth add`.
- Account snapshots are protected with `0600` file permissions. The keyring is protected by your OS login keyring.
- On Linux, a locked or unresponsive keyring falls back to the file mirror after a short timeout.

## Packaging and publishing

The npm package is `@fikrilal/agy-auth`. `npm pack` cross-compiles all six
platform binaries into `npm/vendor/` and the Node shim in `npm/bin/agy-auth.js`
dispatches to the right one. The GitHub release workflow publishes the package
using the `NPM_TOKEN` secret.

See [docs/release.md](docs/release.md) for the release process.

## Development

The repository harness is the canonical gate:

```shell
make verify       # fast: static checks, contracts, build, short tests
make verify-full  # race tests + current-target npm packaging
make verify-ci    # CI parity: fresh race tests + all-target npm packaging
make hooks        # opt-in pre-commit and commit-msg hooks
```

`make verify` is enough for most changes. See [docs/harness.md](docs/harness.md) for the rules, contracts, profiles, and commit policy.

Build and packaging commands remain available:

```shell
make build
make npm-build
make npm-pack
```

The keyring integration test is opt-in because it writes to the real OS keyring:

```shell
AGY_AUTH_KEYRING_TEST=1 go test ./internal/agy -run TestKeyringRoundTrip -v
```

It uses a throwaway service name (`agy-auth-selftest`) and never touches the `gemini`/`antigravity` entry.

## Documentation

- [Documentation index](docs/README.md)
- [Harness](docs/harness.md) — verification profiles, custom lints, contracts, hooks
- [Engineering guides](docs/engineering/README.md) — agent workflow, architecture, testing, standards
- [Execution plans](docs/exec-plans/README.md) — non-trivial work plans
- [ADRs](docs/adr/README.md) — architecture decisions
- [Internals](docs/internals.md) — credential storage, registry, switching, OAuth
- [Release process](docs/release.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Disclaimer

This project is not affiliated with Google. Use it at your own risk. By using the default OAuth login you send OAuth requests to Google using the same public client as Antigravity CLI. Review Google's terms and your account policies before using it with a work or enterprise account.
