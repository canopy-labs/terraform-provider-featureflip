data "featureflip_webhook_event_types" "all" {}

# Subscribe to every flag event, but nothing else.
resource "featureflip_webhook" "flags_only" {
  name        = "flags-only"
  url         = "https://hooks.example.com/featureflip"
  event_types = [for t in data.featureflip_webhook_event_types.all.event_types : t if startswith(t, "flag.")]
}
