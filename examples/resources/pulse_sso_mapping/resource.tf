# Members of the "pulse-admins" group get the administrator role, "pulse-ops" the operator role.
resource "pulse_sso_mapping" "admins" {
  provider_id = pulse_sso_provider.keycloak.id
  group_value = "pulse-admins"
  role        = "administrator"
}

resource "pulse_sso_mapping" "ops" {
  provider_id = pulse_sso_provider.keycloak.id
  group_value = "pulse-ops"
  role        = "operator"
}
