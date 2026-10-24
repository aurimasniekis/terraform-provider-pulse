package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func init() { registerDataSource(NewSystemInfoDataSource) }

type systemInfoDataSource struct{ baseDataSource }

// NewSystemInfoDataSource returns the pulse_system_info data source.
func NewSystemInfoDataSource() datasource.DataSource { return &systemInfoDataSource{} }

type systemInfoModel struct {
	ID          types.String `tfsdk:"id"`
	Version     types.String `tfsdk:"version"`
	Profile     types.String `tfsdk:"profile"`
	NATSURL     types.String `tfsdk:"nats_url"`
	PublicURL   types.String `tfsdk:"public_url"`
	CACert      types.String `tfsdk:"ca_cert"`
	NATSVersion types.String `tfsdk:"nats_version"`
}

func (d *systemInfoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_info"
}

func (d *systemInfoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The server's version and the connection details tools need: where to connect over NATS, the public " +
			"HTTP API URL and the internal CA certificate (needed while port 4222 serves Pulse's own bootstrap certificate).",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{MarkdownDescription: "Always `system`.", Computed: true},
			"version":      schema.StringAttribute{MarkdownDescription: "pulsed version.", Computed: true},
			"profile":      schema.StringAttribute{MarkdownDescription: "`prod` or `dev`.", Computed: true},
			"nats_url":     schema.StringAttribute{MarkdownDescription: "Where tools connect (raw NATS over TLS).", Computed: true},
			"public_url":   schema.StringAttribute{MarkdownDescription: "Base URL of the public HTTP API.", Computed: true},
			"ca_cert":      schema.StringAttribute{MarkdownDescription: "Internal CA certificate (PEM).", Computed: true},
			"nats_version": schema.StringAttribute{MarkdownDescription: "NATS server version.", Computed: true},
		},
	}
}

func (d *systemInfoDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	info, err := d.client.GetSystemInfo(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read system information", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &systemInfoModel{
		ID: types.StringValue("system"), Version: types.StringValue(info.Version), Profile: types.StringValue(info.Profile),
		NATSURL: types.StringValue(info.Connect.NATSURL), PublicURL: types.StringValue(info.Connect.PublicURL),
		CACert: types.StringValue(info.Connect.CACert), NATSVersion: types.StringValue(info.NATS.Version),
	})...)
}
