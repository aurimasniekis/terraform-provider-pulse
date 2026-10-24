package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewUserResource) }

var _ resource.ResourceWithImportState = (*userResource)(nil)

type userResource struct{ baseResource }

// NewUserResource returns the pulse_user resource.
func NewUserResource() resource.Resource { return &userResource{} }

type userModel struct {
	ID             types.String `tfsdk:"id"`
	Email          types.String `tfsdk:"email"`
	DisplayName    types.String `tfsdk:"display_name"`
	Roles          types.Set    `tfsdk:"roles"`
	MFARequired    types.Bool   `tfsdk:"mfa_required"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	HasPassword    types.Bool   `tfsdk:"has_password"`
	MFAEnrolled    types.Bool   `tfsdk:"mfa_enrolled"`
	ProvisionedVia types.String `tfsdk:"provisioned_via"`
	SSORoles       types.Set    `tfsdk:"sso_roles"`
	AllRoles       types.Set    `tfsdk:"all_roles"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	roleList := "`" + strings.Join(ssoDashboardRoles, "`, `") + "`"
	resp.Schema = schema.Schema{
		MarkdownDescription: "A dashboard user: a person who signs in to the Pulse dashboard.\n\n" +
			"A user created here has no password. They sign in with single sign-on (link their identity with " +
			"`pulse_user_identity`, or let a `pulse_sso_provider` with just-in-time provisioning match them by email), or an " +
			"administrator issues them a one-time password link from the dashboard's Users page.\n\n" +
			"`roles` holds only the roles assigned here; roles a user gets from SSO group mappings (`pulse_sso_mapping`) are " +
			"reported in `sso_roles` and are never removed by this resource. Changing a user's roles or disabling them signs them " +
			"out everywhere.\n\n" +
			"Pulse refuses to remove the administrator role from, disable or delete the last enabled administrator, and a token " +
			"cannot disable or delete its own user.\n\n" +
			"Import with the user ID or the email address.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("User ID."),
			"email": schema.StringAttribute{
				MarkdownDescription: "Email address, in lower case (Pulse stores emails in lower case). Optional for users who " +
					"sign in only with SSO. Pulse cannot change a user's email, so changing it forces a new resource.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.LengthBetween(3, 254), ssoLowercaseValidator{what: "email"},
					stringvalidator.RegexMatches(ssoEmailRE, "must be an email address")},
			},
			"display_name": schema.StringAttribute{MarkdownDescription: "Name shown in the dashboard (at most 128 bytes).",
				Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 128), ssoTrimmedValidator{}}},
			"roles": schema.SetAttribute{
				MarkdownDescription: "Roles assigned to the user directly: " + roleList + ". Defaults to none (for example for a " +
					"user who gets every role from SSO group mappings). Roles from SSO mappings are not listed here; see `sso_roles`.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: setdefault.StaticValue(setOf(nil)),
				Validators: []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(ssoDashboardRoles...))},
			},
			"mfa_required": schema.BoolAttribute{
				MarkdownDescription: "Require the user to enroll an authenticator app (TOTP) for password sign-in even when their " +
					"roles do not demand it. Default `false`. Pulse also requires it for password accounts holding roles listed in " +
					"its `PULSE_MFA_REQUIRED_ROLES` setting (`administrator` and `operator` by default); that requirement is not " +
					"shown as a difference here.",
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
			},
			"enabled": schema.BoolAttribute{MarkdownDescription: "Whether the user can sign in. Default `true`. Disabling a user " +
				"signs them out everywhere.", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"has_password": schema.BoolAttribute{MarkdownDescription: "Whether the user has set a password.", Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
			"mfa_enrolled": schema.BoolAttribute{MarkdownDescription: "Whether the user has enrolled an authenticator app.",
				Computed: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
			"provisioned_via": computedString("How the user was created: `admin` (by an administrator, including this " +
				"provider), `jit` (just-in-time on their first SSO sign-in), or another way such as an invitation."),
			"sso_roles": schema.SetAttribute{MarkdownDescription: "Roles the user holds through SSO group mappings, updated at each " +
				"SSO sign-in.", ElementType: types.StringType, Computed: true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}},
			"all_roles": schema.SetAttribute{MarkdownDescription: "Every role the user holds: `roles` together with `sso_roles`.",
				ElementType: types.StringType, Computed: true, PlanModifiers: []planmodifier.Set{allRolesPlanModifier{}}},
		},
	}
}

// fill copies a user into the model. prevMFA is the configured or stored mfa_required: Pulse reports true for password
// accounts whose roles demand MFA, which the flag cannot turn off, so a false value is kept in that case.
func (m *userModel) fill(u *client.User, prevMFA types.Bool) {
	m.ID = types.StringValue(u.ID)
	m.Email = optString(u.Email)
	m.DisplayName = types.StringValue(u.DisplayName)
	m.Roles = setOf(u.RolesFrom(client.RoleSourceManual))
	m.MFARequired = types.BoolValue(u.MFARequired)
	if u.MFARequired && u.HasPassword && !prevMFA.IsNull() && !prevMFA.IsUnknown() && !prevMFA.ValueBool() {
		m.MFARequired = types.BoolValue(false)
	}
	m.Enabled = types.BoolValue(u.Enabled)
	m.HasPassword = types.BoolValue(u.HasPassword)
	m.MFAEnrolled = types.BoolValue(u.MFAEnrolled)
	m.ProvisionedVia = types.StringValue(u.ProvisionedVia)
	m.SSORoles = setOf(u.RolesFrom(client.RoleSourceSSO))
	all := slices.Clone(u.Roles)
	slices.Sort(all)
	m.AllRoles = setOf(slices.Compact(all))
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.UserCreateInput{Email: plan.Email.ValueString(), DisplayName: plan.DisplayName.ValueString(),
		Roles: stringsOf(ctx, plan.Roles, &resp.Diagnostics), MFARequired: plan.MFARequired.ValueBool()}
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.client.CreateUser(ctx, in)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create user", err)
		return
	}
	if !plan.Enabled.ValueBool() {
		u2, err := r.client.UpdateUser(ctx, u.ID, client.UserUpdateInput{Enabled: ptr(false)})
		if err != nil {
			plan.fill(u, plan.MFARequired)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			ssoGuardError(&resp.Diagnostics, "User created but could not be disabled", err)
			return
		}
		u = u2
	}
	plan.fill(u, plan.MFARequired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.client.GetUser(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read user", err)
		return
	}
	state.fill(u, state.MFARequired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	roles := stringsOf(ctx, plan.Roles, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.UserUpdateInput{DisplayName: ptr(plan.DisplayName.ValueString()), Roles: &roles,
		Enabled: ptr(plan.Enabled.ValueBool()), MFARequired: ptr(plan.MFARequired.ValueBool())}
	u, err := r.client.UpdateUser(ctx, state.ID.ValueString(), in)
	if err != nil {
		ssoGuardError(&resp.Diagnostics, "Cannot update user", err)
		return
	}
	plan.fill(u, plan.MFARequired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteUser(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		ssoGuardError(&resp.Diagnostics, "Cannot delete user", err)
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !strings.Contains(req.ID, "@") {
		resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
		return
	}
	u, err := r.client.GetUserByEmail(ctx, req.ID)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot import user", err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), u.ID)...)
}

// allRolesPlanModifier plans all_roles as the configured roles plus the SSO roles from state, so it is known before
// apply.
type allRolesPlanModifier struct{}

func (allRolesPlanModifier) Description(context.Context) string {
	return "Plans all_roles as roles together with sso_roles."
}

func (m allRolesPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (allRolesPlanModifier) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var roles, sso types.Set
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("roles"), &roles)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("sso_roles"), &sso)...)
	} else {
		sso = setOf(nil) // a new user has no SSO roles
	}
	if resp.Diagnostics.HasError() || roles.IsUnknown() || sso.IsUnknown() {
		return
	}
	all := append(stringsOf(ctx, roles, &resp.Diagnostics), stringsOf(ctx, sso, &resp.Diagnostics)...)
	slices.Sort(all)
	resp.PlanValue = setOf(slices.Compact(all))
}
