# Release process

The npm package is `@fikrilal/agy-auth`; the installed command is `agy-auth`.

## One-time setup

1. Log in to npm and confirm the account owns the `fikrilal` scope:

   ```shell
   npm login
   npm whoami
   ```

   If your npm username is not `fikrilal`, change `"name"` in `package.json`
   (for example `@your-user/agy-auth`) and update the install commands in
   `README.md`. Alternatively, create an npm organization named `fikrilal` and
   publish under it.

2. Create a granular access token with read/write access to `@fikrilal/agy-auth` (or the `@fikrilal` scope) and **Bypass 2FA** enabled.

   > npm is tightening bypass-2FA tokens for direct publishing. If the release workflow starts failing with `403`, move to npm trusted publishing (OIDC) or a supported token type.

3. Add it to GitHub as the `NPM_TOKEN` repository secret:

   ```shell
   gh secret set NPM_TOKEN --repo fikrilal/agy-auth
   ```

   The release workflow skips the npm publish step when `NPM_TOKEN` is absent,
   so GitHub releases still work without it.

## Versioning

`package.json` is the source of truth for the version. The npm build injects
it into the Go binary with `-X github.com/fikrilal/agy-auth/internal/cli.Version`,
so `agy-auth version` matches the npm package version.

1. Update `"version"` in `package.json`.
2. Add an entry to `CHANGELOG.md`.
3. Commit and tag:

   ```shell
   git add package.json CHANGELOG.md
   git commit -m "Release v0.2.0"
   git tag v0.2.0
   git push origin main --tags
   ```

4. The `release` workflow:
   - checks that the tag matches `package.json`,
   - runs `go vet` and `go test`,
   - builds all six platform binaries into `npm/vendor/`,
   - publishes `@fikrilal/agy-auth` to npm when `NPM_TOKEN` is set,
   - creates a GitHub release with the binaries and `checksums.txt`.

## Manual publish

```shell
npm publish --access public
```

Scoped packages must be published with `--access public` on the first release;
`package.json` also sets `publishConfig.access`.

## Update notification

`agy-auth` checks the npm `latest` dist-tag at most once every 24 hours after
`list`, `switch`, `status`, or `usage`. On an interactive terminal it prompts
`Update now? [y/N]`; yes updates npm installs through npm or a standalone
Linux/macOS binary from the matching GitHub release after SHA-256 checksum
verification. Non-interactive runs print the manual commands. Declining
suppresses the prompt for that version. `agy-auth version --check` forces a
check without applying it. No account or token data is sent. Users can disable
automatic checks with `AGY_AUTH_NO_UPDATE_CHECK=1`.

After a release, verify the published version, release assets, and check path:

```shell
npm view @fikrilal/agy-auth version
gh release view "v$(node -p 'require("./package.json").version')" \
  --json assets --jq '.assets[].name'
agy-auth version --check
```

## Verify

```shell
npm view @fikrilal/agy-auth version
npm install -g @fikrilal/agy-auth
agy-auth version
```
