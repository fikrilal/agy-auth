# Google Cloud Project (Pay As You Go) Account Support

| Field | Value |
|---|---|
| Status | completed |
| Owner | maintainers |
| Date | 2026-10-30 |
| Related | [ADR-0003](../../adr/0003-gcp-paygo-accounts.md), [Internals](../../internals.md), [User guide](../../user-guide.md) |

## Objective

Enable `agy-auth` to authenticate, store, display, and switch Google Cloud project (`auth_method: "gcp"`) accounts in **Pay As You Go** billing mode alongside personal (`auth_method: "consumer"`) accounts.

## Context

When signing in to Antigravity CLI (`agy`) with a Google Cloud project in Pay As You Go mode, `agy` stores a credential JSON with `auth_method: "gcp"`, `project_id: "<gcp-project-id>"`, `region: "global"|"us"|"eu"`, and `id_token_email` in the OS keyring and file mirror. Without `project_id` (and when `user_tier` is empty), `agy` treats a `"gcp"` credential as incomplete onboarding and refuses to authenticate. Previously, `agy-auth` only created `"consumer"` credentials during direct OAuth login and did not track `project_id`, `region`, or `id_token_email`.

## Scope

### In scope

- Extend `agy.TokenFile` with `StoredToken` fields (`id_token_email`, `project_id`, `region`, `user_tier`, `tier_display_name`, `wif_provider`, `saved_wif_provider`, `writer_id`) and region/auth-method validation helpers.
- Update `internal/oauth` to support `agy`'s GCP OAuth scopes, populate `id_token_email`, `project_id`, and `region`, and recognize both legacy and current `agy` OAuth client secret SHA-256 fingerprints.
- Update `internal/registry` to store `project_id` and `region` on `Account`, distinguish accounts by `(auth_method, project_id)` when `auth_method == "gcp"`, and resolve queries against `project_id`.
- Update `internal/cli` (`login`, `add`, `import`, `list`, `current`, `status`, `spec`) to support `--gcp`, `--project`, and `--region` flags (plus interactive prompts on `login --gcp`) and display GCP project/region metadata.
- Update `internal/usage` to pass the token's `project_id` when querying quota for GCP project credentials.
- Add contract fixtures, unit tests, and documentation updates.

### Out of scope

- Advanced Workforce Identity Federation (WIF / SSO config) browser flows.

## Expected paths

Informational only; this is not enforced by a task controller.

- `internal/agy/token.go`
- `internal/agy/token_test.go`
- `internal/oauth/oauth.go`
- `internal/oauth/oauth_test.go`
- `internal/registry/registry.go`
- `internal/registry/registry_test.go`
- `internal/usage/usage.go`
- `internal/usage/groups.go`
- `internal/usage/usage_test.go`
- `internal/cli/cmd_login.go`
- `internal/cli/cmd_list.go`
- `internal/cli/cmd_import.go`
- `internal/cli/cmd_status.go`
- `internal/cli/picker.go`
- `internal/cli/live.go`
- `internal/cli/render.go`
- `internal/cli/spec.go`
- `internal/cli/cli_test.go`
- `internal/harness/contracts/contracts.go`
- `harness/fixtures/credential.gcp.json`
- `README.md`
- `docs/internals.md`
- `docs/user-guide.md`
- `docs/adr/0003-gcp-paygo-accounts.md`

## Risks

- Changing account ID derivation for existing consumer accounts would break existing registries; mitigated by keeping `accountID` unchanged for non-GCP accounts and only suffixing `:gcp:<project_id>` when `auth_method == "gcp"` and `project_id != ""`.
- Newer `agy` binaries rotated the embedded OAuth client secret; mitigated by matching against a list of known SHA-256 fingerprints.

## Acceptance criteria

- [x] `agy.ParseTokenFile` parses `project_id`, `region`, `id_token_email`, and related `StoredToken` fields, and `Email()` falls back to `id_token_email`.
- [x] `agy-auth login --gcp --project <id> [--region global|us|eu]` (and interactive `agy-auth login --gcp`) writes a valid `auth_method: "gcp"` credential with `project_id` and `region` that `agy` accepts in Pay As You Go mode.
- [x] `agy-auth add` and `agy-auth import` preserve or apply `--gcp`/`--project`/`--region` metadata.
- [x] `agy-auth list`, `agy-auth current`, and `agy-auth status` surface GCP project and region details in both text and `--json` output.
- [x] `agy-auth switch <project-id>` resolves and switches to the matching GCP account.
- [x] `go run ./cmd/harness verify --profile fast` passes without errors.

## Verification plan

```shell
go run ./cmd/harness verify --profile fast
go run ./cmd/harness verify --profile full
```

Additional checks:

- Verify contract fixture `harness/fixtures/credential.gcp.json`.
- Verify OAuth discovery against the installed `agy` binary fingerprint.

## Rollback

Revert the commit; `registry.json` remains on schema version 2 with optional `project_id` and `region` fields (`omitempty`), so older binaries ignore unknown fields cleanly.

## Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-10-30 | Scope GCP account IDs by `project_id` when `auth_method == "gcp"` | Allows the same Google user to store both personal consumer and per-project GCP Pay As You Go accounts |
| 2026-10-30 | Support both `--project <id>` flag and interactive prompt when `--gcp` is set | Supports both scripted CLI usage and interactive onboarding matching `agy`'s screens |

## Completion

- Commit/tag: uncommitted (awaiting user request)
- Verification result: `harness verify --profile fast` and targeted unit/contract suites passed
- Residual risk: low
