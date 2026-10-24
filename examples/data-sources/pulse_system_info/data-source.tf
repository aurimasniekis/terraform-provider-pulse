data "pulse_system_info" "this" {}

# Hand the connection details to a tool's configuration.
output "nats_url" {
  value = data.pulse_system_info.this.nats_url
}
