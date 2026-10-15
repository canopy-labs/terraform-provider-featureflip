# Terraform Provider for Featureflip

Manage [Featureflip](https://featureflip.io) projects, environments, feature flags, targeting rules, segments, SDK keys, and webhooks as code.

## Usage

```hcl
terraform {
  required_providers {
    featureflip = {
      source  = "canopy-labs/featureflip"
      version = "~> 0.1"
    }
  }
}

provider "featureflip" {
  organization = "acme-corp" # or FEATUREFLIP_ORGANIZATION
  # token via FEATUREFLIP_TOKEN (create a service token in the dashboard)
}
```

Full documentation: [registry.terraform.io/providers/canopy-labs/featureflip](https://registry.terraform.io/providers/canopy-labs/featureflip/latest/docs)

## Upgrading to v0.4.0

v0.4.0 adds three lifecycle attributes: `owner_email` and `permanent` on `featureflip_feature_flag`,
and `default_flag_lifetime_days` on `featureflip_project`. All three are opt-in, so an existing
configuration plans with no changes after the upgrade:

- **Omitted means unmanaged.** Terraform leaves the value alone: an owner, permanent marker or
  default lifetime set in the dashboard is kept, and so is the owner the API assigns when a flag is
  created with a personal access token (the creating user). Flags created with a service token
  start unowned.
- **To clear a value, set it explicitly:** `owner_email = ""` leaves the flag unowned,
  `permanent = false` makes it temporary, and `default_flag_lifetime_days = 0` removes the project
  default. In state, an unowned flag reads as `""` and a project without a default as `0`.
- **Removing one of these attributes from config stops managing it**; it does not clear the value.
- **Archived flags are read-only**, apart from clearing the owner, the permanent marker or the
  expiry. A plan that would change anything else on a flag that stays archived fails at plan time;
  set `archived = false` in the same apply to restore it first.

## Development

- `make test` — unit tests (no network; `httptest` only).
- `make docs` — regenerate `docs/` from the provider schemas + `examples/` (commit the output; the
  `docs/` tree is what the Terraform Registry renders and CI fails on drift).
- `make testacc` — acceptance tests against a live API. These create and destroy real resources, so
  they need `FEATUREFLIP_BASE_URL`, `FEATUREFLIP_ORGANIZATION`, and `FEATUREFLIP_TOKEN`.

### Acceptance tests

- **In CI:** `.github/workflows/acceptance.yml` runs against a dedicated test org on the hosted dev
  instance. It's secret-gated (no `pull_request` trigger), so it runs on push to `main`, nightly, and
  manual dispatch — never on fork PRs.
- **Locally against a throwaway stack:** point at a [feature-flagger](https://github.com/canopy-labs)
  monorepo checkout, which supplies the `docker-compose.yml` API stack:
  ```bash
  FEATUREFLIP_REPO=~/src/feature-flagger ./scripts/local-api.sh   # boots + seeds, writes .env.acceptance
  make testacc
  ```
  `seed.sh` also writes `FEATUREFLIP_ACC_OWNER_EMAIL` and `FEATUREFLIP_ACC_MEMBER_TOKEN` (the seeded
  user's email and personal access token). Tests that need them skip when they are unset; the upgrade
  test also downloads v0.3.0 from the Terraform Registry.
  Per-token rate limit is 300 req/min, so avoid running the full suite back-to-back; prefer
  `make testacc TESTARGS='-run TestAccX'`.

## Releasing

Push a `vX.Y.Z` tag on `main`. `.github/workflows/release.yml` builds, GPG-signs, and publishes the
release via GoReleaser; the Terraform Registry ingests it.
