# A home-automation bot that publishes door events, replays them with its own durable consumer and stores large
# payloads in the shared blob store.
resource "pulse_client" "door_bot" {
  name        = "door-bot"
  description = "Door sensors"
  tags        = ["home"]

  grants = [
    { action = "publish", subject = "evt.home.door.>" },
    { action = "durable", subject = "evt.home.>", name = "doors" },
    { action = "blobs" },
  ]
}

# Hand the .creds file to the tool, e.g. as a file on its host.
resource "local_sensitive_file" "door_bot_creds" {
  content         = pulse_client.door_bot.creds
  filename        = "${path.module}/door-bot.creds"
  file_permission = "0600"
}
