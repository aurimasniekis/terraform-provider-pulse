package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// Allowlist tests use 192.0.2.0/24 and 2001:db8:a::/48; ban tests use other documentation ranges, because an
// allowlist entry lifts bans inside it.

func allowlistGone(t *testing.T) resource.TestCheckFunc {
	return checkGone(t, "pulse_allowlist_entry", func(ctx context.Context, c *client.Client, a map[string]string) error {
		_, err := c.GetAllowEntry(ctx, a["id"])
		return err
	})
}

func TestAccAllowlistEntry(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_allowlist_entry.a"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             allowlistGone(t),
		Steps: []resource.TestStep{
			{
				Config: allowlistConfig("192.0.2.10", `  note = "office"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "id", "192.0.2.10/32"),
					resource.TestCheckResourceAttr(res, "network", "192.0.2.10"),
					resource.TestCheckResourceAttr(res, "note", "office"),
				),
			},
			{
				// Same network spelled differently, new note: in place.
				Config:           allowlistConfig("192.0.2.10/32", `  note = "home"`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "id", "192.0.2.10/32"),
					resource.TestCheckResourceAttr(res, "note", "home"),
				),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "192.0.2.10"},
			// Drift: removed out of band → recreated.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error { return c.DisallowNetwork(ctx, "192.0.2.10/32") }),
				Config:    allowlistConfig("192.0.2.10/32", `  note = "home"`), ConfigPlanChecks: expectRecreate(res),
			},
			{
				// A different network forces a new entry; host bits are cleared.
				Config:           allowlistConfig("192.0.2.130/25", ""),
				ConfigPlanChecks: expectReplace(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "id", "192.0.2.128/25"),
					resource.TestCheckResourceAttr(res, "note", ""),
				),
			},
		},
	})
}

func TestAccAllowlistEntryIPv6(t *testing.T) {
	testAccPreCheck(t)
	const res = "pulse_allowlist_entry.a"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             allowlistGone(t),
		Steps: []resource.TestStep{
			{
				Config: allowlistConfig("2001:DB8:A:0:0:0:0:1", ""),
				Check:  resource.TestCheckResourceAttr(res, "id", "2001:db8:a::1/128"),
			},
			{
				Config:           allowlistConfig("2001:db8:a:1::/48", ""),
				ConfigPlanChecks: expectReplace(res),
				Check:            resource.TestCheckResourceAttr(res, "id", "2001:db8:a::/48"),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true, ImportStateId: "2001:db8:a::/48",
				ImportStateVerifyIgnore: []string{"network"}},
		},
	})
}

func TestAccAllowlistEntryExisting(t *testing.T) {
	testAccPreCheck(t)
	const network = "192.0.2.77/32"
	t.Cleanup(func() { _ = adminClient(t).DisallowNetwork(context.Background(), network) })
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig:   oob(t, func(ctx context.Context, c *client.Client) error { return c.AllowNetwork(ctx, network, "manual") }),
				Config:      allowlistConfig("192.0.2.77", ""),
				ExpectError: regexpMust(`already on the allowlist`),
			},
			{Config: allowlistConfig("not-an-ip", ""), ExpectError: regexpMust(`not an IP address or CIDR`)},
		},
	})
}

func allowlistConfig(network, extra string) string {
	return fmt.Sprintf(`
resource "pulse_allowlist_entry" "a" {
  network = %q
%s
}
`, network, extra)
}
