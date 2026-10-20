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

2. Create an npm **Automation** token with publish rights.

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
2. Update `CHANGELOG.md` if you maintain one.
3. Commit and tag:

   ```shell
   git add package.json
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

## Verify

```shell
npm view @fikrilal/agy-auth version
npm install -g @fikrilal/agy-auth
agy-auth version
```
