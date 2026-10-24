# Forward door events to Home Assistant. Pulse signs each request with the generated secret
# (Pulse-Signature header); change secret_version to rotate it.
resource "pulse_outbound_webhook" "home_assistant" {
  name           = "home-assistant"
  url            = "https://ha.example.com/api/webhook/pulse-doors"
  filter         = "evt.home.door.>"
  secret_version = "1"

  headers = {
    Authorization = "Bearer ${var.ha_token}"
  }
}

variable "ha_token" {
  type      = string
  sensitive = true
}
