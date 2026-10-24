package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewAuthSettingsResource) }

const authSettingsID = "auth_settings"

var _ resource.ResourceWithImportState = (*authSettingsResource)(nil)

type authSettingsResource struct{ baseResource }

// NewAuthSettingsResource returns the pulse_auth_settings resource.
func NewAuthSettingsResource() resource.Resource { return &authSettingsResource{} }

type authSettingsModel struct {
	ID                 types.String `tfsdk:"id"`
	LocalLoginDisabled types.Bool   `tfsdk:"local_login_disabled"`
}

func (r *authSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_settings"
}

func (r *authSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The server's dashboard sign-in settings. There is one per server: declare it at most once.\n\n" +
			"Set `local_login_disabled = true` to make the dashboard SSO-only. Pulse accepts that only after an administrator " +
			"has completed a test login on an enabled `pulse_sso_provider`, and while at least one enabled administrator has a " +
			"linked identity on an enabled provider; otherwise the apply fails. While it is on, Pulse also refuses changes that " +
			"would leave no administrator able to sign in with SSO. Break-glass recovery on the server keeps working.\n\n" +
			"Destroying this resource turns password sign-in back on (`local_login_disabled = false`), the safe default.\n\n" +
			"Import with any ID, for example `terraform import pulse_auth_settings.this auth_settings`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Always `" + authSettingsID + "`."),
			"local_login_disabled": schema.BoolAttribute{
				MarkdownDescription: "Refuse email and password sign-in, so people must use SSO. Default `false`.",
				Optional:            true, Computed: true, Default: booldefault.StaticBool(false),
			},
		},
	}
}

func (r *authSettingsResource) apply(ctx context.Context, m *authSettingsModel) (*client.AuthSettings, error) {
	return r.client.PutAuthSettings(ctx, client.AuthSettings{LocalLoginDisabled: m.LocalLoginDisabled.ValueBool()})
}

func (r *authSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan authSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.apply(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot change sign-in settings", err)
		return
	}
	plan.ID, plan.LocalLoginDisabled = types.StringValue(authSettingsID), types.BoolValue(s.LocalLoginDisabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *authSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state authSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.GetAuthSettings(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read sign-in settings", err)
		return
	}
	state.ID, state.LocalLoginDisabled = types.StringValue(authSettingsID), types.BoolValue(s.LocalLoginDisabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *authSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan authSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.apply(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot change sign-in settings", err)
		return
	}
	plan.ID, plan.LocalLoginDisabled = types.StringValue(authSettingsID), types.BoolValue(s.LocalLoginDisabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete turns password sign-in back on.
func (r *authSettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if _, err := r.client.PutAuthSettings(ctx, client.AuthSettings{LocalLoginDisabled: false}); err != nil {
		apiError(&resp.Diagnostics, "Cannot turn password sign-in back on", err)
	}
}

func (r *authSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), authSettingsID)...)
}
