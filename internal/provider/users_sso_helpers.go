package provider

// Helpers shared by the dashboard user and single sign-on resources.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// ssoDashboardRoles are the roles a dashboard user can hold, most privileged first.
var ssoDashboardRoles = []string{"administrator", "operator", "auditor", "viewer"}

// ssoEmailRE is a loose email check; Pulse validates the address fully.
var ssoEmailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+$`)

// ssoGuardError adds an API error, explaining Pulse's account safety checks when it refused a change (412).
func ssoGuardError(diags *diag.Diagnostics, summary string, err error) {
	msg := err.Error()
	if client.HasStatus(err, http.StatusPreconditionFailed) {
		msg += "\n\nPulse always keeps at least one enabled administrator (and, while password sign-in is disabled, at least one " +
			"administrator who can sign in with SSO), and a token cannot disable or delete its own user. Give another user the " +
			"administrator role first, or re-enable password sign-in with pulse_auth_settings."
	}
	diags.AddError(summary, msg)
}

// ssoTrimmedValidator refuses values with leading or trailing whitespace (Pulse trims them, which would show as a
// difference after apply).
type ssoTrimmedValidator struct{}

func (ssoTrimmedValidator) Description(context.Context) string {
	return "must not start or end with whitespace"
}

func (v ssoTrimmedValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ssoTrimmedValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if s := req.ConfigValue.ValueString(); s != strings.TrimSpace(s) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", "The value "+v.Description(ctx)+".")
	}
}

// ssoLowercaseValidator refuses values that are not lower case (Pulse lower-cases emails and email domains).
type ssoLowercaseValidator struct{ what string }

func (v ssoLowercaseValidator) Description(context.Context) string {
	return "must be lower case without surrounding whitespace"
}

func (v ssoLowercaseValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ssoLowercaseValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if want := strings.ToLower(strings.TrimSpace(s)); s != want {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.what,
			fmt.Sprintf("Pulse stores %ss in lower case; write %q instead of %q.", v.what, want, s))
	}
}
