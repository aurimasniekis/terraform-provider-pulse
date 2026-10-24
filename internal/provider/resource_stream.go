package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewStreamResource) }

var streamNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var _ resource.ResourceWithImportState = (*streamResource)(nil)

type streamResource struct{ baseResource }

// NewStreamResource returns the pulse_stream resource.
func NewStreamResource() resource.Resource { return &streamResource{} }

type streamModel struct {
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Subjects          types.Set    `tfsdk:"subjects"`
	Storage           types.String `tfsdk:"storage"`
	Discard           types.String `tfsdk:"discard"`
	MaxBytes          types.Int64  `tfsdk:"max_bytes"`
	MaxMsgs           types.Int64  `tfsdk:"max_msgs"`
	MaxMsgSize        types.Int64  `tfsdk:"max_msg_size"`
	MaxAgeSeconds     types.Int64  `tfsdk:"max_age_seconds"`
	DuplicatesSeconds types.Int64  `tfsdk:"duplicates_seconds"`
	Compression       types.Bool   `tfsdk:"compression"`
	AllowDirect       types.Bool   `tfsdk:"allow_direct"`
}

func (r *streamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream"
}

func (r *streamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	unlimited := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{MarkdownDescription: desc + " `-1` (the default) means unlimited.", Optional: true, Computed: true,
			Default: int64default.StaticInt64(-1), Validators: []validator.Int64{int64validator.AtLeast(-1)}}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A user stream: a JetStream stream next to the managed `EVENTS` stream, for events that need their own " +
			"retention. Its subjects must not overlap `evt.>` or Pulse's internal subjects. Deleting it deletes all its messages. " +
			"Import with the stream name.",
		Attributes: map[string]schema.Attribute{
			"name": requiredImmutableString("Stream name (letters, digits, `_` and `-`, at most 64).",
				stringvalidator.RegexMatches(streamNameRE, "must be 1-64 letters, digits, '_' or '-'")),
			"description": schema.StringAttribute{MarkdownDescription: "Description.", Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), Validators: []validator.String{stringvalidator.LengthAtMost(512)}},
			"subjects": schema.SetAttribute{MarkdownDescription: "Subjects the stream stores (wildcards allowed).",
				ElementType: types.StringType, Required: true,
				Validators: []validator.Set{setvalidator.SizeBetween(1, 32)}},
			"storage": schema.StringAttribute{MarkdownDescription: "`file` (default) or `memory`. Changing it forces a new resource.",
				Optional: true, Computed: true, Default: stringdefault.StaticString("file"),
				Validators:    []validator.String{stringvalidator.OneOf("file", "memory")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"discard": schema.StringAttribute{MarkdownDescription: "What happens when a limit is reached: `old` (default) drops the " +
				"oldest messages, `new` refuses new ones.", Optional: true, Computed: true, Default: stringdefault.StaticString("old"),
				Validators: []validator.String{stringvalidator.OneOf("old", "new")}},
			"max_bytes":    unlimited("Size limit in bytes."),
			"max_msgs":     unlimited("Message count limit."),
			"max_msg_size": unlimited("Largest message in bytes (at most 1048576)."),
			"max_age_seconds": schema.Int64Attribute{MarkdownDescription: "Maximum message age in seconds; `0` (the default) keeps " +
				"messages until another limit applies.", Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				Validators: []validator.Int64{int64validator.AtLeast(0)}},
			"duplicates_seconds": schema.Int64Attribute{MarkdownDescription: "De-duplication window for `Nats-Msg-Id` in seconds " +
				"(at most 86400). Defaults to the NATS default (120).", Optional: true, Computed: true,
				Validators:    []validator.Int64{int64validator.Between(1, 86400)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"compression": schema.BoolAttribute{MarkdownDescription: "S2 compression on disk. Default `false`.", Optional: true,
				Computed: true, Default: booldefault.StaticBool(false)},
			"allow_direct": schema.BoolAttribute{MarkdownDescription: "Allow direct get (fast reads by sequence or subject). Default `false`.",
				Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		},
	}
}

func (m *streamModel) input(ctx context.Context, diags *diag.Diagnostics) client.StreamInput {
	in := client.StreamInput{
		Name: m.Name.ValueString(), Description: m.Description.ValueString(), Subjects: stringsOf(ctx, m.Subjects, diags),
		Storage: m.Storage.ValueString(), Discard: m.Discard.ValueString(), MaxBytes: m.MaxBytes.ValueInt64(),
		MaxMsgs: m.MaxMsgs.ValueInt64(), MaxMsgSize: m.MaxMsgSize.ValueInt64(), MaxAgeSeconds: m.MaxAgeSeconds.ValueInt64(),
		Compression: m.Compression.ValueBool(), AllowDirect: m.AllowDirect.ValueBool(),
	}
	if !m.DuplicatesSeconds.IsNull() && !m.DuplicatesSeconds.IsUnknown() {
		in.DuplicatesSeconds = m.DuplicatesSeconds.ValueInt64()
	}
	return in
}

func (m *streamModel) fill(s *client.Stream) {
	m.Name = types.StringValue(s.Name)
	m.Description = types.StringValue(s.Description)
	m.Subjects = setOf(s.Subjects)
	m.Storage = types.StringValue(s.StorageName())
	m.Discard = types.StringValue(s.DiscardName())
	m.MaxBytes = types.Int64Value(s.MaxBytes)
	m.MaxMsgs = types.Int64Value(s.MaxMsgs)
	m.MaxMsgSize = types.Int64Value(s.MaxMsgSize)
	m.MaxAgeSeconds = types.Int64Value(s.MaxAgeSeconds)
	m.DuplicatesSeconds = types.Int64Value(s.DuplicatesSeconds)
	m.Compression = types.BoolValue(s.Compressed())
	m.AllowDirect = types.BoolValue(s.AllowDirect)
}

func (r *streamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.CreateStream(ctx, in)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create stream", err)
		return
	}
	plan.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *streamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.GetStream(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read stream", err)
		return
	}
	state.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *streamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.UpdateStream(ctx, in)
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot update stream", err)
		return
	}
	plan.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *streamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteStream(ctx, state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete stream", err)
	}
}

func (r *streamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
