# GitHub push events for a repository. Pulse generates the secret; paste it and the URL into the
# repository's webhook settings (Content type: application/json).
resource "pulse_inbound_webhook" "github" {
  name = "github-homelab"
  kind = "github"
}

output "github_webhook_url" {
  value = pulse_inbound_webhook.github.url
}

output "github_webhook_secret" {
  value     = pulse_inbound_webhook.github.secret
  sensitive = true
}

# A generic HMAC sender with a timestamped signature, publishing on a custom subject.
resource "pulse_inbound_webhook" "sensor" {
  name    = "sensor-gateway"
  kind    = "hmac"
  subject = "evt.home.sensor.reading"
  secret  = var.sensor_hmac_key

  config = {
    header           = "X-Signature"
    prefix           = "sha256="
    timestamp_header = "X-Timestamp"
    tolerance_sec    = 120
    type             = "home.sensor.reading"
  }
}

variable "sensor_hmac_key" {
  type      = string
  sensitive = true
}
