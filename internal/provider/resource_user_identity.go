package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewUserIdentityResource) }

var _ resource.ResourceWithImportState = (*userIdentityResource)(nil)

type userIdentityResource struct{ baseResource }

// NewUserIdentityResource returns the pulse_user_identity resource.
func NewUserIdentityResource() resource.Resource { return &userIdentityResource{} }

type userIdentityModel struct {
	ID         types.String `tfsdk:"id"`
	UserID     types.String `tfsdk:"user_id"`
	ProviderID types.String `tfsdk:"provider_id"`
	Subject    types.String `tfsdk:"subject"`
	Issuer     types.String `tfsdk:"issuer"`
	LinkedVia  types.String `tfsdk:"linked_via"`
}

func (r *userIdentityResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_identity"
}

func (r *userIdentityResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Links a single sign-on identity to a dashboard user, so that the person can sign in with that " +
			"provider: their next SSO sign-in whose ID token carries this subject (`sub` claim) signs in as this user. Use it " +
			"for providers with `provisioning = \"preprovisioned\"`, where Pulse does not create or match users on its own.\n\n" +
			"An identity can be linked to one user only. Removing the link drops the roles the user got from SSO group " +
			"mappings and ends their SSO sessions. Pulse refuses to unlink the last administrator who can sign in with SSO " +
			"while password sign-in is disabled.\n\n" +
			"Import with `<user_id>/<provider_id>/<subject>`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("`<user_id>/<provider_id>/<subject>`."),
			"user_id": requiredImmutableString("ID of the user (`pulse_user.<name>.id`).",
				stringvalidator.LengthBetween(1, 64)),
			"provider_id": requiredImmutableString("ID of the SSO provider (`pulse_sso_provider.<name>.id`).",
				stringvalidator.LengthBetween(1, 64)),
			"subject": requiredImmutableString("The person's subject identifier at the provider: the `sub` claim of their ID "+
				"token (at most 255 bytes). Many providers show it as the user's object or account ID.",
				stringvalidator.LengthBetween(1, 255)),
			"issuer":     computedString("Issuer URL of the provider at the time the identity was linked."),
			"linked_via": computedString("How the identity was linked: `admin` (by an administrator, including this provider, or through an administrator's link invitation), `self_link` (the user linked it from their account page) or `jit` (created at the first SSO sign-in)."),
		},
	}
}

func (m *userIdentityModel) fill(i *client.Identity) {
	m.ID = types.StringValue(i.UserID + "/" + i.ProviderID + "/" + i.Subject)
	m.UserID = types.StringValue(i.UserID)
	m.ProviderID = types.StringValue(i.ProviderID)
	m.Subject = types.StringValue(i.Subject)
	m.Issuer = types.StringValue(i.Issuer)
	m.LinkedVia = types.StringValue(i.LinkedVia)
}

func (r *userIdentityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userIdentityModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	uid, pid, sub := plan.UserID.ValueString(), plan.ProviderID.ValueString(), plan.Subject.ValueString()
	if err := r.client.LinkUserIdentity(ctx, uid, pid, sub); err != nil {
		apiError(&resp.Diagnostics, "Cannot link identity", err)
		return
	}
	i, err := r.client.GetUserIdentity(ctx, uid, pid, sub)
	if err != nil {
		apiError(&resp.Diagnostics, "Identity linked but could not be read back", err)
		return
	}
	plan.fill(i)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userIdentityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userIdentityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	i, err := r.client.GetUserIdentity(ctx, state.UserID.ValueString(), state.ProviderID.ValueString(), state.Subject.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read identity", err)
		return
	}
	state.fill(i)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every argument forces a new resource.
func (r *userIdentityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *userIdentityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userIdentityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UnlinkUserIdentity(ctx, state.UserID.ValueString(), state.Issuer.ValueString(), state.Subject.ValueString())
	if err != nil && !client.IsNotFound(err) {
		ssoGuardError(&resp.Diagnostics, "Cannot unlink identity", err)
	}
}

func (r *userIdentityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The subject may itself contain "/", so only the first two separators split.
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("expected an import ID of the form %q, got %q", "<user_id>/<provider_id>/<subject>", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("provider_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("subject"), parts[2])...)
}
