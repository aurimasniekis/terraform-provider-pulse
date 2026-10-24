package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

const testSigningSecret = "my-signing-secret-0123456789" // gitleaks:allow (test literal)

func TestAccOutboundWebhook(t *testing.T) {
	testAccPreCheck(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(srv.Close)
	name := randName("oh")
	const res = "pulse_outbound_webhook.o"
	var id, secret1 string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_outbound_webhook", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetOutboundHook(ctx, a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: outboundConfig(name, srv.URL+"/a", "evt.tfacc.>", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "name", name),
					resource.TestCheckResourceAttr(res, "url", srv.URL+"/a"),
					resource.TestCheckResourceAttr(res, "filter", "evt.tfacc.>"),
					resource.TestCheckResourceAttr(res, "headers.%", "0"),
					resource.TestCheckResourceAttr(res, "paused", "false"),
					resource.TestMatchResourceAttr(res, "secret", regexpMust(`^whsec_[0-9a-f]{64}$`)),
					resource.TestMatchResourceAttr(res, "consumer", regexpMust(`^pulse_out_[a-z0-9]+$`)),
					capture(res, "id", &id),
					capture(res, "secret", &secret1),
				),
			},
			{
				Config: outboundConfig(name+"-b", srv.URL+"/b", "evt.tfacc.*.created", `
  paused  = true
  headers = {
    Authorization = "Bearer tfacc"
    X-Env         = "test"
  }
`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "name", name+"-b"),
					resource.TestCheckResourceAttr(res, "url", srv.URL+"/b"),
					resource.TestCheckResourceAttr(res, "filter", "evt.tfacc.*.created"),
					resource.TestCheckResourceAttr(res, "paused", "true"),
					resource.TestCheckResourceAttr(res, "headers.%", "2"),
					resource.TestCheckResourceAttr(res, "headers.Authorization", "Bearer tfacc"),
					attrIs(res, "id", &id),
					attrIs(res, "secret", &secret1),
				),
			},
			{ResourceName: res, ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"secret", "secret_version"}},
			{
				// Rotation and resume.
				Config:           outboundConfig(name+"-b", srv.URL+"/b", "evt.tfacc.*.created", `  secret_version = "2"`),
				ConfigPlanChecks: expectUpdate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "paused", "false"),
					resource.TestCheckResourceAttr(res, "headers.%", "0"),
					resource.TestMatchResourceAttr(res, "secret", regexpMust(`^whsec_`)),
					attrDiffers(res, "secret", &secret1),
				),
			},
			{
				Config:           outboundConfig(name+"-b", srv.URL+"/b", "evt.tfacc.*.created", "  secret = \""+testSigningSecret+"\""),
				ConfigPlanChecks: expectUpdate(res),
				Check:            resource.TestCheckResourceAttr(res, "secret", testSigningSecret),
			},
			// Drift: deleted out of band → recreated (paused on creation).
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteOutboundHook(ctx, id) }),
				Config:           outboundConfig(name+"-b", srv.URL+"/b", "evt.tfacc.*.created", "  secret = \""+testSigningSecret+"\"\n  paused = true"),
				ConfigPlanChecks: expectRecreate(res),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "paused", "true"),
					resource.TestCheckResourceAttr(res, "secret", testSigningSecret),
				),
			},
			// Out-of-band resume is corrected.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					hs, err := c.ListOutboundHooks(ctx)
					if err != nil {
						return err
					}
					for _, h := range hs {
						if h.Name == name+"-b" {
							_, err := c.SetOutboundHookPaused(ctx, h.ID, false)
							return err
						}
					}
					return fmt.Errorf("hook %s not found", name+"-b")
				}),
				Config:           outboundConfig(name+"-b", srv.URL+"/b", "evt.tfacc.*.created", "  secret = \""+testSigningSecret+"\"\n  paused = true"),
				ConfigPlanChecks: expectUpdate(res),
				Check:            resource.TestCheckResourceAttr(res, "paused", "true"),
			},
		},
	})
}

func TestAccOutboundWebhookInvalid(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// The SSRF guard refuses private addresses that are not explicitly allowed.
			{Config: outboundConfig(randName("oh"), "http://10.1.2.3/hook", "evt.>", ""), ExpectError: regexpMust(`(?i)url`)},
			{Config: outboundConfig(randName("oh"), "https://example.com/hook", "app.>", ""), ExpectError: regexpMust(`subject filter`)},
			{Config: outboundConfig(randName("oh"), "https://example.com/hook", "evt.>", `  headers = { Host = "x" }`),
				ExpectError: regexpMust(`cannot be set`)},
		},
	})
}

func outboundConfig(name, url, filter, extra string) string {
	return fmt.Sprintf(`
resource "pulse_outbound_webhook" "o" {
  name   = %q
  url    = %q
  filter = %q
%s
}
`, name, url, filter, extra)
}
