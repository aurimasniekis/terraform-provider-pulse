package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// ssoFakeIssuer runs a minimal OpenID provider over TLS (discovery and JWKS with a generated EC key) and returns its
// issuer URL and its certificate as PEM.
func ssoFakeIssuer(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	pub, err := key.PublicKey.Bytes() // uncompressed point: 0x04 || X || Y
	if err != nil {
		t.Fatal(err)
	}
	jwks := map[string]any{"keys": []map[string]any{{"kty": "EC", "crv": "P-256", "kid": "k1", "use": "sig", "alg": "ES256",
		"x": b64(pub[1:33]), "y": b64(pub[33:])}}}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
			"jwks_uri": srv.URL + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"ES256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

// checkSSOStartRedirects starts an SSO sign-in on the admin listener and expects Pulse to redirect to the fake
// issuer's authorization endpoint, which proves discovery worked with the CA bundle and private-network setting.
func checkSSOStartRedirects(name, issuer string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
			acc.AdminURL+"/auth/oidc/"+rs.Primary.Attributes["slug"]+"/start", nil)
		if err != nil {
			return err
		}
		hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, issuer+"/authorize?") {
			return fmt.Errorf("SSO start: status %d, Location %q (want a redirect to %s/authorize)", resp.StatusCode, loc, issuer)
		}
		return nil
	}
}

func TestAccSSOProvider(t *testing.T) {
	testAccPreCheck(t)
	issuer, caPEM := ssoFakeIssuer(t)
	slug := strings.ReplaceAll(randName("sso"), "-", "")
	slug2 := slug + "-b"
	var id string
	base := func(slug, extra string) string {
		return fmt.Sprintf(`
resource "pulse_sso_provider" "p" {
  slug                  = %q
  display_name          = "Test IdP"
  issuer                = %q
  client_id             = "pulse-test"
  allow_private_network = true
  ca_bundle_pem         = <<-EOT
%sEOT
%s
}
`, slug, issuer, caPEM, extra)
	}
	updated := `
  display_name                 = "Test IdP (updated)"
  client_secret                = "secret-two"
  scopes                       = ["openid", "email", "groups"]
  groups_claim                 = "groups"
  provisioning                 = "jit"
  jit = {
    email_domains = ["example.com"]
    groups        = ["pulse-users"]
  }
  stepup_fallback              = "local_totp"
  acr_values                   = ["mfa", "phr"]
  enabled                      = true
  end_session_enabled          = true
  allow_cross_origin_endpoints = true
`
	// display_name appears twice in the update; drop the base one.
	updatedConfig := func(slug string) string {
		return strings.Replace(base(slug, updated), "  display_name          = \"Test IdP\"\n", "", 1)
	}
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_sso_provider", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetSSOProvider(ctx, a["id"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: base(slug, `  client_secret = "secret-one"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					capture("pulse_sso_provider.p", "id", &id),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "slug", slug),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "issuer", issuer),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "has_client_secret", "true"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.#", "3"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.0", "openid"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "provisioning", "preprovisioned"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "stepup_fallback", "none"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "groups_claim", ""),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "jit.email_domains.#", "0"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "acr_values.#", "0"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "enabled", "false"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "allow_private_network", "true"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "ca_bundle_pem", caPEM),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "redirect_uri", acc.AdminURL+"/auth/oidc/callback"),
					resource.TestCheckNoResourceAttr("pulse_sso_provider.p", "test_login_ok_at"),
				),
			},
			// Everything changes in place, including the slug.
			{
				Config:           updatedConfig(slug2),
				ConfigPlanChecks: expectUpdate("pulse_sso_provider.p"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr("pulse_sso_provider.p", "id", &id),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "slug", slug2),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "display_name", "Test IdP (updated)"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "has_client_secret", "true"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.#", "3"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.2", "groups"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "groups_claim", "groups"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "provisioning", "jit"),
					resource.TestCheckTypeSetElemAttr("pulse_sso_provider.p", "jit.email_domains.*", "example.com"),
					resource.TestCheckTypeSetElemAttr("pulse_sso_provider.p", "jit.groups.*", "pulse-users"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "jit.tenants.#", "0"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "stepup_fallback", "local_totp"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "acr_values.1", "phr"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "enabled", "true"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "end_session_enabled", "true"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "allow_cross_origin_endpoints", "true"),
					checkSSOStartRedirects("pulse_sso_provider.p", issuer),
				),
			},
			// The secret is write-only: import cannot read it back.
			{ResourceName: "pulse_sso_provider.p", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"client_secret"}},
			{ResourceName: "pulse_sso_provider.p", ImportState: true, ImportStateVerify: true, ImportStateId: slug2,
				ImportStateVerifyIgnore: []string{"client_secret"}},
			// A secret removed outside Terraform is set again.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					p, err := c.GetSSOProvider(ctx, id)
					if err != nil {
						return err
					}
					_, err = c.UpdateSSOProvider(ctx, id, client.SSOProviderInput{Version: p.Version, ClientSecret: ptr("")})
					return err
				}),
				Config:           updatedConfig(slug2),
				ConfigPlanChecks: expectUpdate("pulse_sso_provider.p"),
				Check:            resource.TestCheckResourceAttr("pulse_sso_provider.p", "has_client_secret", "true"),
			},
			// Removing the secret from the configuration removes it from Pulse; defaults come back.
			{
				Config:           base(slug2, ""),
				ConfigPlanChecks: expectUpdate("pulse_sso_provider.p"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "has_client_secret", "false"),
					resource.TestCheckNoResourceAttr("pulse_sso_provider.p", "client_secret"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.#", "3"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "scopes.1", "profile"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "provisioning", "preprovisioned"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "jit.groups.#", "0"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "acr_values.#", "0"),
					resource.TestCheckResourceAttr("pulse_sso_provider.p", "enabled", "false"),
				),
			},
			{Config: base(slug2, ""), ConfigPlanChecks: expectNoop("pulse_sso_provider.p")},
			// Drift: deleted out of band → recreated.
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteSSOProvider(ctx, id) }),
				Config:           base(slug2, ""),
				ConfigPlanChecks: expectRecreate("pulse_sso_provider.p"),
			},
		},
	})
}

func TestAccSSOProviderInvalid(t *testing.T) {
	testAccPreCheck(t)
	cfg := func(extra string) string {
		return fmt.Sprintf(`
resource "pulse_sso_provider" "p" {
  slug         = %q
  display_name = "Bad"
  issuer       = "https://idp.example.com"
  client_id    = "pulse"
%s
}
`, strings.ReplaceAll(randName("bad"), "-", ""), extra)
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg(`  provisioning = "jit"`), ExpectError: ssoWrappedRE("JIT provisioning needs at least one restriction")},
			{Config: cfg(`  scopes = ["profile"]`), ExpectError: ssoWrappedRE("scopes must include openid")},
			{Config: cfg(`  jit = { email_domains = ["Example.com"] }`), ExpectError: regexpMust(`lower case`)},
			{Config: strings.Replace(cfg(""), "https://idp.example.com", "http://idp.example.com", 1),
				ExpectError: ssoWrappedRE("issuer must use https")},
		},
	})
}
