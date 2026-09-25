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
