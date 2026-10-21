# Agent workflow

This is the working agreement for coding agents and humans. The harness is the gate; this document is the path to it.

## Read order

1. [README](../../README.md) — user-facing behavior.
2. [Harness](../harness.md) — profiles, rules, contracts, hooks.
3. [Architecture](architecture.md) — package boundaries and credential flow.
4. [Internals](../internals.md) — credential storage, registry, switching, OAuth.
5. The relevant package under `internal/`.

## When to write an execution plan

Write a plan under [docs/exec-plans/active/](../exec-plans/active/) when the work is:

- more than a small, obvious fix;
- touching multiple packages or a credential path;
- changing public behavior, storage, OAuth, CI, release, or the harness;
- ambiguous enough that the approach should be reviewed before code.

Small docs fixes, typo fixes, and tightly specified one-file changes do not need a plan.

Use the [template](../exec-plans/_template.md). Keep it short: objective, scope, expected paths, risks, acceptance criteria, verification plan, rollback, decision log. Move it to `completed/` when the work is done.

The plan is a **review artifact**, not an authority controller. `agy-auth` intentionally has no task state machine, path enforcement, repair loop, or handoff approval. Humans still own judgment, review, commits, and publication.

## Work loop

1. **Inspect** — read the relevant code and tests; do not propose changes to code you have not read.
2. **Plan** — for non-trivial work, write the execution plan; keep it current if scope changes.
3. **Implement** — keep the change focused; do not mix unrelated refactors.
4. **Verify** — run the harness profile appropriate to the change.
5. **Report** — changed files, behavior, verification, and residual risk.

## Verification

```shell
make verify        # fast: static checks, contracts, build, short tests
make verify-full   # race tests + current-target npm packaging
make verify-ci     # CI parity: fresh race tests + all-target npm packaging
```

- `make verify` is the default local gate.
- `make verify-full` is expected for non-trivial changes.
- CI runs `make verify-ci`; do not duplicate its steps in a pull request description.
- Do not use skip flags. A check you did not run is not evidence.
- `make hooks` installs the pre-commit and commit-msg hooks.

If a failure or review comment repeats, add a lint, contract, or regression test instead of relying on memory. Update [docs/harness.md](../harness.md) when behavior changes.

## Commit pattern

Use Conventional Commits:

```text
feat(scope): summary
fix(scope): summary
docs: summary
test(scope): summary
chore(scope): summary
```

Scopes are lowercase and short (`harness`, `cli`, `storage`, `oauth`, `registry`, `docs`, `ci`, `release`). The `commit-msg` hook enforces the pattern and rejects bot co-author trailers. The user is the only author.

## Boundaries

- Do not commit tokens, OAuth client secrets, or real account snapshots.
- Do not import `internal/harness` from production code.
- Do not bypass the atomic write helper or the keyring boundary.
- Do not add a guardrail for a one-off preference; wait for a repeated failure.
- Leave unrelated dirty worktree changes untouched.
- Only commit or push when asked.

## Related

- [Harness](../harness.md)
- [Testing](testing.md)
- [Code standards](code-standards.md)
- [Execution plans](../exec-plans/README.md)
- [Contributing](../../CONTRIBUTING.md)
