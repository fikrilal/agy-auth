# Repository Harness

`agy-auth` ships with a repository-local harness: deterministic guardrails, independent contracts, and one verification surface for humans and coding agents.

The goal is not to replace review. The goal is to make the unsafe path fail mechanically, before review, with an actionable error.

## Model

The harness follows the standard harness-engineering split:

| Layer | What it does here |
|---|---|
| **Feedforward** | `AGENTS.md`, this document, `docs/internals.md`, architecture map, contract fixtures |
| **Sensors** | Custom Go AST lints, secret scanning, docs/version checks, commit checks |
| **External contracts** | Credential and registry fixtures that cannot drift silently |
| **Unified verification** | `harness verify --profile fast|full|ci`, used locally and in CI |

It is deliberately **not** loop engineering. There is no task authority, execution plan controller, repair loop, handoff approval, event intake, operating-evidence ledger, or autonomous harness improvement. The harness is the environment; the loop is a separate concern.

## Commands

```shell
go run ./cmd/harness verify --profile fast
go run ./cmd/harness verify --profile full
go run ./cmd/harness verify --profile ci

go run ./cmd/harness lint
go run ./cmd/harness secrets
go run ./cmd/harness docs
go run ./cmd/harness contracts
go run ./cmd/harness doctor
go run ./cmd/harness install-hooks
go run ./cmd/harness list
go run ./cmd/harness version
```

The Makefile exposes the common forms:

```shell
make verify        # fast
make verify-full   # full
make verify-ci     # ci
make lint
make doctor
make hooks
```

There are no `--skip-*` flags. If a check is not relevant, fix the change or update the harness rules in a reviewed commit. `--no-verify` is a host-level git override, not verification evidence.

## Profiles

| Step | fast | full | ci |
|---|:---:|:---:|:---:|
| `gofmt -l` | ✓ | ✓ | ✓ |
| Custom Go AST lints | ✓ | ✓ | ✓ |
| Secret scan | ✓ | ✓ | ✓ |
| Docs links + version sync | ✓ | ✓ | ✓ |
| Contract fixtures | ✓ | ✓ | ✓ |
| `go vet ./...` | ✓ | ✓ | ✓ |
| `go build ./...` | ✓ | ✓ | ✓ |
| `go test -short ./...` | ✓ | ✓ |  |
| `go test -race ./...` |  | ✓ |  |
| `go test -race -count=1 -shuffle=on ./...` |  |  | ✓ |
| Build current npm target |  | ✓ | ✓ |
| Node shim smoke test |  | ✓ | ✓ |
| `npm pack --dry-run` |  | ✓ |  |
| Build all npm targets |  |  | ✓ |
| `npm pack` to a temp directory |  |  | ✓ |

`fast` is the default. `full` is the pre-review gate for non-trivial changes. `ci` is what GitHub Actions runs; local and hosted verification must stay in parity.

## Custom lints

| Rule | Scope | Catches |
|---|---|---|
| `output-boundary` | `internal/{agy,registry,oauth,cli}` | `fmt.Print*`, `log.*`, `os.Stdout/Stderr` outside `internal/cli/app.go` |
| `json-boundary` | `internal/cli` | `json.NewEncoder` outside `internal/cli/render.go` |
| `keyring-boundary` | all files | `go-keyring` use outside `internal/agy/storage.go` |
| `atomic-writes` | `internal/{agy,registry}` | `os.WriteFile`, `os.Create`, write-mode `os.OpenFile` outside `writeFileAtomic` |
| `file-permissions` | `internal/{agy,registry}` | Literal modes other than `0600`/`0700` |
| `architecture` | all packages | Wrong internal imports; production imports `internal/harness` |
| `no-panic` | `internal/*` non-test | `panic(...)` |
| `no-os-exit` | non-`cmd` packages | `os.Exit` |
| `test-isolation` | `_test.go` | `DefaultStore`, `DefaultStorage`, real keyring use |
| `secret-literals` | non-ignored text files | OAuth client secrets/IDs, npm/GitHub/OpenAI/AWS/Slack tokens, private keys |
| `bot-coauthor` | git history | `CommandCodeBot` or bot co-author trailers |
| `docs-links` | Markdown | Broken relative links |
| `version-sync` | repository metadata | `package.json`, Go default version, and `CHANGELOG.md` drift |
| `command-help` | CLI regression test | A command or alias without help |

Findings are printed as `file:line:column: rule: message`. Secret findings never include the matched value.

## Contracts

`harness/fixtures/` is independent ground truth for the two external data shapes:

- `credential.consumer.json` — full agy credential document with a fake ID token.
- `credential.minimal.json` — access-token-only document.
- `credential.invalid.json` — missing token object; must be rejected.
- `registry.valid.json` — valid registry index.
- `registry.invalid.json` — path-traversal account ID; must be rejected.

`harness contracts` validates them with the public parsers. Change a fixture only when the external contract intentionally changes.

Critical CLI behavior lives in the normal Go test suite (`TestCLILifecycle`, `TestParseTokenFile`, `TestFileStorageRoundTrip`, `TestDiscoverClient`, `TestEveryCommandHasHelp`). There is no separate oracle registry; oracles are a loop-engineering mechanism and are deferred until there is a task-authority consumer.

## Commit policy

Commits use Conventional Commits, matching the core kits:

```text
feat(harness): add verification profiles
fix(cli): handle EOF while reading a code
docs: explain credential storage
chore(deps): update npm metadata
```

Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`. Scopes are lowercase. `Merge`, `Revert`, `fixup!`, and `squash!` subjects are allowed for git mechanics.

The `commit-msg` hook also rejects `CommandCodeBot` and bot co-author trailers. The user is the only author.

## Hooks

```shell
make hooks
```

This runs `harness install-hooks`, sets `core.hooksPath=.githooks`, and makes both hooks executable:

- `.githooks/pre-commit` runs `harness verify --profile fast`.
- `.githooks/commit-msg` runs `harness commit-msg <file>`.

Hooks are opt-in; CI remains authoritative. A commit created with `--no-verify` is not considered verified.

## CI and release

`.github/workflows/ci.yml` runs exactly one command:

```shell
go run ./cmd/harness verify --profile ci
```

The release workflow runs the same `ci` profile before publishing, then keeps the tag/version check and npm publish. Do not duplicate the step list in workflow YAML.

## Extending the harness

Add a guardrail only when it prevents a real, recurring failure:

1. Add the rule/check and a focused test with a failing and passing case.
2. Add it to the appropriate profile and `harness list`.
3. Document the rule in this file and its remediation message in code.
4. Run `make verify`, `make verify-full`, and `make verify-ci`.
5. Treat the change as harness-sensitive; it is reviewed like production code.

No baselines for secrets, keyring-boundary, atomic-write, or permission violations. Allowlists, if ever needed, must name the rule, file, owner, rationale, and date, and must not hide a real credential.

## Troubleshooting

- `harness verify --profile fast` needs Go only; `full`/`ci` also need Node 18+ and npm.
- `harness doctor` reports missing tools, version drift, broken docs links, contract failures, lint/secret findings, and whether hooks are installed.
- If `full`/`ci` fails at npm packaging, run `node scripts/build-npm.mjs --current` (or `--all`) directly; the harness calls the same script.
- If a hook fails, the error names the rule and file. Do not bypass with `--no-verify`; fix the finding or update the harness policy through review.

## Ownership

| Path | Responsibility |
|---|---|
| `cmd/harness` | CLI entrypoint |
| `internal/harness/app.go` | command dispatch and exit codes |
| `internal/harness/verify` | profiles and step execution |
| `internal/harness/lint` | Go AST rules |
| `internal/harness/secret` | adverse-content scan |
| `internal/harness/docs` | link and version checks |
| `internal/harness/contracts` | credential/registry fixtures |
| `internal/harness/doctor` | read-only readiness inspection |
| `internal/harness/gitcheck` | commit history check and hook installation |
| `harness/fixtures` | independent data contracts |
| `.githooks` | opt-in local guards |

Production packages must not import `internal/harness`; the `architecture` lint enforces this.

## Related docs

- [Contributing](../CONTRIBUTING.md)
- [Internals](internals.md)
- [Release process](release.md)
- [Agent contract](../AGENTS.md)
