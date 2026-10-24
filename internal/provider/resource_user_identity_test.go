package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// ssoProviderConfig is a provider that is never contacted (Pulse discovers the issuer only at sign-in).
func ssoProviderConfig(slug string) string {
	return fmt.Sprintf(`
resource "pulse_sso_provider" "p" {
  slug         = %q
  display_name = "Test IdP"
  issuer       = "https://%s.idp.example.com"
  client_id    = "pulse"
}
`, slug, slug)
}

func ssoTestSlug(prefix string) string { return strings.ReplaceAll(randName(prefix), "-", "") }

func TestAccUserIdentity(t *testing.T) {
	testAccPreCheck(t)
	slug := ssoTestSlug("idn")
	email := randName("idn") + "@example.com"
	cfg := func(subject string) string {
		return ssoProviderConfig(slug) + userConfig(email, `  roles = ["viewer"]`) + fmt.Sprintf(`
resource "pulse_user_identity" "i" {
  user_id     = pulse_user.u.id
  provider_id = pulse_sso_provider.p.id
  subject     = %q
}
`, subject)
	}
	var userID, issuer string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_user_identity", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetUserIdentity(ctx, a["user_id"], a["provider_id"], a["subject"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: cfg("subject/with/slashes"),
				Check: resource.ComposeAggregateTestCheckFunc(
					capture("pulse_user.u", "id", &userID),
					capture("pulse_user_identity.i", "issuer", &issuer),
					resource.TestCheckResourceAttrPair("pulse_user_identity.i", "user_id", "pulse_user.u", "id"),
					resource.TestCheckResourceAttrPair("pulse_user_identity.i", "provider_id", "pulse_sso_provider.p", "id"),
					resource.TestCheckResourceAttr("pulse_user_identity.i", "subject", "subject/with/slashes"),
					resource.TestCheckResourceAttr("pulse_user_identity.i", "issuer", "https://"+slug+".idp.example.com"),
					resource.TestCheckResourceAttr("pulse_user_identity.i", "linked_via", "admin"),
				),
			},
			{ResourceName: "pulse_user_identity.i", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("pulse_user_identity.i", "user_id", "provider_id", "subject")},
			// Drift: unlinked out of band → linked again.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					return c.UnlinkUserIdentity(ctx, userID, issuer, "subject/with/slashes")
				}),
				Config:           cfg("subject/with/slashes"),
				ConfigPlanChecks: expectRecreate("pulse_user_identity.i"),
			},
			// The subject cannot change in place.
			{
				Config:           cfg("other-subject"),
				ConfigPlanChecks: expectReplace("pulse_user_identity.i"),
				Check:            resource.TestCheckResourceAttr("pulse_user_identity.i", "subject", "other-subject"),
			},
		},
	})
}

// An identity can belong to one user only.
func TestAccUserIdentityConflict(t *testing.T) {
	testAccPreCheck(t)
	slug := ssoTestSlug("idc")
	cfg := ssoProviderConfig(slug) + userConfig(randName("idc")+"@example.com", "") +
		strings.Replace(userConfig(randName("idc")+"@example.com", ""), `"u"`, `"v"`, 1) + `
resource "pulse_user_identity" "a" {
  user_id     = pulse_user.u.id
  provider_id = pulse_sso_provider.p.id
  subject     = "same"
}

resource "pulse_user_identity" "b" {
  user_id     = pulse_user.v.id
  provider_id = pulse_sso_provider.p.id
  subject     = "same"
  depends_on  = [pulse_user_identity.a]
}
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      cfg,
			ExpectError: ssoWrappedRE("already linked to another user"),
		}},
	})
}
