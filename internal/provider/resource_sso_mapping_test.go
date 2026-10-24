package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccSSOMapping(t *testing.T) {
	testAccPreCheck(t)
	slug := ssoTestSlug("map")
	cfg := func(group, role string) string {
		return ssoProviderConfig(slug) + fmt.Sprintf(`
resource "pulse_sso_mapping" "m" {
  provider_id = pulse_sso_provider.p.id
  group_value = %q
  role        = %q
}
`, group, role)
	}
	var providerID, mappingID string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_sso_mapping", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetSSOMapping(ctx, a["provider_id"], a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: cfg("pulse-admins", "administrator"),
				Check: resource.ComposeAggregateTestCheckFunc(
					capture("pulse_sso_mapping.m", "provider_id", &providerID),
					capture("pulse_sso_mapping.m", "id", &mappingID),
					resource.TestCheckResourceAttrPair("pulse_sso_mapping.m", "provider_id", "pulse_sso_provider.p", "id"),
					resource.TestCheckResourceAttr("pulse_sso_mapping.m", "group_value", "pulse-admins"),
					resource.TestCheckResourceAttr("pulse_sso_mapping.m", "role", "administrator"),
				),
			},
			{ResourceName: "pulse_sso_mapping.m", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("pulse_sso_mapping.m", "provider_id", "id")},
			// Drift: deleted out of band → recreated.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					return c.DeleteSSOMapping(ctx, providerID, mappingID)
				}),
				Config:           cfg("pulse-admins", "administrator"),
				ConfigPlanChecks: expectRecreate("pulse_sso_mapping.m"),
			},
			{
				Config:           cfg("pulse-admins", "viewer"),
				ConfigPlanChecks: expectReplace("pulse_sso_mapping.m"),
				Check:            resource.TestCheckResourceAttr("pulse_sso_mapping.m", "role", "viewer"),
			},
			{
				Config:           cfg("pulse-viewers", "viewer"),
				ConfigPlanChecks: expectReplace("pulse_sso_mapping.m"),
				Check:            resource.TestCheckResourceAttr("pulse_sso_mapping.m", "group_value", "pulse-viewers"),
			},
		},
	})
}

func TestAccSSOMappingDuplicate(t *testing.T) {
	testAccPreCheck(t)
	cfg := ssoProviderConfig(ssoTestSlug("mdup")) + `
resource "pulse_sso_mapping" "a" {
  provider_id = pulse_sso_provider.p.id
  group_value = "ops"
  role        = "operator"
}

resource "pulse_sso_mapping" "b" {
  provider_id = pulse_sso_provider.p.id
  group_value = "ops"
  role        = "operator"
  depends_on  = [pulse_sso_mapping.a]
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    []resource.TestStep{{Config: cfg, ExpectError: ssoWrappedRE("this mapping already exists")}},
	})
}
