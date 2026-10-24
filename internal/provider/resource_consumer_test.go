package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccConsumer(t *testing.T) {
	testAccPreCheck(t)
	cl := randName("cc")
	var name string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_consumer", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetConsumer(ctx, a["stream"], a["name"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: consumerConfig(cl, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_consumer.c", "stream", "EVENTS"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "deliver_policy", "all"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "ack_wait_seconds", "30"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "max_deliver", "-1"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "max_ack_pending", "1000"),
					resource.TestMatchResourceAttr("pulse_consumer.c", "name", regexpMust(`^[a-z0-9]+_orders$`)),
					capture("pulse_consumer.c", "name", &name),
				),
			},
			{
				Config: consumerConfig(cl, `
  description      = "Order processor"
  filter_subjects  = ["evt.shop.order.>", "evt.shop.refund.>"]
  ack_wait_seconds = 120
  max_deliver      = 5
  max_ack_pending  = 50
`),
				ConfigPlanChecks: expectUpdate("pulse_consumer.c"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_consumer.c", "filter_subjects.#", "2"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "ack_wait_seconds", "120"),
					resource.TestCheckResourceAttr("pulse_consumer.c", "max_deliver", "5"),
				),
			},
			{ResourceName: "pulse_consumer.c", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: importID("pulse_consumer.c", "stream", "name"),
				ImportStateVerifyIdentifierAttribute: "id"},
			// The start position cannot change in place.
			{
				Config: consumerConfig(cl, `
  deliver_policy = "by_start_time"
  opt_start_time = "2026-01-01T00:00:00Z"
`),
				ConfigPlanChecks: expectReplace("pulse_consumer.c"),
				Check:            resource.TestCheckResourceAttr("pulse_consumer.c", "opt_start_time", "2026-01-01T00:00:00Z"),
			},
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteConsumer(ctx, "EVENTS", name) }),
				Config: consumerConfig(cl, `
  deliver_policy = "by_start_time"
  opt_start_time = "2026-01-01T00:00:00Z"
`),
				ConfigPlanChecks: expectRecreate("pulse_consumer.c"),
			},
		},
	})
}

func TestAccConsumerValidation(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: consumerConfig(randName("cc"), `  deliver_policy = "by_start_sequence"`), ExpectError: regexpMust(`needs opt_start_seq`)},
			{Config: consumerConfig(randName("cc"), `  opt_start_seq = 5`), ExpectError: regexpMust(`opt_start_seq needs`)},
			{Config: consumerConfig(randName("cc"), `  filter_subjects = ["app.other.>"]`), ExpectError: regexpMust(`matches none of the stream`)},
		},
	})
}

func consumerConfig(clientName, extra string) string {
	filter := `  filter_subjects = ["evt.shop.>"]`
	if containsLine(extra, "filter_subjects") {
		filter = ""
	}
	return fmt.Sprintf(`
resource "pulse_client" "c" {
  name   = %q
  grants = [{ action = "durable", subject = "evt.shop.>", name = "orders" }]
}

resource "pulse_consumer" "c" {
  name = "${pulse_client.c.id}_orders"
%s
%s
}
`, clientName, filter, extra)
}

func containsLine(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
