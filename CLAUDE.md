# terraform-provider-featureflip

Terraform provider for Featureflip (Go, terraform-plugin-framework, protocol v6). This is the
canonical public repo; the Terraform Registry requires it be named `terraform-provider-<type>`
(Makefile pins `--provider-name`; .goreleaser.yml pins `project_name`; go.mod pins the module path).
It talks to Featureflip's public `/api/v1` Management API and shares no code with the backend —
the API is the only contract between them.

## Commands

- `make test` — unit tests, no network (httptest only)
- `make testacc` — live acceptance tests (needs terraform on PATH + `FEATUREFLIP_*` env). Do NOT run the full suite back-to-back: per-token rate limit is 300 req/min → self-inflicted 429s. Prefer `make testacc TESTARGS='-run TestAccX'`
- `make docs` — regenerate docs/ from schemas + examples/ (commit the output; CI fails on drift). `docs/` is the **Terraform Registry** doc tree
- `./scripts/local-api.sh` — boot + seed a local API for acceptance tests. Standalone repo → set `FEATUREFLIP_REPO` to a feature-flagger monorepo checkout (supplies docker-compose.yml); writes .env.acceptance

## Upstream API contract (source of truth)

- OpenAPI: `apps/management-api/public-v1-openapi.json` in the private `canopy-labs/feature-flagger`
  monorepo (regenerated there via `make openapi`). This provider's client is hand-maintained against it
- Guard codes (LAST_ENVIRONMENT, FLAG_HAS_DEPENDENTS, VARIATION_HAS_DEPENDENTS) arrive inside the
  validation envelope's `fields` values, NOT `message` — detect via client.HasCode()

## Live-API behaviors the code works around (don't "fix" these)

- New projects auto-seed development/staging/production environments — acceptance tests must
  use other env keys (convention: "acctest")
- Condition-list order is NOT preserved by the API — Read paths realign to plan/state order via
  conditions.go reorderConditionsLike / reorderConditionGroupsLike
- Optional strings normalize to "" server-side — all write paths carry null-vs-"" guards
- `null` vs `[]` on list/set fields = leave-unchanged vs clear (tags, prerequisites rely on this;
  client structs deliberately omit omitempty)
- Configure fail-fasts via GET /orgs/{org} (validates the org slug and token-org binding; /me works
  for both token types since upstream #1779 but tells you nothing about the configured org)
- Signup rejects paid plans without a billing interval (upstream #1783) — seed.sh signs up plain
  (Solo) and bumps the org to Business via psql

## Conventions

- Resource template: 404 on Read → RemoveResource; Delete tolerates 404; key/type = RequiresReplace;
  key-qualified error messages; re-GET after writes (except sdk_key: create response is authoritative,
  update returns no body)
- sdk_key plaintext: create-response only; never in Read, never in any diagnostic string
