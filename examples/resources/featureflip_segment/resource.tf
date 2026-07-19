resource "featureflip_segment" "beta_users" {
  project = featureflip_project.web.key
  key     = "beta-users"
  name    = "Beta Users"

  conditions = [
    { attribute = "email", operator = "EndsWith", values = ["@acme.io"] },
  ]
}
