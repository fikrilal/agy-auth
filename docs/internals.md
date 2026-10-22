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
registry.json
accounts/<id>.json
backups/active-<timestamp>.json
backups/registry-<timestamp>.json
```

`registry.json`:

```json
{
  "version": 1,
  "active": "acct_...",
  "previous": "acct_...",
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

Account IDs are the first 10 hex characters of SHA-256 over the Google subject (or the refresh-token fingerprint when no ID token is available). Snapshot files are stored with mode `0600`; the registry is `0600`; directories are `0700`.

The registry's `active` field is a hint. `list` and `status` treat the live credential as authoritative and match it against snapshots by subject first, then fingerprint, then a unique email.

`previous` records the account that was active before the last switch (including `add`, `login`, and `import --activate`). `switch -` uses it; it is cleared when that account is removed, when the registry no longer contains it, or when it equals `active`.

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

## Recovering the registry

The account snapshots are authoritative. If `registry.json` is lost or corrupted, rebuild it from the snapshots:

```shell
agy-auth import ~/.config/agy-auth/accounts
```

This recreates entries from the raw credentials (email and subject come from the ID token). Aliases, `active`, `previous`, `added_at`, and `last_used_at` are not stored in the snapshots, so they are lost unless you back up `registry.json`. Managed backups under `backups/` can restore the previous registry or live credential if the change was recent.

## OAuth login

The login flow uses the public Antigravity/Gemini OAuth client embedded in the agy binary. The repository does not contain the client ID or secret; both are discovered at runtime from the installed `agy` binary by matching SHA-256 fingerprints. Override them with `AGY_AUTH_CLIENT_ID` and `AGY_AUTH_CLIENT_SECRET` if Google rotates them.

- authorization endpoint: `https://accounts.google.com/o/oauth2/auth`
- token endpoint: `https://oauth2.googleapis.com/token`
- redirect URI: `https://antigravity.google/oauth-callback`
- scopes: `cloud-platform`, `userinfo.email`, `userinfo.profile`, `cclog`, `experimentsandconfigs`, `aicode`, `openid`
- PKCE: `S256`
- `access_type=offline`, `prompt=consent`, `response_type=code`

The callback page displays the authorization code in a copyable box. It removes `code` from the address bar but keeps `state`. `agy-auth` accepts a bare code or a callback URL and validates `state` when present.

The token exchange sends `client_id`, `client_secret`, `code`, `code_verifier`, `grant_type=authorization_code`, and `redirect_uri`.

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
