resource "featureflip_project" "web" {
  key         = "web"
  name        = "Web App"
  description = "Customer-facing web application"

  # Temporary flags with no expires_at of their own count as expired after 90 days.
  default_flag_lifetime_days = 90
}
