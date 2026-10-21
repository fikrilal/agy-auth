# Code standards

These are the rules that matter for `agy-auth`. The [harness](../harness.md) checks the mechanical ones; the rest are review expectations.

## Go

- Run `gofmt` through `make verify`; do not hand-format.
- Keep packages small and focused. Prefer an explicit return over a panic.
- Wrap errors with context; do not swallow failures.
- Keep CLI output in `internal/cli`; library packages return values and errors.
- Do not call `os.Exit` outside `cmd/`.
- Do not use `panic` in `internal/`.

## Boundaries

- `internal/agy/storage.go` is the only keyring boundary.
- All credential and registry writes go through the atomic write helper.
- Credential files are `0600`; directories are `0700`.
- CLI output goes through `App.Out` / `App.Err`; JSON goes through `emitJSON`.
- Production code must not import `internal/harness`.

## Credentials and secrets

- Never commit OAuth client IDs/secrets, tokens, real account snapshots, or private keys.
- Never print tokens or the OAuth client secret.
- Login must discover the public Antigravity client from the installed `agy` binary; only SHA-256 fingerprints belong in source.
- Tests must not touch the real keyring or a real account.

## CLI

- Every command and alias must have help text.
- Machine-readable output should be stable and documented; do not mix warnings into JSON.
- Query resolution should be predictable: row number, alias, email, or ID prefix.
- Destructive operations should be explicit about what they remove.

## Commits

Use Conventional Commits:

```text
feat(scope): summary
fix(scope): summary
docs: summary
test(scope): summary
chore(scope): summary
```

Scopes are lowercase. The `commit-msg` hook enforces the pattern and rejects bot co-author trailers. The user is the only author.

## Documentation

- Update `README.md` for user-visible behavior.
- Update `docs/internals.md` for storage, registry, switching, or OAuth changes.
- Update `docs/harness.md` when a guardrail changes.
- Add an ADR when a decision changes architecture, security, or public behavior.
- Add an execution plan for non-trivial work.

## Related

- [Harness](../harness.md)
- [Architecture](architecture.md)
- [Testing](testing.md)
- [Agent workflow](agent-workflow.md)
