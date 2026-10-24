package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewSSOMappingResource) }

var _ resource.ResourceWithImportState = (*ssoMappingResource)(nil)

type ssoMappingResource struct{ baseResource }

// NewSSOMappingResource returns the pulse_sso_mapping resource.
func NewSSOMappingResource() resource.Resource { return &ssoMappingResource{} }

type ssoMappingModel struct {
	ID         types.String `tfsdk:"id"`
	ProviderID types.String `tfsdk:"provider_id"`
	GroupValue types.String `tfsdk:"group_value"`
	Role       types.String `tfsdk:"role"`
}

func (r *ssoMappingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_mapping"
}

func (r *ssoMappingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants a dashboard role to everyone who signs in through an SSO provider while in a given group. " +
			"The provider's `groups_claim` names the ID token claim that lists the person's groups; at each SSO sign-in Pulse " +
			"replaces the user's SSO roles with the roles of every mapping that matches. These roles show up in " +
			"`pulse_user.sso_roles`, never in `pulse_user.roles`.\n\n" +
			"Removing a mapping takes the role away at the user's next SSO sign-in. Map several groups or roles with several " +
			"mappings; the same group and role can be mapped only once per provider.\n\n" +
			"Import with `<provider_id>/<mapping_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Mapping ID."),
			"provider_id": requiredImmutableString("ID of the SSO provider (`pulse_sso_provider.<name>.id`).",
				stringvalidator.LengthBetween(1, 64)),
			"group_value": requiredImmutableString("A value of the provider's groups claim, exactly as the provider sends it (a "+
				"group name or, for Microsoft Entra ID, a group object ID; at most 256 bytes).",
				stringvalidator.LengthBetween(1, 256), ssoTrimmedValidator{}),
			"role": requiredImmutableString("Role granted: `"+strings.Join(ssoDashboardRoles, "`, `")+"`.",
				stringvalidator.OneOf(ssoDashboardRoles...)),
		},
	}
}

func (m *ssoMappingModel) fill(v *client.SSOMapping) {
	m.ID = types.StringValue(v.ID)
	m.ProviderID = types.StringValue(v.ProviderID)
	m.GroupValue = types.StringValue(v.GroupValue)
	m.Role = types.StringValue(v.Role)
}

func (r *ssoMappingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoMappingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.CreateSSOMapping(ctx, plan.ProviderID.ValueString(), plan.GroupValue.ValueString(), plan.Role.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create SSO mapping", err)
		return
	}
	plan.fill(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ssoMappingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoMappingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.GetSSOMapping(ctx, state.ProviderID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read SSO mapping", err)
		return
	}
	state.fill(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every argument forces a new resource.
func (r *ssoMappingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *ssoMappingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoMappingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSSOMapping(ctx, state.ProviderID.ValueString(), state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete SSO mapping", err)
	}
}

func (r *ssoMappingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2, "<provider_id>/<mapping_id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("provider_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
