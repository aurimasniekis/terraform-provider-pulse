package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewBuiltinAlertRuleResource) }

// builtinAlertDefault mirrors a built-in rule's seeded settings in Pulse (internal/alerts/rules.go): it is enabled,
// with this severity, for_seconds and params. fields lists the params the kind uses.
type builtinAlertDefault struct {
	severity   string
	forSeconds int64
	params     client.AlertRuleParams
	fields     []string
}

var builtinAlertDefaults = map[string]builtinAlertDefault{
	"disk_forecast":  {"warning", 0, client.AlertRuleParams{Days: 30}, []string{"days"}},
	"stream_usage":   {"warning", 0, client.AlertRuleParams{Ratio: 0.85}, []string{"ratio"}},
	"cert_expiry":    {"critical", 0, client.AlertRuleParams{Days: 14}, []string{"days"}},
	"backup_stale":   {"critical", 0, client.AlertRuleParams{Hours: 36}, []string{"hours"}},
	"client_expiry":  {"warning", 0, client.AlertRuleParams{Days: 30}, []string{"days"}},
	"ban_storm":      {"warning", 0, client.AlertRuleParams{Count: 10, WindowMinutes: 10}, []string{"count", "window_minutes"}},
	"slow_consumers": {"warning", 0, client.AlertRuleParams{WindowMinutes: 10}, []string{"window_minutes"}},
	"dead_letters":   {"warning", 0, client.AlertRuleParams{Hours: 24}, []string{"hours"}},
	"nats_down":      {"critical", 60, client.AlertRuleParams{}, nil},
}

var builtinAlertParamTypes = map[string]attr.Type{
	"days": types.Float64Type, "hours": types.Float64Type, "ratio": types.Float64Type, "count": types.Int64Type,
	"window_minutes": types.Int64Type,
}

var (
	_ resource.ResourceWithImportState    = (*builtinAlertRuleResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*builtinAlertRuleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*builtinAlertRuleResource)(nil)
)

type builtinAlertRuleResource struct{ baseResource }

// NewBuiltinAlertRuleResource returns the pulse_builtin_alert_rule resource.
func NewBuiltinAlertRuleResource() resource.Resource { return &builtinAlertRuleResource{} }

type builtinAlertRuleModel struct {
	ID          types.String `tfsdk:"id"`
	Kind        types.String `tfsdk:"kind"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Severity    types.String `tfsdk:"severity"`
	ForSeconds  types.Int64  `tfsdk:"for_seconds"`
	Params      types.Object `tfsdk:"params"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type builtinAlertParamsModel struct {
	Days          types.Float64 `tfsdk:"days"`
	Hours         types.Float64 `tfsdk:"hours"`
	Ratio         types.Float64 `tfsdk:"ratio"`
	Count         types.Int64   `tfsdk:"count"`
	WindowMinutes types.Int64   `tfsdk:"window_minutes"`
}

func builtinAlertKinds() []string {
	out := make([]string, 0, len(builtinAlertDefaults))
	for k := range builtinAlertDefaults {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (r *builtinAlertRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_builtin_alert_rule"
}

func (r *builtinAlertRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	var table strings.Builder
	table.WriteString("| kind | severity | for_seconds | params |\n|---|---|---|---|\n")
	for _, k := range builtinAlertKinds() {
		d := builtinAlertDefaults[k]
		var ps []string
		for _, f := range d.fields {
			ps = append(ps, fmt.Sprintf("`%s = %v`", f, builtinParamValue(d.params, f)))
		}
		if len(ps) == 0 {
			ps = []string{"none"}
		}
		fmt.Fprintf(&table, "| `%s` | `%s` | %d | %s |\n", k, d.severity, d.forSeconds, strings.Join(ps, ", "))
	}
	param := func(desc string) schema.Float64Attribute {
		return schema.Float64Attribute{MarkdownDescription: desc, Optional: true, Computed: true,
			Validators: []validator.Float64{float64validator.AtLeast(0)}}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Settings of one of Pulse's built-in alert rules. Built-in rules always exist; this resource manages " +
			"their settings. Unset attributes take the rule's built-in defaults, and destroying the resource puts all of " +
			"them back (the rule itself stays). Declare each `kind` at most once. Import with the kind, e.g. `backup_stale`.\n\n" +
			"Built-in defaults (every rule is enabled by default):\n\n" + table.String(),
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Rule ID (the same as `kind`)."),
			"kind": schema.StringAttribute{MarkdownDescription: "Which built-in rule: " + backtickList(builtinAlertKinds()) +
				". Changing it forces a new resource.", Required: true,
				Validators:    []validator.String{stringvalidator.OneOf(builtinAlertKinds()...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"enabled": schema.BoolAttribute{MarkdownDescription: "Disabled rules are not evaluated; disabling one resolves its alerts " +
				"without notifications. Default `true`.", Optional: true, Computed: true},
			"severity": schema.StringAttribute{MarkdownDescription: "`info`, `warning` or `critical`. Defaults to the rule's built-in " +
				"severity.", Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf(alertSeverities...)}},
			"for_seconds": schema.Int64Attribute{MarkdownDescription: "How long the condition must hold before the alert fires, in " +
				"seconds (at most 604800). Defaults to the rule's built-in value.", Optional: true, Computed: true,
				Validators: alertForSecondsValidators()},
			"params": schema.SingleNestedAttribute{
				MarkdownDescription: "The rule's thresholds. Each kind uses only some of them (see the table above); setting " +
					"another is an error. Unset ones take the built-in defaults; the ones the kind does not use stay null.",
				Optional: true, Computed: true,
				Attributes: map[string]schema.Attribute{
					"days": param("`disk_forecast`: fire when a stream reaches its size limit within this many days at the " +
						"current rate. `cert_expiry`, `client_expiry`: fire when less than this many days are left. Positive."),
					"hours": param("`backup_stale`: fire when no backup succeeded for this many hours. `dead_letters`: look-back " +
						"window in hours. Positive."),
					"ratio": param("`stream_usage`: fire above this fraction of a stream's size limit (between 0 and 1)."),
					"count": schema.Int64Attribute{MarkdownDescription: "`ban_storm`: number of bans within `window_minutes` " +
						"(at least 1).", Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
					"window_minutes": schema.Int64Attribute{MarkdownDescription: "`ban_storm`, `slow_consumers`: evaluation window " +
						"in minutes (at least 1).", Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
				},
			},
			"name":        computedString("Rule name shown in notifications."),
			"description": computedString("What the rule checks."),
		},
	}
}

func backtickList(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "`" + s + "`"
	}
	return strings.Join(out, ", ")
}

func builtinParamValue(p client.AlertRuleParams, field string) any {
	switch field {
	case "days":
		return p.Days
	case "hours":
		return p.Hours
	case "ratio":
		return p.Ratio
	case "count":
		return p.Count
	case "window_minutes":
		return p.WindowMinutes
	}
	return nil
}

func (r *builtinAlertRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg builtinAlertRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Kind.IsUnknown() || cfg.Params.IsNull() || cfg.Params.IsUnknown() {
		return
	}
	d, ok := builtinAlertDefaults[cfg.Kind.ValueString()]
	if !ok {
		return
	}
	for name, v := range cfg.Params.Attributes() {
		if !v.IsNull() && !slices.Contains(d.fields, name) {
			resp.Diagnostics.AddAttributeError(path.Root("params").AtName(name), "Parameter not used by this rule",
				fmt.Sprintf("The %s rule does not use params.%s; it uses %s.", cfg.Kind.ValueString(), name, paramsUsed(d.fields)))
		}
	}
	if cfg.Kind.ValueString() == "stream_usage" {
		var p builtinAlertParamsModel
		resp.Diagnostics.Append(cfg.Params.As(ctx, &p, basetypes.ObjectAsOptions{})...)
		if !p.Ratio.IsNull() && !p.Ratio.IsUnknown() && (p.Ratio.ValueFloat64() <= 0 || p.Ratio.ValueFloat64() >= 1) {
			resp.Diagnostics.AddAttributeError(path.Root("params").AtName("ratio"), "Invalid ratio", "ratio must be between 0 and 1.")
		}
	}
}

func paramsUsed(fields []string) string {
	if len(fields) == 0 {
		return "no params"
	}
	return backtickList(fields)
}

// ModifyPlan fills unset settings with the kind's built-in defaults, so removing a setting restores its default.
func (r *builtinAlertRuleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var cfg, plan builtinAlertRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || cfg.Kind.IsUnknown() {
		return
	}
	d, ok := builtinAlertDefaults[cfg.Kind.ValueString()]
	if !ok {
		return
	}
	plan.ID = cfg.Kind
	if cfg.Enabled.IsNull() {
		plan.Enabled = types.BoolValue(true)
	}
	if cfg.Severity.IsNull() {
		plan.Severity = types.StringValue(d.severity)
	}
	if cfg.ForSeconds.IsNull() {
		plan.ForSeconds = types.Int64Value(d.forSeconds)
	}
	if cfg.Params.IsUnknown() {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
	var cp builtinAlertParamsModel
	if !cfg.Params.IsNull() {
		resp.Diagnostics.Append(cfg.Params.As(ctx, &cp, basetypes.ObjectAsOptions{})...)
	} else {
		cp = builtinAlertParamsModel{Days: types.Float64Null(), Hours: types.Float64Null(), Ratio: types.Float64Null(),
			Count: types.Int64Null(), WindowMinutes: types.Int64Null()}
	}
	uses := func(f string) bool { return slices.Contains(d.fields, f) }
	f64 := func(f string, v types.Float64, def float64) attr.Value {
		switch {
		case !uses(f):
			return types.Float64Null()
		case v.IsNull():
			return types.Float64Value(def)
		}
		return v
	}
	i64 := func(f string, v types.Int64, def int64) attr.Value {
		switch {
		case !uses(f):
			return types.Int64Null()
		case v.IsNull():
			return types.Int64Value(def)
		}
		return v
	}
	obj, diags := types.ObjectValue(builtinAlertParamTypes, map[string]attr.Value{
		"days": f64("days", cp.Days, d.params.Days), "hours": f64("hours", cp.Hours, d.params.Hours),
		"ratio": f64("ratio", cp.Ratio, d.params.Ratio), "count": i64("count", cp.Count, d.params.Count),
		"window_minutes": i64("window_minutes", cp.WindowMinutes, d.params.WindowMinutes),
	})
	resp.Diagnostics.Append(diags...)
	plan.Params = obj
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (m *builtinAlertRuleModel) input(ctx context.Context, diags *diag.Diagnostics) client.AlertRuleInput {
	in := client.AlertRuleInput{Enabled: m.Enabled.ValueBool(), Severity: m.Severity.ValueString(), ForSeconds: m.ForSeconds.ValueInt64()}
	if !m.Params.IsNull() && !m.Params.IsUnknown() {
		var p builtinAlertParamsModel
		diags.Append(m.Params.As(ctx, &p, basetypes.ObjectAsOptions{})...)
		in.Params = client.AlertRuleParams{Days: p.Days.ValueFloat64(), Hours: p.Hours.ValueFloat64(), Ratio: p.Ratio.ValueFloat64(),
			Count: p.Count.ValueInt64(), WindowMinutes: p.WindowMinutes.ValueInt64()}
	}
	return in
}

func (m *builtinAlertRuleModel) fill(a *client.AlertRule) {
	m.ID = types.StringValue(a.ID)
	m.Kind = types.StringValue(a.Kind)
	m.Enabled = types.BoolValue(a.Enabled)
	m.Severity = types.StringValue(a.Severity)
	m.ForSeconds = types.Int64Value(a.ForSeconds)
	m.Name = types.StringValue(a.Name)
	m.Description = types.StringValue(a.Description)
	fields := builtinAlertDefaults[a.Kind].fields
	f64 := func(f string, v float64) attr.Value {
		if !slices.Contains(fields, f) {
			return types.Float64Null()
		}
		return types.Float64Value(v)
	}
	i64 := func(f string, v int64) attr.Value {
		if !slices.Contains(fields, f) {
			return types.Int64Null()
		}
		return types.Int64Value(v)
	}
	p := a.Params
	m.Params = types.ObjectValueMust(builtinAlertParamTypes, map[string]attr.Value{
		"days": f64("days", p.Days), "hours": f64("hours", p.Hours), "ratio": f64("ratio", p.Ratio),
		"count": i64("count", p.Count), "window_minutes": i64("window_minutes", p.WindowMinutes),
	})
}

func (r *builtinAlertRuleResource) apply(ctx context.Context, plan *builtinAlertRuleModel, diags *diag.Diagnostics, verb string) {
	in := plan.input(ctx, diags)
	if diags.HasError() {
		return
	}
	a, err := r.client.UpdateAlertRule(ctx, plan.Kind.ValueString(), in)
	if err != nil {
		apiError(diags, "Cannot "+verb+" built-in alert rule", err)
		return
	}
	plan.fill(a)
}

func (r *builtinAlertRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan builtinAlertRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics, "configure")
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *builtinAlertRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state builtinAlertRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.GetAlertRule(ctx, state.Kind.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read built-in alert rule", err)
		return
	}
	if !a.Builtin {
		resp.Diagnostics.AddError("Not a built-in alert rule", "Rule "+a.ID+" is a custom rule; manage it with pulse_alert_rule.")
		return
	}
	state.fill(a)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *builtinAlertRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan builtinAlertRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics, "update")
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete restores the rule's built-in defaults (built-in rules cannot be deleted).
func (r *builtinAlertRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state builtinAlertRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	kind := state.Kind.ValueString()
	d, ok := builtinAlertDefaults[kind]
	if !ok {
		return
	}
	in := client.AlertRuleInput{Enabled: true, Severity: d.severity, ForSeconds: d.forSeconds, Params: d.params}
	if _, err := r.client.UpdateAlertRule(ctx, kind, in); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot restore the built-in alert rule's defaults", err)
	}
}

func (r *builtinAlertRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("kind"), req, resp)
}
