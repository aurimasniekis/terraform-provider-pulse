package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// checkBuiltinDefaults verifies that destroying the resource restored the rule's built-in settings.
func checkBuiltinDefaults(t *testing.T, kind string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		a, err := adminClient(t).GetAlertRule(context.Background(), kind)
		if err != nil {
			return err
		}
		d := builtinAlertDefaults[kind]
		if !a.Enabled || a.Severity != d.severity || a.ForSeconds != d.forSeconds || a.Params != d.params {
			return fmt.Errorf("%s not restored to defaults: %+v", kind, a)
		}
		return nil
	}
}

func TestAccBuiltinAlertRule(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_builtin_alert_rule.r"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkBuiltinDefaults(t, "ban_storm"),
		Steps: []resource.TestStep{
			{
				Config: builtinAlertConfig("ban_storm", `
  severity = "critical"
  params   = { count = 20 }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "id", "ban_storm"),
					resource.TestCheckResourceAttr(res, "name", "Ban storm"),
					resource.TestCheckResourceAttrSet(res, "description"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "severity", "critical"),
					resource.TestCheckResourceAttr(res, "for_seconds", "0"),
					resource.TestCheckResourceAttr(res, "params.count", "20"),
					resource.TestCheckResourceAttr(res, "params.window_minutes", "10"),
					resource.TestCheckNoResourceAttr(res, "params.days"),
				),
			},
			{
				Config: builtinAlertConfig("ban_storm", `
  enabled     = false
  for_seconds = 300
  params      = { count = 5, window_minutes = 30 }
`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckResourceAttr(res, "severity", "warning"),
					resource.TestCheckResourceAttr(res, "for_seconds", "300"),
					resource.TestCheckResourceAttr(res, "params.count", "5"),
					resource.TestCheckResourceAttr(res, "params.window_minutes", "30"),
				),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "ban_storm"},
			// Removing the settings restores the defaults.
			{
				Config:           builtinAlertConfig("ban_storm", ""),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "severity", "warning"),
					resource.TestCheckResourceAttr(res, "for_seconds", "0"),
					resource.TestCheckResourceAttr(res, "params.count", "10"),
					resource.TestCheckResourceAttr(res, "params.window_minutes", "10"),
				),
			},
			{Config: builtinAlertConfig("ban_storm", ""), ConfigPlanChecks: expectNoop(res)},
			// Drift: changed out of band → corrected.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					_, err := c.UpdateAlertRule(ctx, "ban_storm", client.AlertRuleInput{Enabled: false, Severity: "info",
						Params: client.AlertRuleParams{Count: 99, WindowMinutes: 1}})
					return err
				}),
				Config:           builtinAlertConfig("ban_storm", `  severity = "info"`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "severity", "info"),
					resource.TestCheckResourceAttr(res, "params.count", "10"),
				),
			},
		},
	})
}

func TestAccBuiltinAlertRuleNoParams(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_builtin_alert_rule.r"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkBuiltinDefaults(t, "nats_down"),
		Steps: []resource.TestStep{
			{
				Config: builtinAlertConfig("nats_down", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "severity", "critical"),
					resource.TestCheckResourceAttr(res, "for_seconds", "60"),
					resource.TestCheckNoResourceAttr(res, "params.count"),
				),
			},
			{
				Config:           builtinAlertConfig("nats_down", `  for_seconds = 30`),
				ConfigPlanChecks: expectUpdate(res),
				Check:            resource.TestCheckResourceAttr(res, "for_seconds", "30"),
			},
			// Replacing the kind restores nats_down and configures stream_usage.
			{
				Config:           builtinAlertConfig("stream_usage", `  params = { ratio = 0.9 }`),
				ConfigPlanChecks: expectReplace(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkBuiltinDefaults(t, "nats_down"),
					resource.TestCheckResourceAttr(res, "params.ratio", "0.9"),
				),
			},
			{Config: builtinAlertConfig("stream_usage", ""), ConfigPlanChecks: expectUpdate(res),
				Check: resource.TestCheckResourceAttr(res, "params.ratio", "0.85")},
		},
	})
}

func TestAccBuiltinAlertRuleInvalid(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: builtinAlertConfig("disk_forecast", `  params = { count = 3 }`), ExpectError: regexpMust(`does not use params.count`)},
			{Config: builtinAlertConfig("stream_usage", `  params = { ratio = 1.5 }`), ExpectError: regexpMust(`between 0 and 1`)},
			{Config: builtinAlertConfig("threshold", ""), ExpectError: regexpMust(`value must be one of`)},
		},
	})
}

func builtinAlertConfig(kind, extra string) string {
	return fmt.Sprintf(`
resource "pulse_builtin_alert_rule" "r" {
  kind = %q
%s
}
`, kind, extra)
}
