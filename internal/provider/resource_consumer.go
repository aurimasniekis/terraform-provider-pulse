package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewConsumerResource) }

var consumerNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

var (
	_ resource.ResourceWithImportState    = (*consumerResource)(nil)
	_ resource.ResourceWithValidateConfig = (*consumerResource)(nil)
)

type consumerResource struct{ baseResource }

// NewConsumerResource returns the pulse_consumer resource.
func NewConsumerResource() resource.Resource { return &consumerResource{} }

type consumerModel struct {
	ID             types.String `tfsdk:"id"`
	Stream         types.String `tfsdk:"stream"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	FilterSubjects types.Set    `tfsdk:"filter_subjects"`
	DeliverPolicy  types.String `tfsdk:"deliver_policy"`
	OptStartSeq    types.Int64  `tfsdk:"opt_start_seq"`
	OptStartTime   types.String `tfsdk:"opt_start_time"`
	AckWaitSeconds types.Int64  `tfsdk:"ack_wait_seconds"`
	MaxDeliver     types.Int64  `tfsdk:"max_deliver"`
	MaxAckPending  types.Int64  `tfsdk:"max_ack_pending"`
}

// rfc3339 validates an RFC 3339 timestamp.
type rfc3339 struct{}

func (rfc3339) Description(context.Context) string {
	return "must be an RFC 3339 timestamp, e.g. 2026-01-01T00:00:00Z"
}
func (v rfc3339) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v rfc3339) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid timestamp", req.ConfigValue.ValueString()+" "+v.Description(ctx))
	}
}

func (r *consumerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_consumer"
}

func (r *consumerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A durable pull consumer (explicit acknowledgement) on a stream. It keeps its position across restarts, " +
			"so a tool resumes where it stopped.\n\n" +
			"A client may use a consumer when it holds a `durable` grant: the grant `{ action = \"durable\", name = \"orders\", " +
			"subject = \"evt.shop.>\" }` lets client `X` bind to the consumer named `X_orders`, so name it " +
			"`\"${pulse_client.shop.id}_orders\"`. Creating it here fixes its start position and filters ahead of time; the SDKs " +
			"bind to an existing consumer.\n\n" +
			"Where it starts (`deliver_policy`, `opt_start_seq`, `opt_start_time`) cannot change; changing it replaces the " +
			"consumer, which loses its position. Import with `<stream>/<name>`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("`<stream>/<name>`."),
			"stream": schema.StringAttribute{MarkdownDescription: "Stream, e.g. `EVENTS`. Changing it forces a new consumer.",
				Optional: true, Computed: true, Default: stringdefault.StaticString("EVENTS"),
				Validators:    []validator.String{stringvalidator.RegexMatches(streamNameRE, "must be a stream name")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": requiredImmutableString("Consumer (durable) name: letters, digits, `_` and `-`, at most 128.",
				stringvalidator.RegexMatches(consumerNameRE, "must be 1-128 letters, digits, '_' or '-'")),
			"description": schema.StringAttribute{MarkdownDescription: "Description.", Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), Validators: []validator.String{stringvalidator.LengthAtMost(512)}},
			"filter_subjects": schema.SetAttribute{MarkdownDescription: "Subjects the consumer delivers (wildcards allowed). Each " +
				"must overlap the stream's subjects, and for a client's consumer stay within its `durable` grant's subject.",
				ElementType: types.StringType, Required: true, Validators: []validator.Set{setvalidator.SizeBetween(1, 16)}},
			"deliver_policy": schema.StringAttribute{MarkdownDescription: "Where delivery starts: `all` (default, the whole " +
				"history), `new`, `last`, `last_per_subject`, `by_start_sequence` (needs `opt_start_seq`) or `by_start_time` " +
				"(needs `opt_start_time`). Changing it forces a new consumer.", Optional: true, Computed: true,
				Default: stringdefault.StaticString("all"),
				Validators: []validator.String{stringvalidator.OneOf("all", "new", "last", "last_per_subject", "by_start_sequence",
					"by_start_time")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"opt_start_seq": schema.Int64Attribute{MarkdownDescription: "First stream sequence for `by_start_sequence`. Changing " +
				"it forces a new consumer.", Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: replaceInt},
			"opt_start_time": schema.StringAttribute{MarkdownDescription: "Start time (RFC 3339) for `by_start_time`. Changing it " +
				"forces a new consumer.", Optional: true, Validators: []validator.String{rfc3339{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"ack_wait_seconds": schema.Int64Attribute{MarkdownDescription: "How long a delivered message may stay unacknowledged " +
				"before it is redelivered. Default 30.", Optional: true, Computed: true, Default: int64default.StaticInt64(30),
				Validators: []validator.Int64{int64validator.Between(1, 2147483647)}},
			"max_deliver": schema.Int64Attribute{MarkdownDescription: "Maximum delivery attempts per message; `-1` (the default) " +
				"is unlimited.", Optional: true, Computed: true, Default: int64default.StaticInt64(-1),
				Validators: []validator.Int64{int64validator.Between(-1, 2147483647), int64validator.NoneOf(0)}},
			"max_ack_pending": schema.Int64Attribute{MarkdownDescription: "Maximum unacknowledged messages in flight; `-1` is " +
				"unlimited. Default 1000.", Optional: true, Computed: true, Default: int64default.StaticInt64(1000),
				Validators: []validator.Int64{int64validator.Between(-1, 2147483647), int64validator.NoneOf(0)}},
		},
	}
}

func (r *consumerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m consumerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.DeliverPolicy.IsUnknown() {
		return
	}
	policy := m.DeliverPolicy.ValueString()
	if !m.OptStartSeq.IsNull() && policy != "by_start_sequence" {
		resp.Diagnostics.AddAttributeError(path.Root("opt_start_seq"), "Unexpected start sequence", "opt_start_seq needs deliver_policy = \"by_start_sequence\".")
	}
	if !m.OptStartTime.IsNull() && policy != "by_start_time" {
		resp.Diagnostics.AddAttributeError(path.Root("opt_start_time"), "Unexpected start time", "opt_start_time needs deliver_policy = \"by_start_time\".")
	}
	if policy == "by_start_sequence" && m.OptStartSeq.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("opt_start_seq"), "Missing start sequence", "deliver_policy = \"by_start_sequence\" needs opt_start_seq.")
	}
	if policy == "by_start_time" && m.OptStartTime.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("opt_start_time"), "Missing start time", "deliver_policy = \"by_start_time\" needs opt_start_time.")
	}
}

func (m *consumerModel) input(ctx context.Context, diags *diag.Diagnostics) client.ConsumerInput {
	in := client.ConsumerInput{Description: m.Description.ValueString(), FilterSubjects: stringsOf(ctx, m.FilterSubjects, diags),
		DeliverPolicy: m.DeliverPolicy.ValueString(), AckWaitSeconds: m.AckWaitSeconds.ValueInt64(),
		MaxDeliver: m.MaxDeliver.ValueInt64(), MaxAckPending: m.MaxAckPending.ValueInt64()}
	if !m.OptStartSeq.IsNull() {
		in.OptStartSeq = uint64(m.OptStartSeq.ValueInt64()) //nolint:gosec // validated ≥ 1
	}
	if !m.OptStartTime.IsNull() {
		t, err := time.Parse(time.RFC3339, m.OptStartTime.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("opt_start_time"), "Invalid timestamp", err.Error())
		}
		in.OptStartTime = &t
	}
	return in
}

func (m *consumerModel) fill(stream string, c *client.Consumer) {
	m.ID = types.StringValue(stream + "/" + c.Name)
	m.Stream = types.StringValue(stream)
	m.Name = types.StringValue(c.Name)
	m.Description = types.StringValue(c.Description)
	m.FilterSubjects = setOf(c.FilterSubjects)
	m.DeliverPolicy = types.StringValue(c.DeliverPolicy)
	m.AckWaitSeconds = types.Int64Value(c.AckWaitSeconds)
	m.MaxDeliver = types.Int64Value(c.MaxDeliver)
	m.MaxAckPending = types.Int64Value(c.MaxAckPending)
	if c.OptStartSeq > 0 {
		m.OptStartSeq = types.Int64Value(int64(c.OptStartSeq)) //nolint:gosec // stream sequences fit in int64
	} else {
		m.OptStartSeq = types.Int64Null()
	}
	// Keep the configured spelling of the start time when it denotes the same instant.
	if c.OptStartTime == nil {
		m.OptStartTime = types.StringNull()
	} else if t, err := time.Parse(time.RFC3339, m.OptStartTime.ValueString()); err != nil || !t.Equal(*c.OptStartTime) {
		m.OptStartTime = types.StringValue(c.OptStartTime.UTC().Format(time.RFC3339))
	}
}

func (r *consumerResource) put(ctx context.Context, plan *consumerModel, diags *diag.Diagnostics, summary string) {
	in := plan.input(ctx, diags)
	if diags.HasError() {
		return
	}
	c, err := r.client.PutConsumer(ctx, plan.Stream.ValueString(), plan.Name.ValueString(), in)
	if err != nil {
		apiError(diags, summary, err)
		return
	}
	plan.fill(plan.Stream.ValueString(), c)
}

func (r *consumerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan consumerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// PUT creates or updates: refuse to adopt a consumer that already exists (e.g. created by a client itself).
	if _, err := r.client.GetConsumer(ctx, plan.Stream.ValueString(), plan.Name.ValueString()); err == nil {
		resp.Diagnostics.AddError("Consumer already exists", "Consumer "+plan.Name.ValueString()+" exists on stream "+
			plan.Stream.ValueString()+"; import it with `terraform import` instead.")
		return
	}
	r.put(ctx, &plan, &resp.Diagnostics, "Cannot create consumer")
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *consumerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state consumerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetConsumer(ctx, state.Stream.ValueString(), state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read consumer", err)
		return
	}
	state.fill(state.Stream.ValueString(), c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *consumerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan consumerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.put(ctx, &plan, &resp.Diagnostics, "Cannot update consumer")
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *consumerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state consumerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteConsumer(ctx, state.Stream.ValueString(), state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete consumer", err)
	}
}

func (r *consumerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2, "<stream>/<name>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("opt_start_time"), types.StringNull())...)
}
