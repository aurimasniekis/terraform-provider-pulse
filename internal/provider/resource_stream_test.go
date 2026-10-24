package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccStream(t *testing.T) {
	testAccPreCheck(t)
	name := randName("st")
	subject := "app." + name[3:]
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_stream", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetStream(ctx, a["name"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: streamConfig(name, subject, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_stream.s", "name", name),
					resource.TestCheckResourceAttr("pulse_stream.s", "storage", "file"),
					resource.TestCheckResourceAttr("pulse_stream.s", "discard", "old"),
					resource.TestCheckResourceAttr("pulse_stream.s", "max_bytes", "-1"),
					resource.TestCheckResourceAttr("pulse_stream.s", "duplicates_seconds", "120"),
					resource.TestCheckResourceAttr("pulse_stream.s", "subjects.#", "1"),
				),
			},
			{
				Config: streamConfig(name, subject, `
  description        = "Updated"
  discard            = "new"
  max_bytes          = 10485760
  max_msgs           = 1000
  max_age_seconds    = 86400
  duplicates_seconds = 60
  compression        = true
  allow_direct       = true
`),
				ConfigPlanChecks: expectUpdate("pulse_stream.s"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_stream.s", "description", "Updated"),
					resource.TestCheckResourceAttr("pulse_stream.s", "discard", "new"),
					resource.TestCheckResourceAttr("pulse_stream.s", "max_bytes", "10485760"),
					resource.TestCheckResourceAttr("pulse_stream.s", "duplicates_seconds", "60"),
					resource.TestCheckResourceAttr("pulse_stream.s", "compression", "true"),
					resource.TestCheckResourceAttr("pulse_stream.s", "allow_direct", "true"),
				),
			},
			{ResourceName: "pulse_stream.s", ImportState: true, ImportStateVerify: true, ImportStateId: name,
				ImportStateVerifyIdentifierAttribute: "name"},
			// Drift: deleted out of band → recreated.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteStream(ctx, name) }),
				Config:    streamConfig(name, subject, ""), ConfigPlanChecks: expectRecreate("pulse_stream.s"),
			},
			// Storage cannot change in place.
			{
				Config:           streamConfig(name, subject, `  storage = "memory"`),
				ConfigPlanChecks: expectReplace("pulse_stream.s"),
				Check:            resource.TestCheckResourceAttr("pulse_stream.s", "storage", "memory"),
			},
		},
	})
}

func TestAccStreamReservedSubject(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      streamConfig(randName("st"), "evt.app.thing.created", ""),
			ExpectError: regexpMust(`overlaps reserved`),
		}},
	})
}

func streamConfig(name, subject, extra string) string {
	return fmt.Sprintf(`
resource "pulse_stream" "s" {
  name     = %q
  subjects = [%q]
%s
}
`, name, subject+".>", extra)
}
