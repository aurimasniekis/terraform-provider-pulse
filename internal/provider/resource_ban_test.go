package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func bansGone(t *testing.T) resource.TestCheckFunc {
	return checkGone(t, "pulse_ban", func(ctx context.Context, c *client.Client, a map[string]string) error {
		_, err := c.GetBan(ctx, a["id"])
		return err
	})
}

func TestAccBan(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_ban.b"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             bansGone(t),
		Steps: []resource.TestStep{
			{
				Config: banConfig("203.0.113.7", `  reason = "tfacc"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "id", "203.0.113.7/32"),
					resource.TestCheckResourceAttr(res, "network", "203.0.113.7"),
					resource.TestCheckResourceAttr(res, "reason", "tfacc"),
					resource.TestCheckResourceAttr(res, "duration_hours", "0"),
					resource.TestCheckNoResourceAttr(res, "expires_at"),
				),
			},
			{
				// Spelling the same network differently changes nothing.
				Config:           banConfig("203.0.113.7/32", `  reason = "tfacc"`),
				ConfigPlanChecks: expectUpdate(res),
				Check:            resource.TestCheckResourceAttr(res, "id", "203.0.113.7/32"),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "203.0.113.7"},
			{
				Config:           banConfig("203.0.113.7/32", `  reason = "changed"`),
				ConfigPlanChecks: expectReplace(res),
				Check:            resource.TestCheckResourceAttr(res, "reason", "changed"),
			},
			{
				Config:           banConfig("203.0.113.7/32", "  reason = \"changed\"\n  duration_hours = 2"),
				ConfigPlanChecks: expectReplace(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "duration_hours", "2"),
					resource.TestMatchResourceAttr(res, "expires_at", regexpMust(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`)),
				),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "203.0.113.7/32"},
			// Drift: lifted out of band → banned again.
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.LiftBan(ctx, "203.0.113.7/32") }),
				Config:           banConfig("203.0.113.7/32", "  reason = \"changed\"\n  duration_hours = 2"),
				ConfigPlanChecks: expectRecreate(res),
			},
			{
				Config:           banConfig("198.51.100.0/24", ""),
				ConfigPlanChecks: expectReplace(res),
				Check:            resource.TestCheckResourceAttr(res, "id", "198.51.100.0/24"),
			},
		},
	})
}

func TestAccBanIPv6(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_ban.b"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             bansGone(t),
		Steps: []resource.TestStep{
			{
				// A single IPv6 address bans its /64.
				Config: banConfig("2001:db8:b:1::5", ""),
				Check:  resource.TestCheckResourceAttr(res, "id", "2001:db8:b:1::/64"),
			},
			{Config: banConfig("2001:db8:b:1::9", ""), ConfigPlanChecks: expectUpdate(res)},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "2001:db8:b:1::/64",
				ImportStateVerifyIgnore: []string{"network"}},
		},
	})
}

func TestAccBanRefused(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: banConfig("10.1.2.3", ""), ExpectError: regexpMust(`may not be banned`)},
			{Config: banConfig("203.0.113.300", ""), ExpectError: regexpMust(`not an IP address or CIDR`)},
		},
	})
}

func banConfig(network, extra string) string {
	return fmt.Sprintf(`
resource "pulse_ban" "b" {
  network = %q
%s
}
`, network, extra)
}
