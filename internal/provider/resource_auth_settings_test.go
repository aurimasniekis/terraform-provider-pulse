package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccAuthSettings(t *testing.T) {
	testAccPreCheck(t)
	cfg := func(disabled string) string {
		return "resource \"pulse_auth_settings\" \"s\" {\n  local_login_disabled = " + disabled + "\n}\n"
	}
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Destroy turns password sign-in back on.
		CheckDestroy: func(*terraform.State) error {
			s, err := adminClient(t).GetAuthSettings(context.Background())
			if err != nil {
				return err
			}
			if s.LocalLoginDisabled {
				return errors.New("password sign-in is still disabled after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_auth_settings.s", "id", "auth_settings"),
					resource.TestCheckResourceAttr("pulse_auth_settings.s", "local_login_disabled", "false"),
				),
			},
			{Config: "resource \"pulse_auth_settings\" \"s\" {}\n", ConfigPlanChecks: expectNoop("pulse_auth_settings.s")},
			{ResourceName: "pulse_auth_settings.s", ImportState: true, ImportStateVerify: true, ImportStateId: "anything"},
			// SSO-only needs a tested provider and an administrator who can sign in with SSO; this server has neither.
			{
				Config:      cfg("true"),
				ExpectError: ssoWrappedRE("412 precondition: password login can be disabled only after an administrator completed an SSO test login"),
			},
			{Config: cfg("false"), ConfigPlanChecks: expectNoop("pulse_auth_settings.s")},
		},
	})
}
