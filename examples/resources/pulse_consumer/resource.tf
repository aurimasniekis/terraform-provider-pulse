# The shop's order processor replays every order event, then follows new ones, resuming where it stopped.
resource "pulse_client" "orders" {
  name = "order-processor"
  grants = [
    { action = "durable", subject = "evt.shop.order.>", name = "orders" },
  ]
}

resource "pulse_consumer" "orders" {
  stream           = "EVENTS"
  name             = "${pulse_client.orders.id}_orders" # what the client's durable grant allows
  filter_subjects  = ["evt.shop.order.>"]
  deliver_policy   = "by_start_time"
  opt_start_time   = "2026-01-01T00:00:00Z"
  ack_wait_seconds = 60
  max_deliver      = 10
}
