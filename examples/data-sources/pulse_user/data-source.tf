# A user created on the dashboard or by just-in-time provisioning.
data "pulse_user" "bob" {
  email = "bob@example.com"
}

output "bob_roles" {
  value = data.pulse_user.bob.all_roles
}
