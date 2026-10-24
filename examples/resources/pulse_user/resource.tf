# An operator who signs in with SSO. The user has no password; link their identity with
# pulse_user_identity, or issue a password link from the dashboard's Users page.
resource "pulse_user" "alice" {
  email        = "alice@example.com"
  display_name = "Alice"
  roles        = ["operator"]
}

# A read-only account, disabled for now.
resource "pulse_user" "contractor" {
  email        = "contractor@example.com"
  display_name = "Contractor"
  roles        = ["viewer"]
  mfa_required = true
  enabled      = false
}
