resource "pulse_kv_entry" "quiet_hours" {
  bucket = pulse_kv_bucket.settings.name
  key    = "notifications.quiet_hours"
  value  = jsonencode({ from = "22:00", to = "07:00" })
}

# Binary values are base64.
resource "pulse_kv_entry" "floor_plan" {
  bucket   = pulse_kv_bucket.settings.name
  key      = "maps.floor_plan"
  value    = filebase64("${path.module}/floor-plan.png")
  encoding = "base64"
}
