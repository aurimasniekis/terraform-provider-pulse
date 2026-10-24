package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccHTTPToken(t *testing.T) {
	testAccPreCheck(t)
	name := randName("tok")
	var id string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_http_token", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetHTTPToken(ctx, a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: httpTokenConfig(name, `{ action = "publish", subject = "evt.ci.>" }`, `["192.0.2.0/24", "2001:db8::/32"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_http_token.t", "status", "active"),
					resource.TestCheckResourceAttr("pulse_http_token.t", "cidrs.#", "2"),
					resource.TestMatchResourceAttr("pulse_http_token.t", "secret", regexpMust(`^pulse_`)),
					capture("pulse_http_token.t", "id", &id),
				),
			},
			{Config: httpTokenConfig(name, `{ action = "publish", subject = "evt.ci.>" }`, `["192.0.2.0/24", "2001:db8::/32"]`),
				ConfigPlanChecks: expectNoop("pulse_http_token.t")},
			{ResourceName: "pulse_http_token.t", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"secret"}},
			// Tokens cannot be edited: a grant change replaces the token (and frees the name for the new one).
			{
				Config:           httpTokenConfig(name, `{ action = "subscribe", subject = "evt.ci.>" }`, `[]`),
				ConfigPlanChecks: expectReplace("pulse_http_token.t"),
				Check:            resource.TestCheckResourceAttr("pulse_http_token.t", "cidrs.#", "0"),
			},
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					tok, err := c.ListHTTPTokens(ctx)
					if err != nil {
						return err
					}
					for _, x := range tok {
						if x.Name == name && x.Status == "active" {
							return c.RevokeHTTPToken(ctx, x.ID)
						}
					}
					return fmt.Errorf("token %s not found", name)
				}),
				Config:           httpTokenConfig(name, `{ action = "subscribe", subject = "evt.ci.>" }`, `[]`),
				ConfigPlanChecks: expectRecreate("pulse_http_token.t"),
			},
		},
	})
}

func TestAccHTTPTokenValidation(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: httpTokenConfig(randName("tok"), `{ action = "publish", subject = "evt.ci.>" }`, `["192.0.2.10/24"]`),
				ExpectError: regexpMust(`canonical form`)},
			{Config: httpTokenConfig(randName("tok"), `{ action = "durable", subject = "evt.ci.>", name = "x" }`, `[]`),
				ExpectError: regexpMust(`value must be one of`)},
		},
	})
}

func httpTokenConfig(name, grant, cidrs string) string {
	return fmt.Sprintf(`
resource "pulse_http_token" "t" {
  name   = %q
  grants = [%s]
  cidrs  = %s
}
`, name, grant, cidrs)
}
