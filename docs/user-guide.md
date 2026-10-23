# User guide

This page covers behavior beyond the [README](../README.md): storage, quota views, OAuth, JSON output, environment variables, and limitations.

## Storage and switching

`agy-auth` uses the same credential storage as Antigravity CLI:

- The active credential is written to the OS keyring (`gemini` / `antigravity`) and the file mirror at `~/.gemini/antigravity-cli/antigravity-oauth-token`.
- Stored accounts live under `~/.config/agy-auth/`; use `agy-auth path` to print the exact locations.

```
~/.config/agy-auth/
  encryption.key
  registry.json
  accounts/<account-id>.json
  backups/active-<timestamp>.json
  backups/registry-<timestamp>.json
  update-check.json
```

`accounts/*.json` and `backups/active-*.json` are encrypted with AES-256-GCM (`agy-auth-encrypted:v1:`). `encryption.key` is the separate 32-byte key file (`0600`, directory `0700`). Back up `encryption.key` separately; losing it makes the snapshots unrecoverable. Registry backups contain metadata only and remain plaintext.

Before replacing or deleting the live credential, `agy-auth` snapshots the current value under `backups/` (`0600`, keep 5). `registry.json` is backed up when its contents change. `clean` prunes managed backups beyond retention and snapshots not referenced by the registry; if `registry.json` is missing, snapshots are preserved for rebuild.

`switch` writes the selected snapshot to both the OS keyring and the file fallback, then updates the registry. `agy` refreshes expired access tokens itself with the stored refresh token. Restart `agy` after switching; running sessions keep the old credential.

`export` decrypts selected snapshots and writes plaintext credential JSON to the destination. Treat exported files as secrets and delete them after use.

## Queries and tables

Queries resolve in this order: row number, exact account ID, exact alias, exact email, account ID prefix, then email/alias/ID substring. An ambiguous query opens a candidate picker in a terminal; non-interactive and `--json` calls return `ambiguous_query` with candidates. `switch -` selects the account that was active before the last switch.

Rows are ordered active first, then email. The same `01`-style numbers are used by `list`, pickers, and numeric queries. Same-email accounts share a dim header and only the indented child rows are numbered. Tables adapt to terminal width, truncating the account column and dropping lower-priority columns.

Account tables show `PLAN`, `5H`, `WEEKLY`, `GROUP`, `5H RESET`, and `WEEKLY RESET` when quota data is available. Token expiry, auth method, and last-used date are JSON-only. The `GROUP` column identifies the quota group owning the lower of the displayed `5H`/`WEEKLY` values; `agy-auth usage` shows every group explicitly.

## Quota and live views

`list` fetches the current plan and grouped quota summary from the Antigravity Code Assist backend. The quota source uses the project ID cached by `agy`, an `antigravity/cli` User-Agent, and the account's access token; expired access tokens are refreshed in memory through the same OAuth client used by `login`. Results are cached for 60 seconds (or the live interval while a live view is running). Quota failures are non-fatal: the table shows `-` (or cached values marked `*`) and JSON rows carry `usage_error`. Use `list --no-usage` to skip the lookup.

`agy-auth usage` shows the same grouped quota data as `agy /usage`: `Gemini Models`, `Claude and GPT models`, and any tier-specific group such as Ultra. Each group row has its 5-hour and weekly remaining percentages and reset times. `usage --all` compares every stored account, with a blank line between account blocks; a query selects one account.

`list --live`, `switch --live`, and `remove --live` open an interactive view with a 1 Hz repaint and a configurable 5–3600 second refresh (default 60). Arrow/SS3/CSI-u keys, `j`/`k`, PgUp/PgDn, Home/End, and `0-9` row jumps are supported; lone Esc is distinguished from escape sequences. The frame repaints incrementally and reflows on terminal resize.

`switch --live` switches on Enter and stays open; `remove --live` uses Space to check rows and Enter to remove them. `switch --live` also auto-switches when any fresh named quota group is at 0%, choosing the healthy stored account with the earliest reset. If group data is unavailable, it falls back to the shared summary and model detail. `--live` requires a terminal and cannot be combined with `--json`.

## OAuth login

`agy-auth login` uses the same public OAuth client that Antigravity CLI uses. The client ID and secret are discovered from your installed `agy` binary at runtime; this repository does not embed them. If discovery fails, set `AGY_AUTH_CLIENT_ID` and `AGY_AUTH_CLIENT_SECRET`, or use `--via-agy`. The command prints an authorization URL (and opens a browser when possible), then asks for the authorization code shown at `antigravity.google/oauth-callback`.

The flow uses PKCE (`S256`), `access_type=offline`, and `prompt=consent` so Google returns a refresh token. The account is added and activated without deleting the current credential first.

If you prefer to let `agy` handle sign-in:

```shell
agy-auth login --via-agy
```

This saves the current account, runs `agy` in an isolated temporary home with file-based credential storage, and imports the resulting credential only after sign-in succeeds. Your active credential is left untouched if you cancel or sign-in fails.

## Updates

After `list`, `switch`, `status`, or `usage`, `agy-auth` checks the published npm version at most once every 24 hours. In an interactive terminal it asks `Update now? [y/N]`; non-interactive runs print the update commands. Live views and `--json` do not check. Failures are silent.

- npm installs update through `npm install -g @fikrilal/agy-auth@latest`.
- Standalone Linux/macOS binaries download the matching GitHub release asset and verify its SHA-256 checksum before replacing the executable.
- Windows standalone installs and unsupported platforms print the npm/Go commands.
- Declining suppresses the prompt for that published version.
- Checksums protect against corrupted downloads, not a compromised GitHub release; npm installs also use npm integrity metadata.

`agy-auth version --check` forces an immediate check without applying an update. The check only fetches `@fikrilal/agy-auth` metadata from `registry.npmjs.org`; it sends no account or token data. The result is cached in `update-check.json` (`0600`). Disable automatic checks with `AGY_AUTH_NO_UPDATE_CHECK=1`.

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
      "plan": "Pro"
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

`command` is always the canonical command name, even when an alias is used. `plan` and `usage` are omitted when quota data is unavailable. `usage_error` carries a short stable code (`missing_auth`, `auth`, `forbidden`, `unsupported`, `timeout`, `network`, `invalid_response`, `refresh_failed`, or `http_<status>`); `usage.stale: true` marks cached values retained after a failed refresh. Each `five_hour`/`weekly` window may include a `group` field naming the quota group that supplied the limiting value.

`usage --json` returns `account`, `email`, `updated_at`, and a `groups` array. Each group has a `name`, `description`, and `buckets`; each bucket has `id`, `name`, `window` (`5h`/`weekly`), `remaining_fraction`, `remaining_percent`, and `reset_time`. `usage --all --json` returns an `accounts` array with the same group data plus `usage_error`. `config live --json` returns `live_interval_seconds`.

Stable error codes include `usage`, `account_not_found`, `ambiguous_query`, `previous_not_found`, `credential_not_found`, `credential_error`, `credential_write_failed`, `backup_error`, `registry_error`, `state_uncertain`, `oauth_error`, `quota_error`, `import_failed`, `invalid_credential`, `export_failed`, `clean_failed`, `confirmation_required`, `confirmation_declined`, `selection_failed`, `input_error`, `agy_not_found`, `state_conflict`, `update_check_failed`, and `output_error`.

`ambiguous_query` errors include a `candidates` array with account IDs, emails, and aliases. Hints are included when there is a concrete recovery step. `import` and `export` JSON responses include a `files` array with per-file outcomes and a `summary` object; `import` keeps the `imported` account array, and `export` keeps the `exported` array.

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
| `AGY_AUTH_NO_UPDATE_CHECK=1` | Disable automatic update checks |

## Limitations

- **Restart agy after switching.** Existing sessions do not pick up the new credential.
- `agy-auth` is unofficial and can break when Antigravity changes its credential format or OAuth client.
- Plan and quota display depend on undocumented Code Assist endpoints. If an endpoint or project context changes, the command reports a stable quota error or unavailable row instead of failing the account list; `--no-usage` skips the lookup.
- Per-model IDs are provider-specific and may change when Antigravity updates its catalog.
- The direct OAuth flow targets personal Google accounts (`auth_method: consumer`). For enterprise or custom sign-in flows, sign in with `agy` first and use `agy-auth add`.
- Account snapshots and active-credential backups are encrypted with a local key file, not with your OS login keyring. Protect `encryption.key` and back it up separately; losing it makes the snapshots unrecoverable.
- Exported credential files are intentionally plaintext. Delete them after use.
- The keyring is protected by your OS login keyring. On Linux, a locked or unresponsive keyring falls back to the file mirror after a short timeout.

See [internals](internals.md) for implementation details and [security](../SECURITY.md) for the trust model.
