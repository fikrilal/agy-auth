# ADR-0003: Support Google Cloud Project (Pay As You Go) Accounts

- **Status:** Accepted
- **Date:** 2026-10-30
- **Decision makers:** Repository maintainers
- **Consulted:** Antigravity CLI (`agy`) binary contract (`codeassistclient.StoredToken`, `keyringAuth.TryAuth`, `CLIAuthProvider.ResolveAuthMode`)
- **Related:** [Execution plan](../exec-plans/completed/2026-10-30_gcp-paygo-accounts.md), [Internals](../internals.md)

## Context

Users often switch between a personal Google account (`auth_method: "consumer"`) and a work account that authenticates through a Google Cloud project (`auth_method: "gcp"`) using the **Pay As You Go** billing model in `agy`.

Inspection of the `agy` binary (`codeassistclient.StoredToken`, `keyringAuth.TryAuth`, and `CLIAuthProvider.ResolveAuthMode`) establishes the exact credential contract for Google Cloud project accounts:

1. `StoredToken` includes `auth_method`, `id_token`, `id_token_email`, `project_id`, `region`, `user_tier`, `tier_display_name`, `wif_provider`, `saved_wif_provider`, and `writer_id`.
2. When `auth_method` is `"gcp"`, `keyringAuth.TryAuth` rejects the stored token as incomplete onboarding if both `user_tier` and `project_id` are empty.
3. When `auth_method` is `"gcp"`, `user_tier` is empty (`""`), `project_id` is set to the user's Google Cloud project ID (for example `my-sample-gcp-project`), and `region` is set to `"global"`, `"us"`, or `"eu"`, `CLIAuthProvider.ResolveAuthMode` resolves to `"business_agent_platform_paygo"` (Pay As You Go).
4. Newer `agy` releases also store `id_token_email` alongside or instead of `id_token`, and may rotate the embedded OAuth client secret across `agy` versions.

Previously, `agy-auth` only wrote `auth_method: "consumer"` (without `project_id` or `region`) during `agy-auth login`, did not fall back to `id_token_email` when `id_token` was absent, and derived account IDs solely from `(sub, fingerprint)`, preventing the same Google user from storing both a personal consumer profile and a GCP project profile (or multiple GCP projects) as separate switchable accounts.

## Decision drivers

- Allow users to log in, add, import, list, status-check, and switch between personal (`consumer`) accounts and Google Cloud project (`gcp` Pay As You Go) accounts without re-running `agy`'s multi-step onboarding wizard.
- Preserve 100% backward compatibility with existing `consumer` snapshots, account IDs, and CLI outputs.
- Support storing both a `consumer` profile and one or more `gcp` project profiles even when they share the same Google `sub` claim.
- Never embed OAuth client IDs or client secrets in the repository; only store SHA-256 fingerprints for binary discovery.

## Considered options

- **Option A: Require `--via-agy` for all GCP logins** — Rejected because `--via-agy` requires an interactive terminal, spawns `agy` in an isolated directory, and forces the user through 6 TUI screens. Direct OAuth login with `--gcp --project <id> [--region global|us|eu]` (or interactive prompts when `--gcp` is passed on a TTY) is faster, scriptable, and works headless.
- **Option B: Support first-class GCP Pay As You Go flags (`--gcp`, `--project`, `--region`) across `login`, `add`, and `import`, and include `project_id`/`region` in `TokenFile` and `registry.Account`** — Accepted.
- **Option C: Overwrite the existing account entry when the same Google `sub` logs in with a GCP project** — Rejected because many developers use the same Google identity for both consumer and GCP project profiles, or across multiple GCP projects. Scoping GCP account IDs by `(sub/fingerprint, "gcp", project_id)` while keeping consumer account IDs unchanged allows side-by-side profiles.

## Decision

1. Extend `agy.TokenFile` with the `StoredToken` fields (`id_token_email`, `project_id`, `region`, `user_tier`, `tier_display_name`, `wif_provider`, `saved_wif_provider`, `writer_id`) and domain constants for `AuthMethod` (`consumer`, `gcp`) and GCP regions (`global`, `us`, `eu`).
2. Fall back to `id_token_email` in `TokenFile.Email()` when `id_token` is absent or has no email claim, and populate `id_token_email` when constructing credentials from OAuth responses.
3. Add `--gcp`, `--project <id>`, and `--region <global|us|eu>` flags to `agy-auth login` (and allow `--gcp`/`--project`/`--region` overrides on `add` and single-file `import`). When `--gcp` is passed to `login` without `--project` on an interactive TTY, prompt for the Google Cloud project ID and region (defaulting to `global`).
4. Use `agy`'s GCP OAuth scope set when logging in with `auth_method == "gcp"`, and support discovering both legacy and current `agy` OAuth client secret SHA-256 fingerprints.
5. Persist `project_id` and `region` on `registry.Account`, scope GCP account IDs and matching by project ID so a single Google user can maintain separate consumer and per-project GCP accounts, and allow `Resolve` to match by `project_id`.

## Consequences

- **Good:** Users can add and switch between personal (`consumer`) and work (`gcp` Pay As You Go) accounts in a single command (`agy-auth switch work` or `agy-auth switch <project-id>`).
- **Neutral:** Existing consumer account IDs and snapshots remain unchanged.
- **Bad:** None identified.

## Confirmation

- Contract fixture `harness/fixtures/credential.gcp.json` verified by `internal/harness/contracts`.
- Unit and CLI integration tests in `internal/agy`, `internal/oauth`, `internal/registry`, `internal/usage`, and `internal/cli`.
- `make verify-full` (`go run ./cmd/harness verify --profile full`) passes.

## Links

- [Execution plan](../exec-plans/completed/2026-10-30_gcp-paygo-accounts.md)
- [Internals](../internals.md)
