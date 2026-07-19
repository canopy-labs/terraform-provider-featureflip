# Terraform Provider for FeatureFlip

Manage [FeatureFlip](https://featureflip.io) projects, environments, feature flags, targeting rules, segments, and SDK keys as code.

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
  Per-token rate limit is 300 req/min, so avoid running the full suite back-to-back; prefer
  `make testacc TESTARGS='-run TestAccX'`.

## Releasing

Push a `vX.Y.Z` tag on `main`. `.github/workflows/release.yml` builds, GPG-signs, and publishes the
release via GoReleaser; the Terraform Registry ingests it.
