# Warn when the door sensors have been silent for six hours.
resource "pulse_alert_rule" "doors_quiet" {
  name           = "Door sensors quiet"
  dimension      = "prefix"
  key            = "evt.home.door"
  op             = "below"
  threshold      = 1
  window_minutes = 360
}

# Critical when more than 50 MB of payload arrive within 10 minutes.
resource "pulse_alert_rule" "ingest_burst" {
  name           = "Ingest burst"
  severity       = "critical"
  metric         = "bytes"
  op             = "above"
  threshold      = 50000000
  window_minutes = 10
  for_seconds    = 120
}
