# A home lab on Pulse: tools with least-privilege credentials, a consumer that replays history, webhooks in and out,
# shared settings, an alert, a firewall entry and dashboard access for the household through single sign-on.

terraform {
  required_providers {
    pulse = {
      source = "aurimasniekis/pulse"
    }
  }
}

# PULSE_ADMIN_URL and PULSE_ADMIN_TOKEN come from the environment (through `ssh -L 8080:127.0.0.1:8080 <server>`).
provider "pulse" {}

data "pulse_system_info" "this" {}

# ---- tools ----

resource "pulse_client" "door_bot" {
  name        = "door-bot"
  description = "Door and window sensors"
  tags        = ["home"]
  grants = [
    { action = "publish", subject = "evt.home.door.>" },
    { action = "publish", subject = "evt.home.window.>" },
  ]
}

resource "pulse_client" "notifier" {
  name        = "notifier"
  description = "Sends phone notifications for home events"
  grants = [
    { action = "durable", subject = "evt.home.>", name = "alerts" },
    { action = "blobs" },
  ]
}

resource "pulse_consumer" "notifier" {
  name             = "${pulse_client.notifier.id}_alerts"
  description      = "Home events the notifier has not handled yet"
  filter_subjects  = ["evt.home.>"]
  deliver_policy   = "new"
  ack_wait_seconds = 60
  max_deliver      = 20
}

resource "pulse_http_token" "ci" {
  name            = "ci-deploys"
  description     = "Deployment events from CI"
  grants          = [{ action = "publish", subject = "evt.ci.deploy.>" }]
  cidrs           = ["198.51.100.0/24"]
  expires_in_days = 90
}

# ---- storage ----

resource "pulse_stream" "metrics" {
  name            = "METRICS"
  description     = "Sensor readings, kept for 30 days"
  subjects        = ["metrics.>"]
  max_bytes       = 1073741824
  max_age_seconds = 2592000
  compression     = true
}

resource "pulse_kv_bucket" "settings" {
  name        = "home-settings"
  description = "Settings the home tools read"
  history     = 5
}

resource "pulse_kv_entry" "quiet_hours" {
  bucket = pulse_kv_bucket.settings.name
  key    = "notifications.quiet_hours"
  value  = jsonencode({ from = "22:00", to = "07:00" })
}

resource "pulse_object_bucket" "snapshots" {
  name        = "camera-snapshots"
  description = "Doorbell camera snapshots"
  ttl_seconds = 604800
}

# ---- webhooks ----

resource "pulse_inbound_webhook" "github" {
  name = "github"
  kind = "github"
}

resource "pulse_outbound_webhook" "home_assistant" {
  name   = "home-assistant"
  url    = "https://ha.home.example.com/api/webhook/pulse"
  filter = "evt.home.door.>"
}

# ---- monitoring and security ----

resource "pulse_alert_rule" "quiet_doors" {
  name           = "No door events for a day"
  severity       = "warning"
  key            = "home.door"
  dimension      = "prefix"
  op             = "below"
  threshold      = 1
  window_minutes = 1440
}

resource "pulse_builtin_alert_rule" "disk" {
  kind     = "disk_forecast"
  severity = "critical"
  params = {
    days = 14
  }
}

resource "pulse_allowlist_entry" "office" {
  network = "203.0.113.0/24"
  note    = "Office VPN: never banned"
}

# ---- dashboard access ----

resource "pulse_sso_provider" "family" {
  slug         = "family"
  display_name = "Family account"
  issuer       = "https://auth.example.com"
  client_id    = "pulse"
  groups_claim = "groups"
}

resource "pulse_sso_mapping" "parents" {
  provider_id = pulse_sso_provider.family.id
  group_value = "parents"
  role        = "operator"
}

resource "pulse_sso_mapping" "kids" {
  provider_id = pulse_sso_provider.family.id
  group_value = "kids"
  role        = "viewer"
}

resource "pulse_user" "alex" {
  email        = "alex@example.com"
  display_name = "Alex"
  roles        = ["auditor"]
}

resource "pulse_user_identity" "alex" {
  user_id     = pulse_user.alex.id
  provider_id = pulse_sso_provider.family.id
  subject     = "alex"
}

output "nats_url" {
  value = data.pulse_system_info.this.nats_url
}

output "inbound_github_url" {
  value = pulse_inbound_webhook.github.url
}

output "door_bot_creds" {
  value     = pulse_client.door_bot.creds
  sensitive = true
}
