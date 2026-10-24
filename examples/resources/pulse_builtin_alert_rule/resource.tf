# Backups run nightly: alert after two missed nights instead of the default 36 hours.
resource "pulse_builtin_alert_rule" "backup_stale" {
  kind   = "backup_stale"
  params = { hours = 50 }
}

# Certificates renew 30 days ahead; only a warning when 7 days are left.
resource "pulse_builtin_alert_rule" "cert_expiry" {
  kind     = "cert_expiry"
  severity = "warning"
  params   = { days = 7 }
}

# No interest in slow consumer notices.
resource "pulse_builtin_alert_rule" "slow_consumers" {
  kind    = "slow_consumers"
  enabled = false
}
