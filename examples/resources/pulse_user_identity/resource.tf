resource "pulse_user" "alice" {
  email        = "alice@example.com"
  display_name = "Alice"
  roles        = ["administrator"]
}

# Alice signs in with the company identity provider; her subject (the "sub" claim) there is fixed.
resource "pulse_user_identity" "alice" {
  user_id     = pulse_user.alice.id
  provider_id = pulse_sso_provider.company.id
  subject     = "248289761001"
}
