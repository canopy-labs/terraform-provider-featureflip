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
