# A permanent ban for a scanner network.
resource "pulse_ban" "scanner" {
  network = "203.0.113.0/24"
  reason  = "Port scans against the public listener"
}

# A single IPv6 address bans its whole /64 (id = "2001:db8:5:6::/64").
resource "pulse_ban" "abuser" {
  network        = "2001:db8:5:6::17"
  reason         = "Credential stuffing"
  duration_hours = 0
}
