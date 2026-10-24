package provider

// Plan modifiers shared by the webhook, alert and security resources.

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// configurableSecretPlanModifier handles an Optional+Computed write-once secret: a configured value is planned as
// is; an unconfigured one keeps the value from state unless versionAttr changes (then it is unknown until apply).
type configurableSecretPlanModifier struct{ versionAttr string }

func (m configurableSecretPlanModifier) Description(context.Context) string {
	return "Uses the configured secret, or keeps the generated one from state unless " + m.versionAttr + " changes."
}

func (m configurableSecretPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m configurableSecretPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	secretPlanModifier{versionAttr: m.versionAttr}.PlanModifyString(ctx, req, resp)
}

// keepUnlessChanged plans a computed string from state while the named root attributes are unchanged (it is unknown
// otherwise, because the server derives it from them).
type keepUnlessChanged []string

func (m keepUnlessChanged) Description(context.Context) string {
	return "Keeps the value from state unless " + strings.Join(m, ", ") + " changes."
}

func (m keepUnlessChanged) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m keepUnlessChanged) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.StateValue.IsNull() {
		return
	}
	if rootAttrsEqual(req.Plan.Raw, req.State.Raw, m...) {
		resp.PlanValue = req.StateValue
	}
}

// PlanModifyInt64 does the same for numbers.
func (m keepUnlessChanged) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.StateValue.IsNull() {
		return
	}
	if rootAttrsEqual(req.Plan.Raw, req.State.Raw, m...) {
		resp.PlanValue = req.StateValue
	}
}

// rootAttrsEqual compares root attributes of two object values (plan and state).
func rootAttrsEqual(a, b tftypes.Value, attrs ...string) bool {
	var am, bm map[string]tftypes.Value
	if a.As(&am) != nil || b.As(&bm) != nil {
		return false
	}
	for _, k := range attrs {
		av, aok := am[k]
		bv, bok := bm[k]
		if aok != bok || !av.Equal(bv) {
			return false
		}
	}
	return true
}

// networkRequiresReplace forces a new resource when the network changes, but not when only its spelling does
// (e.g. 192.0.2.1 and 192.0.2.1/32): that is an in-place update that touches nothing in Pulse.
func networkRequiresReplace(key func(string) (string, error)) planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		p, err1 := key(req.PlanValue.ValueString())
		s, err2 := key(req.StateValue.ValueString())
		resp.RequiresReplace = err1 != nil || err2 != nil || p != s
	}, "Changing the network forces a new resource; rewriting the same network differently does not.",
		"Changing the network forces a new resource; rewriting the same network differently does not.")
}

// planNetworkID sets the planned id to the canonical network (for resources identified by a network).
func planNetworkID(ctx context.Context, plan *tfsdk.Plan, key func(string) (string, error)) {
	if plan.Raw.IsNull() {
		return
	}
	var network types.String
	if plan.GetAttribute(ctx, path.Root("network"), &network).HasError() || network.IsUnknown() || network.IsNull() {
		return
	}
	if k, err := key(network.ValueString()); err == nil {
		plan.SetAttribute(ctx, path.Root("id"), types.StringValue(k))
	}
}

// networkValidator rejects values that are not an IP address or CIDR.
type networkValidator struct{ key func(string) (string, error) }

func (v networkValidator) Description(context.Context) string {
	return "must be an IP address or CIDR"
}

func (v networkValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v networkValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := v.key(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid network", err.Error())
	}
}
