# ADR-0001: Adopt a repository harness with mechanical guardrails

- **Status:** Accepted
- **Date:** 2026-10-21
- **Decision makers:** Project owner
- **Consulted:** `backend-core-kit`, `mobile-core-kit`, `codex-auth` references
- **Related:** [Harness](../harness.md), [Execution plans](../exec-plans/README.md), [Agent workflow](../engineering/agent-workflow.md)

## Context

`agy-auth` is a small Go CLI, but it handles long-lived OAuth refresh tokens and the credential that `agy` uses. A mistake can leak a token, corrupt the active credential, or write it with unsafe permissions. The repository is also intended to be maintained by coding agents.

Prose guidance in `AGENTS.md` is useful feedforward, but it is probabilistic. It does not stop an agent from adding a direct `fmt.Println` of a token, bypassing the atomic write helper, importing the keyring outside its boundary, or changing a credential shape without updating a contract.

## Decision drivers

- Make high-risk invariants fail mechanically.
- Keep local and CI verification identical.
- Give agents deterministic, actionable diagnostics.
- Keep the harness small and removable; no second product.
- Preserve human ownership of judgment, review, and publication.

## Considered options

- **Docs and `AGENTS.md` only** — low cost, but soft and easy to bypass.
- **`golangci-lint` only** — useful standard sensors, but cannot express repository-specific credential/architecture invariants.
- **Copy `backendkit`/`mobilekit`** — proven patterns, but both include loop-engineering layers and are much larger than this repository needs.
- **A repository-local Go harness** — native, testable, small enough to own.

## Decision

Adopt a repository-local harness under `cmd/harness` and `internal/harness` with:

- `fast`, `full`, and `ci` verification profiles, used locally and in CI;
- custom Go AST lints for output/JSON boundaries, keyring access, atomic writes, permissions, architecture, panics, `os.Exit`, and test isolation;
- a high-confidence secret scanner;
- docs link and version-sync checks;
- independent credential/registry contract fixtures;
- a read-only doctor;
- opt-in pre-commit and commit-msg hooks;
- Conventional Commits enforced by the commit-msg hook.

The harness explicitly **excludes loop engineering**: no execution-plan controller, task authority, path enforcement, repair loop, workspace management, event intake, handoff approval, operating-evidence ledger, or autonomous harness improvement. Execution plans are documentation and review artifacts; humans retain judgment and publication authority.

Oracles are deferred because, without task authority, a separate registry would duplicate the normal regression suite.

## Consequences

- **Good:** dangerous changes fail locally with a precise rule and remediation.
- **Good:** CI and local verification share one profile source of truth.
- **Good:** contract fixtures make credential/registry drift visible.
- **Good:** hooks catch credential and commit-policy mistakes before review.
- **Neutral:** the harness becomes safety-critical code and needs its own tests, docs, and review.
- **Bad:** contributors must install Go (and Node for full/ci) and run the canonical profile rather than ad-hoc commands.
- **Bad:** every new rule adds maintenance; guardrails must earn their keep.

## Confirmation

- `make verify`, `make verify-full`, and `make verify-ci` pass.
- CI runs the same `ci` profile.
- A deliberately unsafe change is rejected by the relevant lint or secret check.
- The harness's own tests cover the rules and checks.

## Links

- [Harness guide](../harness.md)
- [Execution plans](../exec-plans/README.md)
- [Agent workflow](../engineering/agent-workflow.md)
- [Internals](../internals.md)
