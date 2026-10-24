# SSO-only dashboard. Apply this only after an administrator completed a test login on an enabled
# provider and has a linked identity (pulse_user_identity); Pulse refuses it otherwise.
resource "pulse_auth_settings" "this" {
  local_login_disabled = true

  depends_on = [pulse_user_identity.alice]
}
