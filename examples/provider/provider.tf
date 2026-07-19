terraform {
  required_providers {
    featureflip = {
      source  = "canopy-labs/featureflip"
      version = "~> 0.1"
    }
  }
}

provider "featureflip" {
  organization = "acme-corp"
  # token via FEATUREFLIP_TOKEN environment variable (recommended),
  # or: token = var.featureflip_token
}
