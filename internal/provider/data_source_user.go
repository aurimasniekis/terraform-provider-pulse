package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerDataSource(NewUserDataSource) }

var _ datasource.DataSourceWithConfigValidators = (*userDataSource)(nil)

type userDataSource struct{ baseDataSource }

// NewUserDataSource returns the pulse_user data source.
func NewUserDataSource() datasource.DataSource { return &userDataSource{} }

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedBool := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{MarkdownDescription: desc, Computed: true}
	}
	computedSet := func(desc string) schema.SetAttribute {
		return schema.SetAttribute{MarkdownDescription: desc, ElementType: types.StringType, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a dashboard user by ID or email, for example to link an SSO identity to a user that " +
			"was created on the dashboard or by just-in-time provisioning. Set exactly one of `id` and `email`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{MarkdownDescription: "User ID. Set this or `email`.", Optional: true, Computed: true},
			"email": schema.StringAttribute{MarkdownDescription: "Email address (case-insensitive). Set this or `id`.",
				Optional: true, Computed: true},
			"display_name":    schema.StringAttribute{MarkdownDescription: "Name shown in the dashboard.", Computed: true},
			"roles":           computedSet("Roles assigned to the user directly."),
			"sso_roles":       computedSet("Roles the user holds through SSO group mappings."),
			"all_roles":       computedSet("Every role the user holds."),
			"mfa_required":    computedBool("Whether the user must use an authenticator app for password sign-in."),
			"enabled":         computedBool("Whether the user can sign in."),
			"has_password":    computedBool("Whether the user has set a password."),
			"mfa_enrolled":    computedBool("Whether the user has enrolled an authenticator app."),
			"provisioned_via": schema.StringAttribute{MarkdownDescription: "How the user was created (`admin`, `jit`, …).", Computed: true},
		},
	}
}

func (d *userDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("email"))}
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var u *client.User
	var err error
	if !m.ID.IsNull() {
		u, err = d.client.GetUser(ctx, m.ID.ValueString())
	} else {
		u, err = d.client.GetUserByEmail(ctx, m.Email.ValueString())
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read user", err)
		return
	}
	cfgEmail := m.Email
	m.fill(u, types.BoolNull())
	if !cfgEmail.IsNull() {
		m.Email = cfgEmail // keep the configured spelling
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
