# Camera snapshots, kept for a week and at most 10 GiB.
resource "pulse_object_bucket" "snapshots" {
  name        = "camera-snapshots"
  description = "Doorbell camera snapshots"
  ttl_seconds = 604800
  max_bytes   = 10737418240
}
