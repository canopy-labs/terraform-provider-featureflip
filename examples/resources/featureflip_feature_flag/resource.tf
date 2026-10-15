# Boolean flags get true/false variations automatically.
resource "featureflip_feature_flag" "new_checkout" {
  project = featureflip_project.web.key
  key     = "new-checkout"
  name    = "New Checkout Flow"
  type    = "Boolean"
  tags    = ["checkout"]
}

# Non-Boolean flags declare their variations inline.
resource "featureflip_feature_flag" "banner_text" {
  project = featureflip_project.web.key
  key     = "banner-text"
  name    = "Banner Text"
  type    = "String"

  variations = [
    { key = "control", name = "Control", value = "Welcome!" },
    { key = "variant", name = "Variant", value = "Hi there!" },
  ]
}

# A temporary flag with an expiry date. Nothing changes at evaluation time
# when the date passes; the flag is reported as stale so it gets cleaned up.
resource "featureflip_feature_flag" "holiday_banner" {
  project    = featureflip_project.web.key
  key        = "holiday-banner"
  name       = "Holiday Banner"
  type       = "Boolean"
  expires_at = "2027-01-31T00:00:00Z"
}

# A kill switch that is meant to stay: permanent flags are never reported for
# cleanup. owner_email names the member who gets the flag's cleanup notices.
resource "featureflip_feature_flag" "payments_kill_switch" {
  project     = featureflip_project.web.key
  key         = "payments-kill-switch"
  name        = "Payments Kill Switch"
  type        = "Boolean"
  permanent   = true
  owner_email = "oncall-lead@example.com"
}

# owner_email and permanent are opt-in: omit them to leave the values to the
# dashboard (and, for owner_email, to the API's default of the creating user).
# Set them explicitly to manage them; owner_email = "" keeps a flag unowned.
resource "featureflip_feature_flag" "experiment" {
  project     = featureflip_project.web.key
  key         = "search-ranking-v2"
  name        = "Search Ranking v2"
  type        = "Boolean"
  owner_email = ""
  permanent   = false
}
