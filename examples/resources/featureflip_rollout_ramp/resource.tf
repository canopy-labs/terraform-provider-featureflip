resource "featureflip_flag_environment" "checkout_prod" {
  project     = featureflip_project.web.key
  flag        = featureflip_feature_flag.new_checkout.key
  environment = "production"
  enabled     = true

  rules = [{
    description        = "Logged-in users"
    variation          = "true"
    rollout_percentage = 5
    condition_groups = [{
      operator   = "And"
      conditions = [{ attribute = "logged_in", operator = "Equals", values = ["true"] }]
    }]
  }]

  # The ramp raises this rule's rollout as it goes. Without ignore_changes the
  # next apply would set it back, and editing the rule pauses the ramp.
  lifecycle {
    ignore_changes = [rules[0].rollout_percentage]
  }
}

# 5% for a day, then 25% and 50% for a day each, then everyone.
resource "featureflip_rollout_ramp" "checkout" {
  project     = featureflip_project.web.key
  flag        = featureflip_feature_flag.new_checkout.key
  environment = "production"
  rule_id     = featureflip_flag_environment.checkout_prod.rules[0].id

  steps = [
    { percent = 5, hold_minutes = 1440 },
    { percent = 25, hold_minutes = 1440 },
    { percent = 50, hold_minutes = 1440 },
    { percent = 100, hold_minutes = 0 },
  ]

  # On destroy while the ramp is still running, send everyone back to 0%
  # instead of leaving the rule where the ramp got to.
  abort_percent = 0
}
