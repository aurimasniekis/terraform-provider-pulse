package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func init() { registerDataSource(NewStreamDataSource) }

type streamDataSource struct{ baseDataSource }

// NewStreamDataSource returns the pulse_stream data source.
func NewStreamDataSource() datasource.DataSource { return &streamDataSource{} }

type streamDataModel struct {
	streamModel
	Managed types.Bool   `tfsdk:"managed"`
	Kind    types.String `tfsdk:"kind"`
}

func (d *streamDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream"
}

func (d *streamDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	num := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads any stream, including the managed `EVENTS` stream, for example to use its subjects or limits.",
		Attributes: map[string]schema.Attribute{
			"name":               schema.StringAttribute{MarkdownDescription: "Stream name, e.g. `EVENTS`.", Required: true},
			"description":        computed("Description."),
			"subjects":           schema.SetAttribute{MarkdownDescription: "Subjects.", ElementType: types.StringType, Computed: true},
			"storage":            computed("`file` or `memory`."),
			"discard":            computed("`old` or `new`."),
			"max_bytes":          num("Size limit (-1 = unlimited)."),
			"max_msgs":           num("Message count limit (-1 = unlimited)."),
			"max_msg_size":       num("Largest message (-1 = unlimited)."),
			"max_age_seconds":    num("Maximum age in seconds (0 = none)."),
			"duplicates_seconds": num("De-duplication window in seconds."),
			"compression":        schema.BoolAttribute{MarkdownDescription: "S2 compression.", Computed: true},
			"allow_direct":       schema.BoolAttribute{MarkdownDescription: "Direct get allowed.", Computed: true},
			"managed":            schema.BoolAttribute{MarkdownDescription: "Owned by pulsed (cannot be managed with `pulse_stream`).", Computed: true},
			"kind":               computed("`stream`, `kv` (a KV bucket's stream) or `object` (an object store's stream)."),
		},
	}
}

func (d *streamDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m streamDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := d.client.GetStream(ctx, m.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read stream", err)
		return
	}
	m.fill(s)
	m.Managed, m.Kind = types.BoolValue(s.Managed), types.StringValue(s.Kind)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
