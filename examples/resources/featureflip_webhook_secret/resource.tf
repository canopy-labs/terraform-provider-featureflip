# Rotating a webhook's signing secret without dropping deliveries.
#
# Every active secret signs every delivery, so the receiver can switch to the
# new secret while the old one is still valid.

# Step 1: add a second secret, apply, and update the receiver to verify with it.
resource "featureflip_webhook_secret" "next" {
  webhook_id = featureflip_webhook.flag_changes.id
}

# Step 2: adopt the secret the webhook was created with, and apply.
import {
  to = featureflip_webhook_secret.original
  id = "${featureflip_webhook.flag_changes.id}/${featureflip_webhook.flag_changes.secret_id}"
}

resource "featureflip_webhook_secret" "original" {
  webhook_id = featureflip_webhook.flag_changes.id
}

# Step 3: delete the import block and the "original" resource, then apply.
# Destroying the resource retires the original secret.

output "webhook_signing_secret" {
  value     = featureflip_webhook_secret.next.secret
  sensitive = true
}
