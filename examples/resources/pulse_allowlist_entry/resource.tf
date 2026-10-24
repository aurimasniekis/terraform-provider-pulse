resource "pulse_allowlist_entry" "home" {
  network = "198.51.100.7"
  note    = "Home connection"
}

resource "pulse_allowlist_entry" "office_v6" {
  network = "2001:db8:1200::/48"
  note    = "Office IPv6"
}
