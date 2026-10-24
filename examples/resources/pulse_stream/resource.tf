# Metrics from the home lab, kept for 30 days and at most 1 GiB.
resource "pulse_stream" "metrics" {
  name            = "METRICS"
  description     = "Home lab metrics, short retention"
  subjects        = ["metrics.>"]
  max_bytes       = 1073741824
  max_age_seconds = 2592000
  compression     = true
}
