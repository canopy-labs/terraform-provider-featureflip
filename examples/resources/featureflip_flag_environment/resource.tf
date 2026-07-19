resource "featureflip_flag_environment" "new_checkout_prod" {
  project           = featureflip_project.web.key
  flag              = featureflip_feature_flag.new_checkout.key
  environment       = "production"
  enabled           = true
  default_variation = "false"

  # List position is rule priority. When rules is set, Terraform owns ALL
  # rules in this flag-environment — dashboard-created rules are removed.
  rules = [
    {
      description = "Beta users always get the new checkout"
      variation   = "true"
      segment     = featureflip_segment.beta_users.key
    },
    {
      description        = "50% rollout in Germany"
      variation          = "true"
      rollout_percentage = 50
      condition_groups = [
        {
          operator = "And"
          conditions = [
            { attribute = "country", operator = "Equals", values = ["DE"] },
          ]
        },
      ]
    },
  ]
}
