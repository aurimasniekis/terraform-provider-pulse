package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccAlertRule(t *testing.T) {
	testAccPreCheck(t)
	name := randName("Rule")
	const res = "pulse_alert_rule.r"
	var id string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_alert_rule", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetAlertRule(ctx, a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: alertRuleConfig(name, `
  op             = "above"
  threshold      = 100
  window_minutes = 5
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(res, "id", regexpMust(`^custom_[a-z0-9]+$`)),
					resource.TestCheckResourceAttr(res, "name", name),
					resource.TestCheckResourceAttr(res, "severity", "warning"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "for_seconds", "0"),
					resource.TestCheckResourceAttr(res, "stream", "EVENTS"),
					resource.TestCheckResourceAttr(res, "dimension", "total"),
					resource.TestCheckNoResourceAttr(res, "key"),
					resource.TestCheckResourceAttr(res, "metric", "msgs"),
					resource.TestCheckResourceAttr(res, "threshold", "100"),
					resource.TestCheckResourceAttr(res, "description", "Fires when events for all of EVENTS over 5 min is above 100."),
					capture(res, "id", &id),
				),
			},
			{
				Config: alertRuleConfig(name+" renamed", `
  severity       = "critical"
  enabled        = false
  for_seconds    = 120
  dimension      = "prefix"
  key            = "evt.tfacc"
  metric         = "bytes"
  op             = "below"
  threshold      = 1.5
  window_minutes = 10
`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "name", name+" renamed"),
					resource.TestCheckResourceAttr(res, "severity", "critical"),
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckResourceAttr(res, "for_seconds", "120"),
					resource.TestCheckResourceAttr(res, "dimension", "prefix"),
					resource.TestCheckResourceAttr(res, "key", "evt.tfacc"),
					resource.TestCheckResourceAttr(res, "metric", "bytes"),
					resource.TestCheckResourceAttr(res, "op", "below"),
					resource.TestCheckResourceAttr(res, "threshold", "1.5"),
					resource.TestCheckResourceAttr(res, "window_minutes", "10"),
					resource.TestCheckResourceAttr(res, "description", "Fires when payload bytes for evt.tfacc.> in EVENTS over 10 min is below 1.5."),
					attrIs(res, "id", &id),
				),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true},
			// Drift: deleted out of band → recreated.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteAlertRule(ctx, id) }),
				Config: alertRuleConfig(name, `
  dimension      = "type"
  key            = "tfacc.ping"
  op             = "above"
  threshold      = 0
  window_minutes = 60
`),
				ConfigPlanChecks: expectRecreate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "dimension", "type"),
					resource.TestCheckResourceAttr(res, "threshold", "0"),
					resource.TestCheckResourceAttr(res, "severity", "warning"),
				),
			},
		},
	})
}

func TestAccAlertRuleInvalid(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: alertRuleConfig("x", `
  key            = "evt.tfacc"
  op             = "above"
  threshold      = 1
  window_minutes = 5
`), ExpectError: regexpMust(`key must be unset`)},
			{Config: alertRuleConfig("x", `
  dimension      = "prefix"
  op             = "above"
  threshold      = 1
  window_minutes = 5
`), ExpectError: regexpMust(`key is required`)},
			// Three-token prefixes are counted per hour: Pulse refuses a 5-minute window.
			{Config: alertRuleConfig("x", `
  dimension      = "prefix"
  key            = "evt.tfacc.thing"
  op             = "above"
  threshold      = 1
  window_minutes = 5
`), ExpectError: regexpMust(`multiple`)},
		},
	})
}

func alertRuleConfig(name, extra string) string {
	return fmt.Sprintf(`
resource "pulse_alert_rule" "r" {
  name = %q
%s
}
`, name, extra)
}
