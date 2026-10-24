# A client created in the dashboard: give it a consumer managed here.
data "pulse_client" "legacy_bot" {
  name = "legacy-bot"
}

resource "pulse_consumer" "legacy_bot" {
  name            = "${data.pulse_client.legacy_bot.id}_main"
  filter_subjects = ["evt.legacy.>"]
}
