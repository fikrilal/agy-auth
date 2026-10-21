# Engineering guides

These are implementation-aware guides for people and agents working in `agy-auth`. They explain intent; the [harness](../harness.md) enforces the mechanical rules.

## Index

- [Agent workflow](agent-workflow.md) — read order, execution plans, verification, commits
- [Architecture](architecture.md) — package map, import direction, credential flow
- [Testing](testing.md) — test layers, isolation, keyring policy, harness profiles
- [Code standards](code-standards.md) — Go style, boundaries, credentials, commits
- [Harness](../harness.md) — profiles, lints, contracts, hooks
- [Execution plans](../exec-plans/README.md) — non-trivial work plans
- [ADRs](../adr/README.md) — decisions that outlive a single change

## Scope

The guides are deliberately small. Add a guide only when the same explanation is needed repeatedly. If a rule can be checked mechanically, put it in the harness instead of adding more prose.
