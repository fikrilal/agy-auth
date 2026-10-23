# Security

`agy-auth` handles OAuth access and refresh tokens for Antigravity CLI accounts. Treat those tokens as passwords.

## Reporting

Please report vulnerabilities privately through GitHub Security Advisories:

https://github.com/fikrilal/agy-auth/security/advisories/new

Do not open a public issue for token leakage, credential storage flaws, or authentication bypasses.

## What is stored

- The active credential is written to the OS keyring (`gemini` / `antigravity`) and the file mirror at `~/.gemini/antigravity-cli/antigravity-oauth-token`.
- Account snapshots under `~/.config/agy-auth/accounts/` are encrypted with AES-256-GCM. The key is `~/.config/agy-auth/encryption.key` (`0600`); snapshots and the key directory are `0600`/`0700`.
- Before changing the live credential or registry, timestamped active-credential backups are encrypted under `~/.config/agy-auth/backups/`; registry backups contain metadata only. Files keep the newest 5 of each kind.
- `export` deliberately writes plaintext credential JSON. Exported files are secrets.
- No telemetry. Tokens only go to Google OAuth endpoints, the local `agy` binary, and the Code Assist quota API. The update check sends only the package metadata request to `registry.npmjs.org`; it sends no account or token data.

## Trust model

The encryption key sits beside the encrypted snapshots, so someone who can read your entire user account can read both. The key protects against copying or syncing snapshot files without the key, not against a fully compromised session. Treat `~/.config/agy-auth` like a password store: use full-disk encryption, lock your OS session, and protect `encryption.key` separately. Losing the key makes encrypted snapshots unrecoverable.

Review the [user guide](docs/user-guide.md#storage-and-switching) for paths and export behavior.

## Supported versions

Only the latest release receives security fixes.
