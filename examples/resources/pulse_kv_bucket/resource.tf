# Shared settings for home-automation tools, with the last 5 revisions of every key.
resource "pulse_kv_bucket" "settings" {
  name        = "home-settings"
  description = "Settings read by the home bots"
  history     = 5
}
