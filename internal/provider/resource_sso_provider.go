package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewSSOProviderResource) }

var (
	ssoSlugRE  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	ssoTokenRE = regexp.MustCompile(`^[^\s"]+$`)
)

var ssoDefaultScopes = []string{"openid", "profile", "email"}

var _ resource.ResourceWithImportState = (*ssoProviderResource)(nil)

type ssoProviderResource struct{ baseResource }

// NewSSOProviderResource returns the pulse_sso_provider resource.
func NewSSOProviderResource() resource.Resource { return &ssoProviderResource{} }

type ssoProviderModel struct {
	ID                        types.String `tfsdk:"id"`
	Slug                      types.String `tfsdk:"slug"`
	DisplayName               types.String `tfsdk:"display_name"`
	Issuer                    types.String `tfsdk:"issuer"`
	ClientID                  types.String `tfsdk:"client_id"`
	ClientSecret              types.String `tfsdk:"client_secret"`
	HasClientSecret           types.Bool   `tfsdk:"has_client_secret"`
	Scopes                    types.List   `tfsdk:"scopes"`
	GroupsClaim               types.String `tfsdk:"groups_claim"`
	Provisioning              types.String `tfsdk:"provisioning"`
	JIT                       types.Object `tfsdk:"jit"`
	StepUpFallback            types.String `tfsdk:"stepup_fallback"`
	ACRValues                 types.List   `tfsdk:"acr_values"`
	Enabled                   types.Bool   `tfsdk:"enabled"`
	EndSessionEnabled         types.Bool   `tfsdk:"end_session_enabled"`
	AllowPrivateNetwork       types.Bool   `tfsdk:"allow_private_network"`
	AllowCrossOriginEndpoints types.Bool   `tfsdk:"allow_cross_origin_endpoints"`
	CABundlePEM               types.String `tfsdk:"ca_bundle_pem"`
	RedirectURI               types.String `tfsdk:"redirect_uri"`
	TestLoginOKAt             types.String `tfsdk:"test_login_ok_at"`
}

type ssoJITModel struct {
	EmailDomains  types.Set `tfsdk:"email_domains"`
	Groups        types.Set `tfsdk:"groups"`
	HostedDomains types.Set `tfsdk:"hosted_domains"`
	Tenants       types.Set `tfsdk:"tenants"`
}

var ssoJITAttrTypes = map[string]attr.Type{
	"email_domains":  types.SetType{ElemType: types.StringType},
	"groups":         types.SetType{ElemType: types.StringType},
	"hosted_domains": types.SetType{ElemType: types.StringType},
	"tenants":        types.SetType{ElemType: types.StringType},
}

func ssoJITObject(j client.JITRules) types.Object {
	return types.ObjectValueMust(ssoJITAttrTypes, map[string]attr.Value{
		"email_domains": setOf(j.EmailDomains), "groups": setOf(j.Groups),
		"hosted_domains": setOf(j.HostedDomains), "tenants": setOf(j.Tenants),
	})
}

func (r *ssoProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_provider"
}

func (r *ssoProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optBool := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{MarkdownDescription: desc + " Default `false`.", Optional: true, Computed: true,
			Default: booldefault.StaticBool(false)}
	}
	jitSet := func(desc string, v ...validator.String) schema.SetAttribute {
		return schema.SetAttribute{MarkdownDescription: desc, ElementType: types.StringType, Optional: true, Computed: true,
			Default:    setdefault.StaticValue(setOf(nil)),
			Validators: []validator.Set{setvalidator.ValueStringsAre(append([]validator.String{stringvalidator.LengthBetween(1, 256)}, v...)...)}}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OpenID Connect provider people can use to sign in to the Pulse dashboard (for example Google " +
			"Workspace, Microsoft Entra ID, Okta, Authentik or Keycloak).\n\n" +
			"Register an OAuth client (web application, Authorization Code flow) at the provider with `redirect_uri` as its " +
			"redirect URI, then set `issuer`, `client_id` and `client_secret` here. Pulse contacts the issuer (OpenID discovery) " +
			"only when someone signs in, so a wrong issuer shows up at the first sign-in, not at apply. The issuer must use " +
			"`https`, and Pulse refuses issuers and endpoints on private networks unless `allow_private_network` is set.\n\n" +
			"New providers are disabled by default. Run a test login from the dashboard (Settings, Single sign-on, Test " +
			"login) before you enable the provider or turn off password sign-in with `pulse_auth_settings`. Changing `issuer` " +
			"or `client_id` clears the recorded test login.\n\n" +
			"Who may sign in: with `provisioning = \"preprovisioned\"` (the default) only users whose identity is linked with " +
			"`pulse_user_identity`; with `\"jit\"` Pulse also creates a user at the first sign-in when every rule in `jit` " +
			"matches. Map provider groups to roles with `pulse_sso_mapping`.\n\n" +
			"Deleting the provider unlinks all its identities and removes the roles users got from its group mappings. Import " +
			"with the provider ID or its slug.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Provider ID."),
			"slug": schema.StringAttribute{MarkdownDescription: "Short name used in sign-in URLs (`/auth/oidc/<slug>/start`): a " +
				"lowercase letter followed by up to 31 lowercase letters, digits or `-`. Can be changed in place.",
				Required: true, Validators: []validator.String{stringvalidator.RegexMatches(ssoSlugRE,
					"must be a lowercase letter followed by up to 31 lowercase letters, digits or '-'")}},
			"display_name": schema.StringAttribute{MarkdownDescription: "Name on the sign-in button (at most 64 bytes).",
				Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 64), ssoTrimmedValidator{}}},
			"issuer": schema.StringAttribute{MarkdownDescription: "Issuer URL exactly as the provider publishes it in its " +
				"discovery document (`<issuer>/.well-known/openid-configuration`), for example `https://accounts.google.com`. " +
				"Must use `https` (the dev profile also accepts `http://localhost`).",
				Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 512), ssoTrimmedValidator{}}},
			"client_id": schema.StringAttribute{MarkdownDescription: "OAuth client ID registered at the provider (at most 256 bytes).",
				Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 256), ssoTrimmedValidator{}}},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "OAuth client secret (at most 1024 bytes). Pulse stores it encrypted and never returns it, so " +
					"it is sent only when created or when its value here changes; removing it from the configuration removes it " +
					"from Pulse. Leave it out for public clients. A secret changed outside Terraform is not detected unless it " +
					"was removed.",
				Optional: true, Sensitive: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 1024)},
			},
			"has_client_secret": schema.BoolAttribute{MarkdownDescription: "Whether Pulse holds a client secret for this provider.",
				Computed: true, PlanModifiers: []planmodifier.Bool{ssoHasSecretPlanModifier{}}},
			"scopes": schema.ListAttribute{MarkdownDescription: "Scopes requested at sign-in; must include `openid`. Default " +
				"`[\"openid\", \"profile\", \"email\"]`. Add the scope your provider needs for a groups claim, if any.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: listdefault.StaticValue(listOf(ssoDefaultScopes)),
				Validators: []validator.List{listvalidator.SizeAtLeast(1), listvalidator.ValueStringsAre(
					stringvalidator.LengthBetween(1, 64), stringvalidator.RegexMatches(ssoTokenRE, "must not contain whitespace or quotes"))}},
			"groups_claim": schema.StringAttribute{MarkdownDescription: "Name of the ID token claim that lists the person's groups " +
				"(for example `groups`), used by `pulse_sso_mapping` and the `jit.groups` rule. Pulse 0.1.1 and later also read a " +
				"nested claim by dotted path, such as `porch.groups` for Porch. Default empty (no groups).",
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Validators: []validator.String{stringvalidator.LengthAtMost(64), ssoTrimmedValidator{}}},
			"provisioning": schema.StringAttribute{MarkdownDescription: "`preprovisioned` (default): only users with a linked " +
				"identity can sign in. `jit`: Pulse also creates a user at their first sign-in when every rule in `jit` matches " +
				"(at least one rule is required).",
				Optional: true, Computed: true, Default: stringdefault.StaticString("preprovisioned"),
				Validators: []validator.String{stringvalidator.OneOf("preprovisioned", "jit")}},
			"jit": schema.SingleNestedAttribute{
				MarkdownDescription: "Rules a new person must match to be created at their first sign-in when `provisioning = " +
					"\"jit\"`. Every rule that is set must match.",
				Optional: true, Computed: true,
				Default: objectdefault.StaticValue(ssoJITObject(client.JITRules{})),
				Attributes: map[string]schema.Attribute{
					"email_domains": jitSet("Email domains (lower case, such as `example.com`); the ID token must carry a verified "+
						"email in one of them.", ssoLowercaseValidator{what: "email domain"}),
					"groups":         jitSet("Values of the groups claim; the person must be in one of them. Needs `groups_claim`."),
					"hosted_domains": jitSet("Google Workspace domains, matched against the `hd` claim."),
					"tenants":        jitSet("Microsoft Entra ID tenant IDs, matched against the `tid` claim."),
				},
			},
			"stepup_fallback": schema.StringAttribute{MarkdownDescription: "What Pulse does when a sensitive action needs fresh " +
				"authentication and the provider cannot provide it: `none` (default; sign in with the provider again) or " +
				"`local_totp` (accept the user's Pulse authenticator code).",
				Optional: true, Computed: true, Default: stringdefault.StaticString("none"),
				Validators: []validator.String{stringvalidator.OneOf("none", "local_totp")}},
			"acr_values": schema.ListAttribute{MarkdownDescription: "Authentication context class references to request " +
				"(`acr_values`), for example to ask the provider for multi-factor sign-in. Default none.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: listdefault.StaticValue(listOf(nil)),
				Validators: []validator.List{listvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 128),
					stringvalidator.RegexMatches(ssoTokenRE, "must not contain whitespace or quotes"))}},
			"enabled": optBool("Whether the provider is offered on the sign-in page. Run a test login first."),
			"end_session_enabled": optBool("Also sign the person out at the provider when they sign out of Pulse (when the " +
				"provider supports it)."),
			"allow_private_network": optBool("Allow an issuer and endpoints on private or loopback addresses (for a provider " +
				"in your own network)."),
			"allow_cross_origin_endpoints": optBool("Allow the discovery document to point to endpoints on another origin than " +
				"the issuer."),
			"ca_bundle_pem": schema.StringAttribute{MarkdownDescription: "PEM certificates to trust for the provider's TLS " +
				"certificate in addition to the system roots (for a private certificate authority). Default empty.",
				Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"redirect_uri": computedString("Redirect URI to register at the provider for this client."),
			"test_login_ok_at": schema.StringAttribute{MarkdownDescription: "When an administrator last completed a test login " +
				"with this provider (RFC 3339), or null. Cleared when `issuer` or `client_id` changes.",
				Computed: true, PlanModifiers: []planmodifier.String{ssoTestLoginPlanModifier{}}},
		},
	}
}

// input builds a full provider input from the plan (every field is sent; the secret only when sendSecret).
func (m *ssoProviderModel) input(ctx context.Context, diags *diag.Diagnostics) client.SSOProviderInput {
	var jit ssoJITModel
	diags.Append(m.JIT.As(ctx, &jit, basetypes.ObjectAsOptions{})...)
	return client.SSOProviderInput{
		Slug: ptr(m.Slug.ValueString()), DisplayName: ptr(m.DisplayName.ValueString()), Issuer: ptr(m.Issuer.ValueString()),
		ClientID: ptr(m.ClientID.ValueString()), Scopes: ptr(stringsOf(ctx, m.Scopes, diags)),
		GroupsClaim: ptr(m.GroupsClaim.ValueString()), Provisioning: ptr(m.Provisioning.ValueString()),
		JIT: &client.JITRules{EmailDomains: stringsOf(ctx, jit.EmailDomains, diags), Groups: stringsOf(ctx, jit.Groups, diags),
			HostedDomains: stringsOf(ctx, jit.HostedDomains, diags), Tenants: stringsOf(ctx, jit.Tenants, diags)},
		StepUpFallback: ptr(m.StepUpFallback.ValueString()), ACRValues: ptr(stringsOf(ctx, m.ACRValues, diags)),
		Enabled: ptr(m.Enabled.ValueBool()), EndSessionEnabled: ptr(m.EndSessionEnabled.ValueBool()),
		AllowPrivateNetwork: ptr(m.AllowPrivateNetwork.ValueBool()), AllowCrossOriginEndpoints: ptr(m.AllowCrossOriginEndpoints.ValueBool()),
		CABundlePEM: ptr(m.CABundlePEM.ValueString()),
	}
}

// fill copies a provider into the model; client_secret is kept, except that it is cleared when Pulse holds no secret
// (so a secret removed outside Terraform is set again).
func (m *ssoProviderModel) fill(p *client.SSOProvider) {
	m.ID = types.StringValue(p.ID)
	m.Slug = types.StringValue(p.Slug)
	m.DisplayName = types.StringValue(p.DisplayName)
	m.Issuer = types.StringValue(p.Issuer)
	m.ClientID = types.StringValue(p.ClientID)
	if !p.HasClientSecret {
		m.ClientSecret = types.StringNull()
	}
	m.HasClientSecret = types.BoolValue(p.HasClientSecret)
	m.Scopes = listOf(p.Scopes)
	m.GroupsClaim = types.StringValue(p.GroupsClaim)
	m.Provisioning = types.StringValue(p.Provisioning)
	m.JIT = ssoJITObject(p.JIT)
	m.StepUpFallback = types.StringValue(p.StepUpFallback)
	m.ACRValues = listOf(p.ACRValues)
	m.Enabled = types.BoolValue(p.Enabled)
	m.EndSessionEnabled = types.BoolValue(p.EndSessionEnabled)
	m.AllowPrivateNetwork = types.BoolValue(p.AllowPrivateNetwork)
	m.AllowCrossOriginEndpoints = types.BoolValue(p.AllowCrossOriginEndpoints)
	m.CABundlePEM = types.StringValue(p.CABundlePEM)
	m.RedirectURI = types.StringValue(p.RedirectURI)
	m.TestLoginOKAt = types.StringNull()
	if p.TestLoginOKAt != nil {
		m.TestLoginOKAt = types.StringValue(p.TestLoginOKAt.UTC().Format(time.RFC3339))
	}
}

func (r *ssoProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if s := plan.ClientSecret.ValueString(); s != "" {
		in.ClientSecret = &s
	}
	p, err := r.client.CreateSSOProvider(ctx, in)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create SSO provider", err)
		return
	}
	plan.fill(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ssoProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.GetSSOProvider(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read SSO provider", err)
		return
	}
	state.fill(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ssoProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ssoProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.ClientSecret.Equal(state.ClientSecret) {
		in.ClientSecret = ptr(plan.ClientSecret.ValueString()) // "" removes the secret
	}
	// Pulse uses optimistic concurrency: send the version of a fresh read.
	cur, err := r.client.GetSSOProvider(ctx, state.ID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read SSO provider before updating it", err)
		return
	}
	in.Version = cur.Version
	p, err := r.client.UpdateSSOProvider(ctx, state.ID.ValueString(), in)
	if err != nil {
		ssoGuardError(&resp.Diagnostics, "Cannot update SSO provider", err)
		return
	}
	plan.fill(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ssoProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSSOProvider(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		ssoGuardError(&resp.Diagnostics, "Cannot delete SSO provider", err)
	}
}

func (r *ssoProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	p, err := r.client.GetSSOProvider(ctx, req.ID)
	if client.IsNotFound(err) && ssoSlugRE.MatchString(req.ID) {
		p, err = r.client.GetSSOProviderBySlug(ctx, req.ID)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot import SSO provider", err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.ID)...)
}

// ssoHasSecretPlanModifier plans has_client_secret from client_secret: unchanged secret keeps the state value, a new
// secret means true and a removed one false.
type ssoHasSecretPlanModifier struct{}

func (ssoHasSecretPlanModifier) Description(context.Context) string {
	return "Follows changes of client_secret."
}

func (m ssoHasSecretPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (ssoHasSecretPlanModifier) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var planSecret types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("client_secret"), &planSecret)...)
	if resp.Diagnostics.HasError() || planSecret.IsUnknown() {
		return
	}
	if !req.State.Raw.IsNull() {
		var stateSecret types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("client_secret"), &stateSecret)...)
		if planSecret.Equal(stateSecret) {
			resp.PlanValue = req.StateValue
			return
		}
	}
	resp.PlanValue = types.BoolValue(planSecret.ValueString() != "")
}

// ssoTestLoginPlanModifier keeps test_login_ok_at unless issuer or client_id changes (Pulse then clears it); a new
// provider has none.
type ssoTestLoginPlanModifier struct{}

func (ssoTestLoginPlanModifier) Description(context.Context) string {
	return "Kept unless issuer or client_id changes."
}

func (m ssoTestLoginPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (ssoTestLoginPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	if req.State.Raw.IsNull() {
		resp.PlanValue = types.StringNull()
		return
	}
	for _, a := range []string{"issuer", "client_id"} {
		var pv, sv types.String
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(a), &pv)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(a), &sv)...)
		if pv.IsUnknown() {
			return
		}
		if !pv.Equal(sv) {
			resp.PlanValue = types.StringNull()
			return
		}
	}
	resp.PlanValue = req.StateValue
}
