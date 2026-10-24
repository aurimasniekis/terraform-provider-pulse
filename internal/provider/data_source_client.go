package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerDataSource(NewClientDataSource) }

var _ datasource.DataSourceWithConfigValidators = (*clientDataSource)(nil)

type clientDataSource struct{ baseDataSource }

// NewClientDataSource returns the pulse_client data source.
func NewClientDataSource() datasource.DataSource { return &clientDataSource{} }

type clientDataModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Tags        types.Set    `tfsdk:"tags"`
	Grants      types.Set    `tfsdk:"grants"`
	UserKey     types.String `tfsdk:"user_key"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
	Status      types.String `tfsdk:"status"`
}

func (d *clientDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_client"
}

func (d *clientDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name"))}
}

func (d *clientDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a client (created in the dashboard or elsewhere) by ID or name, for example to name its " +
			"durable consumer (`<id>_<name>`). Its credentials are never readable.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{MarkdownDescription: "Client ID. Set exactly one of `id` and `name`.", Optional: true, Computed: true},
			"name":        schema.StringAttribute{MarkdownDescription: "Client name.", Optional: true, Computed: true},
			"description": computed("Description."),
			"tags":        schema.SetAttribute{MarkdownDescription: "Tags.", ElementType: types.StringType, Computed: true},
			"grants": schema.SetNestedAttribute{MarkdownDescription: "Grants.", Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"action":  computed("Action."),
					"subject": computed("Subject pattern."),
					"stream":  computed("Stream (subscribe and durable grants)."),
					"name":    computed("Durable name (durable grants)."),
				}}},
			"user_key":   computed("Current NATS user public key."),
			"expires_at": computed("When the credentials expire (RFC 3339)."),
			"status":     computed("`active`, `expired` or `revoked`."),
		},
	}
}

func (d *clientDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m clientDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var c *client.NATSClient
	var err error
	if !m.ID.IsNull() {
		c, err = d.client.GetClient(ctx, m.ID.ValueString())
	} else {
		c, err = d.client.FindClient(ctx, m.Name.ValueString())
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read client", err)
		return
	}
	var rm clientModel
	rm.fill(c)
	m.ID, m.Name, m.Description, m.Tags, m.Grants, m.UserKey = rm.ID, rm.Name, rm.Description, rm.Tags, rm.Grants, rm.UserKey
	m.ExpiresAt = types.StringValue(c.ExpiresAt.UTC().Format(time.RFC3339))
	m.Status = rm.Status
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
