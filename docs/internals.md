# agy-auth internals

This document records the behavior `agy-auth` relies on so changes can be made without re-discovering them.

## Credential storage

Antigravity CLI uses `github.com/zalando/go-keyring` with:

- service: `gemini`
- user/account: `antigravity`
- schema: `org.freedesktop.Secret.Generic` (Linux)
- label: `Password for 'antigravity' on 'gemini'` (Linux)

On Linux this is an item in the `login` collection of the Secret Service. On macOS it is a Keychain generic password; on Windows it is a Credential Manager entry.

When the keyring is unavailable, agy falls back to a plain file:

```
$HOME/.gemini/antigravity-cli/antigravity-oauth-token
```

`agy` may also write a keyring timeout marker after a keyring failure and keep using the file for a period. `agy-auth` writes both stores on `switch` and `login` so either path carries the selected account.

## Credential document

Both stores contain the same JSON document:

```json
{
  "token": {
    "access_token": "...",
    "token_type": "Bearer",
    "refresh_token": "...",
    "expiry": "RFC3339Nano timestamp"
  },
  "auth_method": "consumer",
  "id_token": "..."
}
```

`id_token` is an OIDC JWT. `agy-auth` decodes (but does not verify) the payload to read `email` and `sub`. The `sub` value is the stable account identity used for deduplication; the refresh token hash is the fallback fingerprint.

`auth_method` is `consumer` for personal accounts. Additional values may exist for enterprise and other flows.

## Flow

```text
agy credential (keyring or file)
        │  add / list
        ▼
~/.config/agy-auth/accounts/<id>.json
        │  switch
        ▼
keyring + file mirror → restart agy
```

The registry is only an index; the account snapshots are the source of truth.

## Registry

Default directory: `os.UserConfigDir()/agy-auth` (for example `~/.config/agy-auth` on Linux). Override with `AGY_AUTH_HOME`.

```
encryption.key
registry.json
accounts/<id>.json
backups/active-<timestamp>.json
backups/registry-<timestamp>.json
```

`registry.json`:

```json
{
  "version": 2,
  "active": "acct_...",
  "previous": "acct_...",
  "config": {
    "live_interval_seconds": 30,
    "snapshot_encryption": "aes-gcm-v1"
  },
  "accounts": [
    {
      "id": "acct_...",
      "email": "user@example.com",
      "sub": "...",
      "alias": "personal",
      "auth_method": "consumer",
      "fingerprint": "...",
      "added_at": "2026-10-20T15:58:44Z",
      "last_used_at": "2026-10-20T15:58:44Z"
    }
  ]
}
```

Account IDs are the first 10 hex characters of SHA-256 over the Google subject (or the refresh-token fingerprint when no ID token is available). Snapshot files and active-credential backups are AES-256-GCM ciphertext with the `agy-auth-encrypted:v1:` prefix; the key is `encryption.key` (`0600`), generated on first write and guarded by an exclusive `.lock` directory during creation. Registry backups contain only metadata and are not encrypted. Snapshot/backup files are `0600`; the registry is `0600`; directories are `0700`. Legacy plaintext snapshots are encrypted on first read, and legacy active backups are encrypted on the next registry load.

The registry's `active` field is a hint. `list` and `status` treat the live credential as authoritative and match it against snapshots by subject first, then fingerprint, then a unique email.

`previous` records the account that was active before the last switch (including `add`, `login`, and `import --activate`). `switch -` uses it; it is cleared when that account is removed, when the registry no longer contains it, or when it equals `active`.

`config.live_interval_seconds` stores the live-view cadence. It is optional; missing or invalid values use 60 seconds. `config live --interval` accepts 5–3600 seconds, and `import --purge` carries the config forward. `config.snapshot_encryption` records the at-rest format (`aes-gcm-v1`). Registry version 2 is the current schema: `migrateRegistry` applies ordered migrations on load, persists the upgrade, and rejects newer versions. Version 1 files migrate by adding the encryption marker and encrypting existing active backups.

## Display and numbering

`Store.Load` sorts accounts active first, then email ascending, with alias and ID as tiebreaks. This is the order used by `list`, pickers, and numeric queries, so `01` always refers to the same row. Same-email accounts are contiguous: the UI prints a dim email header and indented alias/short-ID children; only the child rows are numbered and selectable.

The account table uses `#`, `ACCOUNT`, and, when at least one row has plan or quota data, `PLAN`, `5H`, `WEEKLY`, `GROUP`, `5H RESET`, and `WEEKLY RESET`. It no longer renders token expiry, auth method, or last-used date; those remain in JSON. `switch` selects through a usage-aware picker (`chooseAccountWithUsage`/`resolveOneWithUsage`) so its interactive and numbered selectors show the same quota columns as `list`; alias/remove/export pickers remain account-only. In interactive pickers and live views, the selected account is marked with a green `>` cursor prefix, its account label is highlighted in cyan, and unselected rows and headers are padded to maintain table alignment. `GROUP` names the group that owns the lower of the displayed `5H`/`WEEKLY` values. The table fits the terminal width by truncating the account label and dropping lower-priority columns on narrow terminals. Cached quota values kept after a failed refresh are marked with `*`.

## Usage and quota

`internal/usage` reads plan and shared quota from the Code Assist backend:

- `POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`
- `POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`

Both requests send the account access token and a project ID. The summary response supplies named groups such as `Gemini Models` and `Claude and GPT models`, each with `5h` and `weekly` buckets. `list` collapses the groups to the lowest remaining fraction for each window; `agy-auth usage` renders the groups individually, matching `agy /usage`.

The backend expects the CLI User-Agent family. `antigravity/cli/<version>` returns all quota groups; a generic `antigravity/agy-auth/...` client receives only the Claude/GPT group. `usage.DefaultUserAgent` therefore uses `antigravity/cli/1.4.5 (agy-auth/<version>; os_type=<goos>; arch=<goarch>; auth_method=consumer)`.

Project lookup order:

1. `AGY_AUTH_PROJECT_ID`
2. `ANTIGRAVITY_PROJECT_ID`
3. `GEMINI_DEFAULT_PROJECT`
4. `~/.gemini/antigravity-cli/cache/default_project_id.txt`
5. `default-cli-project`

Plan labels use `paidTier` first, then `currentTier`: `free-tier` → Free, `standard-tier` → Standard, `g1-pro-tier` → Pro, `g1-ultra-tier` → Ultra; unknown IDs fall back to the tier name.

If the access token is expired, `internal/usage` refreshes it through `internal/oauth` using the refresh token and the runtime-discovered Antigravity client. The refreshed token is used in memory only; snapshots are not rewritten. The provider caches successful snapshots for 60 seconds by default, or the configured live interval while a live view is running. On a later failure, the cached value is returned with `stale: true` and a short `usage_error` code.

`list` enriches rows concurrently with a bounded worker count and a 30-second overall budget. A quota failure never fails the command. `list --no-usage` skips the provider entirely.

`agy-auth usage` renders the grouped summary for the live account, the active stored account, or one query-selected snapshot. Each group has a name, description, and `5h`/`weekly` buckets with remaining fraction and reset time; JSON returns the account/email, updated time, and groups. `usage --all` compares the same group rows for every stored account. Ultra/tier-specific groups are displayed automatically when the backend returns them. The per-model `retrieveUserQuota` endpoint is still used by the live auto-switch path, not by the `usage` display.

## Live views

`list --live`, `switch --live`, and `remove --live` use a raw-mode, alternate-screen controller. It repaints at 1 Hz, refreshes the live credential and registry on the configured 5–3600 second cadence in the background, and uses a generation counter so an in-flight refresh cannot overwrite a newer switch/remove action.

Input goes through a shared key decoder (`internal/cli/keys.go`): plain runes, control keys, arrows/SS3, CSI sequences with modifiers, CSI-u functional keys, PgUp/PgDn, Home/End, and a 50 ms lone-Esc timeout. Digits select rows by their global `01`-style number, with an 800 ms type-ahead window. Unknown CSI/mouse sequences are ignored instead of canceling the view. The frame is built to a line buffer and painted incrementally with absolute cursor writes, clearing stale tails; it reflows when the terminal size changes. The first frame is built from local credential/registry data, then an asynchronous refresh adds quota data without blocking keys. The viewport keeps the selected row visible, pages with PgUp/PgDn, and preserves selection by account ID across refreshes. `switch --live` switches on Enter and stays open; `remove --live` toggles checkboxes with Space and removes checked accounts on Enter. `switch --live` also auto-switches once per session when any fresh named quota group is at 0% on a 5-hour or weekly bucket; it chooses a healthy stored account with the earliest reset. The shared summary and per-model detail are fallbacks only when group data is missing or stale. Stale data is never used for auto-switch. The status line reports the live source (`keyring`/`file`), the next refresh, and a quota-error count; the action line reports the last switch/remove result. The refresh cadence comes from `config.live_interval_seconds` (5–3600 seconds, default 60) and is also applied to the usage cache TTL. `--live` requires a terminal and is rejected with `--json`.

## Update checks

`internal/cli/update.go` compares `Version` with the latest `@fikrilal/agy-auth` npm release. `list`, `switch`, `status`, and `usage` schedule a check after human-readable output; live views (`--live`) and `--json` skip it. The check is at most once per 24 hours, uses a 2-second timeout, and silently ignores failures. `agy-auth version --check` forces a check and reports the result; `--check --json` returns `current`, `latest`, `update_available`, and `checked_at`.

On an interactive terminal, an available update prompts `Update now? [y/N]`. Yes runs the install-method-specific path: npm-managed binaries run `npm install -g @fikrilal/agy-auth@latest`; standalone Linux/macOS binaries download the matching GitHub release asset and verify it against the release `checksums.txt` before atomically replacing the executable. Windows standalone installs and unsupported architectures print the manual commands. Declining records the version in `update-check.json` so the same release is not prompted again. Non-interactive runs print the manual commands.

The result is cached in `update-check.json` (`0600`) beside `registry.json`; a failed check records the attempt with an empty latest version so it does not retry on every command. `AGY_AUTH_NO_UPDATE_CHECK=1` disables automatic checks. The check request sends only `User-Agent` and `Accept`; no account, credential, or registry data is included. The npm registry is the release feed even for Go/source installs, so the notice always includes npm and `go install` update commands.

## Backups

Before replacing or deleting the live credential, `internal/agy.Storage` writes the current raw credential to `backups/active-<timestamp>.json`. `registry.Store.Save` writes the previous `registry.json` to `backups/registry-<timestamp>.json` before changing it. Both use `0600` files in a `0700` directory and keep the newest 5 of each kind. Identical backup content is deduplicated. Registry and file writes that do not change bytes only re-harden permissions; credential writes still refresh the keyring.

Backups contain the same secrets as snapshots. They are local recovery data, not an offsite backup.

## Switching

Queries resolve in this order: row number, exact account ID, exact alias, exact email, account ID prefix, then email/alias/ID substring. Ambiguous matches open a candidate picker in a terminal for `switch`, `alias`, `remove`, and `export`; non-interactive and `--json` calls return `ambiguous_query` with candidates. `switch -` selects `previous`.

1. Load `accounts/<id>.json`.
2. Validate that it contains an access token or refresh token.
3. Refuse to switch to an expired access token with no refresh token.
4. Write the raw document to:
   - the keyring (`gemini` / `antigravity`) via go-keyring, and
   - the file fallback, atomically with mode `0600`.
5. Update `active`, `previous`, and `last_used_at`.
6. Print a reminder to restart agy.

go-keyring's Linux `Set` uses Secret Service `CreateItem(..., replace=true)` with attributes `service` and `username`. It replaces agy's existing item in place, preserving a single entry.

## Removal

`remove` resolves and deduplicates every selector before confirmation or deletion. It deletes all selected snapshot files first, then optionally deletes the live credential with `--logout`, then removes the registry entries and saves. If a snapshot deletion fails, no registry mutation is committed and the command returns `state_uncertain`; some snapshots may already be gone, so reconcile with `agy-auth list` and `agy-auth import`.

## Clean

`clean` removes managed backups beyond retention and snapshot files not referenced by `registry.json`. It only considers backup names matching `active-*.json`/`registry-*.json` with the managed timestamp form and snapshot names with valid account IDs; unknown files are left alone. If `registry.json` is missing, snapshots are preserved so the registry can be rebuilt from them. `--dry-run` reports the same set without deleting anything.

## Import and export

`import` reports each file as imported, updated, or skipped. Directory imports continue after invalid files; a single invalid file exits nonzero. Success rows are written to stdout, skipped rows to stderr, and the run ends with a summary.

`import --purge` rebuilds `registry.json` from `accounts/*.json`. It carries aliases and `added_at`/`last_used_at` from the previous registry by account ID or subject, prefers a live credential match for `active`, and falls back to the previous active account or the first snapshot. Invalid snapshots are skipped and reported.

`export` writes deterministic `<name>-<shortID>.json` files with `0600` permissions and reports per-file results plus a summary.

## Recovering the registry

The account snapshots are authoritative. If `registry.json` is lost or corrupted, rebuild it from the snapshots:

```shell
agy-auth import --purge
```

This recreates entries from the raw credentials (email and subject come from the ID token) and carries aliases and active/previous where they can be matched. Managed backups under `backups/` can restore the previous registry or live credential if the change was recent.

## OAuth login

The login flow uses the public Antigravity/Gemini OAuth client embedded in the agy binary. The repository does not contain the client ID or secret; both are discovered at runtime from the installed `agy` binary by matching SHA-256 fingerprints. Override them with `AGY_AUTH_CLIENT_ID` and `AGY_AUTH_CLIENT_SECRET` if Google rotates them.

- authorization endpoint: `https://accounts.google.com/o/oauth2/auth`
- token endpoint: `https://oauth2.googleapis.com/token`
- redirect URI: `https://antigravity.google/oauth-callback`
- scopes: `cloud-platform`, `userinfo.email`, `userinfo.profile`, `cclog`, `experimentsandconfigs`, `aicode`, `openid`
- PKCE: `S256`
- `access_type=offline`, `prompt=consent`, `response_type=code`

The callback page displays the authorization code in a copyable box. It removes `code` from the address bar but keeps `state`. `agy-auth` accepts a bare code or a callback URL and validates `state` when present.

`login --via-agy` runs the installed `agy` in a temporary `HOME` with `DBUS_SESSION_BUS_ADDRESS`/`XDG_RUNTIME_DIR` cleared so it uses file storage, not the live keyring. The temporary credential is parsed and imported only after agy exits; the real keyring/file credential is written after the registry update. Canceling or failing the agy flow leaves the real active credential untouched, and the temporary home is removed.

The token exchange sends `client_id`, `client_secret`, `code`, `code_verifier`, `grant_type=authorization_code`, and `redirect_uri`.

`oauth.Refresh` uses the same client with `grant_type=refresh_token` and preserves the stored refresh token when Google does not return a replacement. Usage lookups use it for expired access tokens; the refreshed token is not written back to the snapshot.

If Google does not return a new refresh token during a re-login, `agy-auth` reuses the refresh token from the existing snapshot with the same subject.

## Updating OAuth client fingerprints

The repository stores only SHA-256 fingerprints of the Antigravity OAuth client ID and secret. The real values are discovered from the installed `agy` binary at login time.

If Google rotates the client:

1. Extract the candidate values from the local `agy` binary:

   ```shell
   strings "$(command -v agy)" | grep -oE 'GOCSPX-[A-Za-z0-9_-]+|[0-9]+-[a-z0-9]+\.apps\.googleusercontent\.com' | sort -u
   ```

2. Identify the client used by Antigravity and compute its hashes:

   ```shell
   printf %s '<client-id>' | sha256sum
   printf %s '<client-secret>' | sha256sum
   ```

3. Replace `antigravityClientIDHash` and `antigravityClientSecretHash` in `internal/oauth/oauth.go`.
4. Do not commit the values themselves; keep only the hashes.
5. Verify with `agy-auth login --no-browser` and `go test ./internal/oauth`.

## File permissions and atomic writes

Every write uses a temporary file in the destination directory, `chmod 0600`, `fsync`, and `rename`. This avoids partially written credentials and keeps permissions stable on Unix.

## Testing

```shell
go test ./...
go vet ./...
```

For manual CLI tests, use a temporary registry and the file backend:

```shell
TMP=$(mktemp -d)
AGY_AUTH_HOME="$TMP/config" \
AGY_AUTH_STORAGE=file \
AGY_AUTH_AGY_TOKEN_FILE="$TMP/token" \
./bin/agy-auth list
```

The opt-in keyring test:

```shell
AGY_AUTH_KEYRING_TEST=1 go test ./internal/agy -run TestKeyringRoundTrip -v
```

It writes to service `agy-auth-selftest`, user `selftest`, then deletes the item.
