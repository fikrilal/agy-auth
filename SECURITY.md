# Security

`agy-auth` handles OAuth access and refresh tokens for Antigravity CLI accounts. Treat those tokens as passwords.

## Reporting

Please report vulnerabilities privately through GitHub Security Advisories:

https://github.com/fikrilal/agy-auth/security/advisories/new

Do not open a public issue for token leakage, credential storage flaws, or authentication bypasses.

## What is stored

- The active credential is written to the OS keyring (`gemini` / `antigravity`) and the file mirror at `~/.gemini/antigravity-cli/antigravity-oauth-token`.
- Other accounts are plaintext JSON snapshots under `~/.config/agy-auth/accounts/` with `0600` permissions inside a `0700` directory.
- No telemetry. Tokens only go to Google OAuth endpoints and the local `agy` binary.

## Trust model

Anyone who can read your user account can read the snapshot files. Treat `~/.config/agy-auth` like a password store. Use full-disk encryption and lock your OS session.

## Supported versions

Only the latest release receives security fixes.
