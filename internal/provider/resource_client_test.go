package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccClient(t *testing.T) {
	testAccPreCheck(t)
	name := randName("cl")
	var id, creds1, creds2 string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_client", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetClient(ctx, a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: clientConfig(name, "", `
  grants = [
    { action = "publish", subject = "evt.home.>" },
  ]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_client.c", "name", name),
					resource.TestCheckResourceAttr("pulse_client.c", "status", "active"),
					resource.TestCheckResourceAttr("pulse_client.c", "expires_in_days", "365"),
					resource.TestCheckResourceAttr("pulse_client.c", "grants.#", "1"),
					resource.TestMatchResourceAttr("pulse_client.c", "creds", regexpMust(`-----BEGIN NATS USER JWT-----`)),
					resource.TestMatchResourceAttr("pulse_client.c", "user_key", regexpMust(`^U[A-Z0-9]{55}$`)),
					capture("pulse_client.c", "id", &id),
					capture("pulse_client.c", "creds", &creds1),
				),
			},
			// Description, tags and grants change in place; the credentials stay.
			{
				Config: clientConfig(name, "", `
  description = "Door sensor bot"
  tags        = ["home", "bot"]
  grants = [
    { action = "publish", subject = "evt.home.door.>" },
    { action = "subscribe", subject = "evt.home.>" },
    { action = "durable", subject = "evt.home.>", name = "doors" },
    { action = "blobs" },
  ]
`),
				ConfigPlanChecks: expectUpdate("pulse_client.c"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_client.c", "description", "Door sensor bot"),
					resource.TestCheckResourceAttr("pulse_client.c", "tags.#", "2"),
					resource.TestCheckResourceAttr("pulse_client.c", "grants.#", "4"),
					resource.TestCheckTypeSetElemNestedAttrs("pulse_client.c", "grants.*", map[string]string{"action": "durable", "name": "doors"}),
					resource.TestCheckResourceAttrWith("pulse_client.c", "creds", func(v string) error {
						if v != creds1 {
							return fmt.Errorf("credentials changed without rotation")
						}
						return nil
					}),
					func(s *terraform.State) error {
						c, err := adminClient(t).GetClient(context.Background(), id)
						if err != nil || len(c.Grants) != 4 || c.Description != "Door sensor bot" {
							return fmt.Errorf("server state %+v: %w", c, err)
						}
						return nil
					},
				),
			},
			// Rotation.
			{
				Config:           clientConfig(name, `creds_version = "2"`, ""),
				ConfigPlanChecks: expectUpdate("pulse_client.c"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_client.c", "grants.#", "0"),
					capture("pulse_client.c", "creds", &creds2),
					func(*terraform.State) error {
						if creds2 == creds1 || !strings.Contains(creds2, "NATS USER JWT") {
							return fmt.Errorf("credentials were not rotated")
						}
						return nil
					},
				),
			},
			// Unchanged configuration: no diff.
			{Config: clientConfig(name, `creds_version = "2"`, ""), ConfigPlanChecks: expectNoop("pulse_client.c")},
			{ResourceName: "pulse_client.c", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"creds", "creds_version"}},
			{ResourceName: "pulse_client.c", ImportState: true, ImportStateId: name, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"creds", "creds_version"}},
			// Revoked out of band → re-created under the same name (the revoked record is removed).
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.RevokeClient(ctx, id) }),
				Config:           clientConfig(name, `creds_version = "2"`, ""),
				ConfigPlanChecks: expectRecreate("pulse_client.c"),
			},
		},
	})
}

func clientConfig(name, top, extra string) string {
	return fmt.Sprintf(`
resource "pulse_client" "c" {
  name = %q
  %s
%s
}
`, name, top, extra)
}

func TestAccClientDataSource(t *testing.T) {
	testAccPreCheck(t)
	name := randName("cd")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: clientConfig(name, "", `  grants = [{ action = "publish", subject = "evt.x.>" }]`) + `
data "pulse_client" "by_name" {
  name = pulse_client.c.name
}
data "pulse_client" "by_id" {
  id = pulse_client.c.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.pulse_client.by_name", "id", "pulse_client.c", "id"),
				resource.TestCheckResourceAttrPair("data.pulse_client.by_id", "name", "pulse_client.c", "name"),
				resource.TestCheckResourceAttr("data.pulse_client.by_id", "grants.#", "1"),
				resource.TestCheckResourceAttr("data.pulse_client.by_id", "status", "active"),
			),
		}},
	})
}
