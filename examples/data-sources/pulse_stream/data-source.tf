data "pulse_stream" "events" {
  name = "EVENTS"
}

output "events_max_bytes" {
  value = data.pulse_stream.events.max_bytes
}
