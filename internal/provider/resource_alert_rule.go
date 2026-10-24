package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewAlertRuleResource) }

// alertRuleNameRE rejects names with leading or trailing whitespace (Pulse trims them).
var alertRuleNameRE = regexp.MustCompile(`^\S(.*\S)?$`)

var alertSeverities = []string{"info", "warning", "critical"}

var (
	_ resource.ResourceWithImportState    = (*alertRuleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*alertRuleResource)(nil)
)

type alertRuleResource struct{ baseResource }

// NewAlertRuleResource returns the pulse_alert_rule resource.
func NewAlertRuleResource() resource.Resource { return &alertRuleResource{} }

type alertRuleModel struct {
	ID            types.String  `tfsdk:"id"`
	Name          types.String  `tfsdk:"name"`
	Severity      types.String  `tfsdk:"severity"`
	Enabled       types.Bool    `tfsdk:"enabled"`
	ForSeconds    types.Int64   `tfsdk:"for_seconds"`
	Stream        types.String  `tfsdk:"stream"`
	Dimension     types.String  `tfsdk:"dimension"`
	Key           types.String  `tfsdk:"key"`
	Metric        types.String  `tfsdk:"metric"`
	Op            types.String  `tfsdk:"op"`
	Threshold     types.Float64 `tfsdk:"threshold"`
	WindowMinutes types.Int64   `tfsdk:"window_minutes"`
	Description   types.String  `tfsdk:"description"`
}

func (r *alertRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_rule"
}

// alertForSecondsValidators validates for_seconds of both alert rule resources.
func alertForSecondsValidators() []validator.Int64 {
	return []validator.Int64{int64validator.Between(0, 604800)}
}

func (r *alertRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom threshold alert rule: it counts the events (or payload bytes) of a stream over a sliding " +
			"window, in total, for one event type or for one subject prefix, and fires when the count is above or below a " +
			"threshold. Firing and resolving alerts are sent to the configured notifiers. To tune Pulse's built-in rules use " +
			"`pulse_builtin_alert_rule`. Import with the rule ID (`custom_…`).",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Rule ID (`custom_…`)."),
			"name": schema.StringAttribute{MarkdownDescription: "Name shown in notifications (at most 100 characters).", Required: true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 100),
					stringvalidator.RegexMatches(alertRuleNameRE, "must not start or end with whitespace")}},
			"severity": schema.StringAttribute{MarkdownDescription: "`info`, `warning` (default) or `critical`.", Optional: true,
				Computed: true, Default: stringdefault.StaticString("warning"),
				Validators: []validator.String{stringvalidator.OneOf(alertSeverities...)}},
			"enabled": schema.BoolAttribute{MarkdownDescription: "Disabled rules are not evaluated; disabling one resolves its " +
				"alerts without notifications. Default `true`.", Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"for_seconds": schema.Int64Attribute{MarkdownDescription: "How long the condition must hold before the alert fires, in " +
				"seconds (at most 604800). Default `0` (fire at the first evaluation that matches).", Optional: true, Computed: true,
				Default: int64default.StaticInt64(0), Validators: alertForSecondsValidators()},
			"stream": schema.StringAttribute{MarkdownDescription: "Stream whose events are counted. Default `EVENTS`.", Optional: true,
				Computed: true, Default: stringdefault.StaticString("EVENTS"),
				Validators: []validator.String{stringvalidator.LengthBetween(1, 64)}},
			"dimension": schema.StringAttribute{MarkdownDescription: "What to count: `total` (default, every event), `type` (events " +
				"with the `ce-type` in `key`) or `prefix` (events under the subject prefix in `key`).", Optional: true, Computed: true,
				Default: stringdefault.StaticString("total"), Validators: []validator.String{stringvalidator.OneOf("total", "type", "prefix")}},
			"key": schema.StringAttribute{MarkdownDescription: "For `dimension = \"type\"`: the event type (`(untyped)` for events " +
				"without one). For `dimension = \"prefix\"`: a literal subject prefix of 1-4 tokens such as `evt.home.door`. Prefixes " +
				"of 3 tokens are counted per hour and of 4 tokens per day, so `window_minutes` must be a multiple of 60 or 1440. " +
				"Must be unset for `total`.", Optional: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 256)}},
			"metric": schema.StringAttribute{MarkdownDescription: "`msgs` (default, event count) or `bytes` (payload bytes).",
				Optional: true, Computed: true, Default: stringdefault.StaticString("msgs"),
				Validators: []validator.String{stringvalidator.OneOf("msgs", "bytes")}},
			"op": schema.StringAttribute{MarkdownDescription: "`above` fires when the count exceeds `threshold`, `below` when it " +
				"falls under it (e.g. to notice a source that went quiet).", Required: true,
				Validators: []validator.String{stringvalidator.OneOf("above", "below")}},
			"threshold": schema.Float64Attribute{MarkdownDescription: "Threshold for the count over the window (not negative).",
				Required: true, Validators: []validator.Float64{float64validator.AtLeast(0)}},
			"window_minutes": schema.Int64Attribute{MarkdownDescription: "Sliding window in minutes (1-43200).", Required: true,
				Validators: []validator.Int64{int64validator.Between(1, 43200)}},
			"description": schema.StringAttribute{MarkdownDescription: "Pulse's plain-language summary of the rule.", Computed: true,
				PlanModifiers: []planmodifier.String{keepUnlessChanged{"stream", "dimension", "key", "metric", "op", "threshold", "window_minutes"}}},
		},
	}
}

func (r *alertRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var dim, key types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("dimension"), &dim)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("key"), &key)...)
	if dim.IsUnknown() || key.IsUnknown() {
		return
	}
	switch d := dim.ValueString(); {
	case (d == "" || d == "total") && !key.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("key"), "Unexpected key", `key must be unset when dimension is "total"`)
	case (d == "type" || d == "prefix") && key.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("key"), "Missing key", `key is required when dimension is "`+d+`"`)
	}
}

func (m *alertRuleModel) input() client.AlertRuleInput {
	return client.AlertRuleInput{Name: m.Name.ValueString(), Enabled: m.Enabled.ValueBool(), Severity: m.Severity.ValueString(),
		ForSeconds: m.ForSeconds.ValueInt64(), Params: client.AlertRuleParams{Stream: m.Stream.ValueString(),
			Dimension: m.Dimension.ValueString(), Key: m.Key.ValueString(), Metric: m.Metric.ValueString(), Op: m.Op.ValueString(),
			Threshold: m.Threshold.ValueFloat64(), WindowMinutes: m.WindowMinutes.ValueInt64()}}
}

func (m *alertRuleModel) fill(a *client.AlertRule) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Severity = types.StringValue(a.Severity)
	m.Enabled = types.BoolValue(a.Enabled)
	m.ForSeconds = types.Int64Value(a.ForSeconds)
	p := a.Params
	m.Stream = types.StringValue(p.Stream)
	m.Dimension = types.StringValue(p.Dimension)
	m.Key = optString(p.Key)
	m.Metric = types.StringValue(p.Metric)
	m.Op = types.StringValue(p.Op)
	m.Threshold = types.Float64Value(p.Threshold)
	m.WindowMinutes = types.Int64Value(p.WindowMinutes)
	m.Description = types.StringValue(a.Description)
}

func (r *alertRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alertRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.CreateAlertRule(ctx, plan.input())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create alert rule", err)
		return
	}
	plan.fill(a)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alertRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.GetAlertRule(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read alert rule", err)
		return
	}
	if a.Builtin {
		resp.Diagnostics.AddError("Not a custom alert rule", "Rule "+a.ID+" is built in; manage it with pulse_builtin_alert_rule.")
		return
	}
	state.fill(a)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *alertRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan alertRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.UpdateAlertRule(ctx, plan.ID.ValueString(), plan.input())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot update alert rule", err)
		return
	}
	plan.fill(a)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alertRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAlertRule(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete alert rule", err)
	}
}

func (r *alertRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
