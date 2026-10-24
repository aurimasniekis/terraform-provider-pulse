package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func checkUsersGone(t *testing.T) resource.TestCheckFunc {
	return checkGone(t, "pulse_user", func(ctx context.Context, c *client.Client, a map[string]string) error {
		_, err := c.GetUser(ctx, a["id"])
		return err
	})
}

func userConfig(email, extra string) string { return userConfigNamed(email, "Test User", extra) }

func userConfigNamed(email, name, extra string) string {
	emailLine := ""
	if email != "" {
		emailLine = fmt.Sprintf("  email        = %q\n", email)
	}
	return fmt.Sprintf(`
resource "pulse_user" "u" {
%s  display_name = %q
%s
}
`, emailLine, name, extra)
}

func TestAccUser(t *testing.T) {
	testAccPreCheck(t)
	email := randName("user") + "@example.com"
	email2 := randName("user") + "@example.com"
	var id string
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkUsersGone(t),
		Steps: []resource.TestStep{
			{
				Config: userConfig(email, `  roles = ["viewer"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					capture("pulse_user.u", "id", &id),
					resource.TestCheckResourceAttr("pulse_user.u", "email", email),
					resource.TestCheckResourceAttr("pulse_user.u", "display_name", "Test User"),
					resource.TestCheckResourceAttr("pulse_user.u", "roles.#", "1"),
					resource.TestCheckTypeSetElemAttr("pulse_user.u", "roles.*", "viewer"),
					resource.TestCheckResourceAttr("pulse_user.u", "all_roles.#", "1"),
					resource.TestCheckResourceAttr("pulse_user.u", "sso_roles.#", "0"),
					resource.TestCheckResourceAttr("pulse_user.u", "enabled", "true"),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "false"),
					resource.TestCheckResourceAttr("pulse_user.u", "has_password", "false"),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_enrolled", "false"),
					resource.TestCheckResourceAttr("pulse_user.u", "provisioned_via", "admin"),
				),
			},
			{
				Config: userConfigNamed(email, "Renamed", `
  roles        = ["operator", "auditor"]
  mfa_required = true
  enabled      = false
`),
				ConfigPlanChecks: expectUpdate("pulse_user.u"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr("pulse_user.u", "id", &id),
					resource.TestCheckResourceAttr("pulse_user.u", "display_name", "Renamed"),
					resource.TestCheckResourceAttr("pulse_user.u", "roles.#", "2"),
					resource.TestCheckTypeSetElemAttr("pulse_user.u", "roles.*", "operator"),
					resource.TestCheckTypeSetElemAttr("pulse_user.u", "all_roles.*", "auditor"),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "true"),
					resource.TestCheckResourceAttr("pulse_user.u", "enabled", "false"),
				),
			},
			{ResourceName: "pulse_user.u", ImportState: true, ImportStateVerify: true},
			{ResourceName: "pulse_user.u", ImportState: true, ImportStateVerify: true, ImportStateId: strings.ToUpper(email)},
			// Back to no roles and enabled.
			{
				Config:           userConfigNamed(email, "Renamed", ""),
				ConfigPlanChecks: expectUpdate("pulse_user.u"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_user.u", "roles.#", "0"),
					resource.TestCheckResourceAttr("pulse_user.u", "all_roles.#", "0"),
					resource.TestCheckResourceAttr("pulse_user.u", "enabled", "true"),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "false"),
				),
			},
			// Drift: deleted out of band → recreated.
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteUser(ctx, id) }),
				Config:           userConfig(email, ""),
				ConfigPlanChecks: expectRecreate("pulse_user.u"),
			},
			// Pulse cannot change an email address.
			{
				Config:           userConfig(email2, ""),
				ConfigPlanChecks: expectReplace("pulse_user.u"),
				Check:            resource.TestCheckResourceAttr("pulse_user.u", "email", email2),
			},
			// Removing the email also replaces the user.
			{
				Config:           userConfig("", ""),
				ConfigPlanChecks: expectReplace("pulse_user.u"),
				Check:            resource.TestCheckNoResourceAttr("pulse_user.u", "email"),
			},
		},
	})
}

// A user created disabled is disabled right away.
func TestAccUserCreateDisabled(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkUsersGone(t),
		Steps: []resource.TestStep{{
			Config: userConfig(randName("off")+"@example.com", "  enabled = false\n  roles = [\"administrator\"]"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("pulse_user.u", "enabled", "false"),
				resource.TestCheckTypeSetElemAttr("pulse_user.u", "roles.*", "administrator"),
			),
		}},
	})
}

func TestAccUserInvalid(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: userConfig("Alice@Example.com", ""), ExpectError: regexpMust(`lower case`)},
			{Config: userConfig("", `  roles = ["root"]`), ExpectError: regexpMust(`value must be one of`)},
			{Config: userConfig("not-an-email", ""), ExpectError: regexpMust(`must be an email address`)},
		},
	})
}

// A user with the same email cannot be created twice.
func TestAccUserDuplicateEmail(t *testing.T) {
	testAccPreCheck(t)
	email := randName("dup") + "@example.com"
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkUsersGone(t),
		Steps: []resource.TestStep{{
			Config:      userConfig(email, "") + strings.Replace(userConfig(email, ""), `"u"`, `"v"`, 1),
			ExpectError: ssoWrappedRE("a user with this email already exists"),
		}},
	})
}

// A viewer token can read but not manage users; the error says why.
func TestAccUserViewerForbidden(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf("provider \"pulse\" {\n  token = %q\n}\n", acc.ViewerToken) +
				userConfig(randName("viewer")+"@example.com", ""),
			ExpectError: ssoWrappedRE("POST /users: 403 forbidden", "The token's role does not allow this"),
		}},
	})
}

func TestAccUserDataSource(t *testing.T) {
	testAccPreCheck(t)
	email := randName("ds") + "@example.com"
	cfg := userConfig(email, `  roles = ["auditor"]`) + fmt.Sprintf(`
data "pulse_user" "by_email" {
  email = %q
  depends_on = [pulse_user.u]
}

data "pulse_user" "by_id" {
  id = pulse_user.u.id
}
`, strings.ToUpper(email))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkUsersGone(t),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.pulse_user.by_email", "id", "pulse_user.u", "id"),
					resource.TestCheckResourceAttr("data.pulse_user.by_email", "display_name", "Test User"),
					resource.TestCheckTypeSetElemAttr("data.pulse_user.by_email", "roles.*", "auditor"),
					resource.TestCheckResourceAttr("data.pulse_user.by_id", "email", email),
					resource.TestCheckTypeSetElemAttr("data.pulse_user.by_id", "all_roles.*", "auditor"),
					resource.TestCheckResourceAttr("data.pulse_user.by_id", "enabled", "true"),
				),
			},
		},
	})
}

func TestAccUserDataSourceInvalid(t *testing.T) {
	testAccPreCheck(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      "data \"pulse_user\" \"none\" {\n  email = \"" + randName("nobody") + "@example.com\"\n}\n",
				ExpectError: ssoWrappedRE("not found"),
			},
			{
				Config:      "data \"pulse_user\" \"both\" {\n  id    = \"x\"\n  email = \"x@example.com\"\n}\n",
				ExpectError: ssoWrappedRE("Exactly one of these attributes must be configured"),
			},
			{Config: "data \"pulse_user\" \"neither\" {}\n", ExpectError: ssoWrappedRE("Exactly one of these attributes must be configured")},
		},
	})
}

// ssoWrappedRE matches phrases, in order, in Terraform's output, which wraps long lines.
func ssoWrappedRE(phrases ...string) *regexp.Regexp {
	res := make([]string, 0, len(phrases))
	for _, p := range phrases {
		words := strings.Fields(p)
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		res = append(res, strings.Join(words, `\s+`))
	}
	return regexp.MustCompile(`(?s)` + strings.Join(res, ".*"))
}

// ssoSetPassword gives a user a password through a reset link, as the person would on the dashboard.
func ssoSetPassword(ctx context.Context, userID string) error {
	do := func(path, token string, body any) (*http.Response, error) {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, acc.AdminURL+"/api/admin/v1"+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("Origin", acc.AdminURL) // unauthenticated browser requests must be same-origin
		}
		return http.DefaultClient.Do(req)
	}
	resp, err := do("/users/"+userID+"/reset-link", acc.AdminToken, map[string]any{})
	if err != nil {
		return err
	}
	var link struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&link)
	_ = resp.Body.Close()
	if err != nil {
		return fmt.Errorf("reset link: %w", err)
	}
	if resp.StatusCode != http.StatusOK || link.Token == "" {
		return fmt.Errorf("reset link: status %d", resp.StatusCode)
	}
	resp, err = do("/auth/reset/"+link.Token, "", map[string]string{"password": "correct horse battery staple " + randName("pw")})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("reset password: status %d", resp.StatusCode)
	}
	return nil
}

// Pulse requires MFA for password accounts with the administrator role whatever mfa_required says; that must not
// show as a difference.
func TestAccUserMFARequiredByRole(t *testing.T) {
	testAccPreCheck(t)
	email := randName("mfa") + "@example.com"
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkUsersGone(t),
		Steps: []resource.TestStep{
			{
				Config: userConfig(email, `  roles = ["administrator"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					capture("pulse_user.u", "id", &id),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "false"),
				),
			},
			{
				PreConfig:        func() { ssoMust(t, ssoSetPassword(context.Background(), id)) },
				Config:           userConfig(email, `  roles = ["administrator"]`),
				ConfigPlanChecks: expectNoop("pulse_user.u"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_user.u", "has_password", "true"),
					resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "false"),
					func(*terraform.State) error {
						u, err := adminClient(t).GetUser(context.Background(), id)
						if err != nil {
							return err
						}
						if !u.MFARequired {
							return errors.New("expected Pulse to report mfa_required for a password administrator")
						}
						return nil
					},
				),
			},
			{
				Config:           userConfigNamed(email, "Renamed admin", `  roles = ["administrator", "viewer"]`),
				ConfigPlanChecks: expectUpdate("pulse_user.u"),
				Check:            resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "false"),
			},
			{
				Config:           userConfigNamed(email, "Renamed admin", "  roles = [\"administrator\", \"viewer\"]\n  mfa_required = true"),
				ConfigPlanChecks: expectUpdate("pulse_user.u"),
				Check:            resource.TestCheckResourceAttr("pulse_user.u", "mfa_required", "true"),
			},
		},
	})
}

func ssoMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
