# Execution plans

Execution plans are short, reviewable documents for non-trivial work. They answer: what are we changing, why, what is in scope, how will we know it worked, and how do we back out?

They are **not** loop-engineering authority. `agy-auth` has no task state machine, path enforcement, repair loop, or handoff approval. A plan is a planning and review artifact; humans still own judgment, review, commits, and publication.

## When to write one

Write a plan when the work is:

- more than a small, obvious fix;
- touching multiple packages, credentials, storage, OAuth, CI, release, or the harness;
- changing public behavior or an external contract;
- ambiguous enough that the approach should be reviewed before implementation.

Small docs fixes, typo fixes, and tightly specified one-file changes do not need a plan.

## Lifecycle

1. **Draft** — write the plan, usually while exploring. It can live in `_WIP/` until the approach is agreed.
2. **Active** — move it to `active/` before implementation. Keep it current if scope changes.
3. **Completed** — move it to `completed/` when the change is verified and reviewed. Add the verification result and the final commit/tag.

There is no automated status transition. The file move is the record.

## Naming

```text
YYYY-MM-DD_short-title.md
```

Examples:

```text
2026-10-21_cli-help-and-json-contract.md
2026-10-22_live-switch-view.md
```

## Template

Use [_template.md](_template.md). Keep it short:

- objective and context;
- in/out of scope;
- expected paths (informational, not enforced);
- risks;
- acceptance criteria;
- verification plan;
- rollback;
- decision log.

## Review

- A plan is reviewed like a small design document.
- If the plan changes architecture, security, or public behavior, add or update an ADR.
- If a plan reveals a repeated failure mode, add a harness rule or test instead of relying on the plan prose.

## Related

- [Agent workflow](../engineering/agent-workflow.md)
- [Harness](../harness.md)
- [ADRs](../adr/README.md)
