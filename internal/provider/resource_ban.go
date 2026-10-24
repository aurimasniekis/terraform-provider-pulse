package provider

import (
	"context"
	"math"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

func init() { registerResource(NewBanResource) }

var (
	_ resource.ResourceWithImportState = (*banResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*banResource)(nil)
)

type banResource struct{ baseResource }

// NewBanResource returns the pulse_ban resource.
func NewBanResource() resource.Resource { return &banResource{} }

type banModel struct {
	ID            types.String `tfsdk:"id"`
	Network       types.String `tfsdk:"network"`
	Reason        types.String `tfsdk:"reason"`
	DurationHours types.Int64  `tfsdk:"duration_hours"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
}

func (r *banResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ban"
}

func (r *banResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A manual ban: Pulse refuses connections and requests from the network on its public listeners " +
			"(and feeds it to the edge firewall and Cloudflare list when those are set up). Destroying the resource lifts the ban.\n\n" +
			"Pulse refuses to ban loopback, private and link-local ranges, allowlisted networks and Cloudflare's ranges. A " +
			"temporary ban (`duration_hours` > 0) disappears from Pulse when it expires; the next plan then creates it again. " +
			"Use `duration_hours = 0` for a permanent ban. Changing any setting forces a new resource (the ban is lifted and placed again). Import with the " +
			"network in the form Pulse stores it (e.g. `203.0.113.7/32` or `2001:db8:1:2::/64`).",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("The network as Pulse stores it: a CIDR with the host bits cleared; a single IPv4 address becomes " +
				"`/32` and a single IPv6 address its `/64` (one host usually controls the whole /64)."),
			"network": schema.StringAttribute{MarkdownDescription: "IP address or CIDR to ban (IPv4 or IPv6). Spellings of the same " +
				"network (e.g. `203.0.113.7` and `203.0.113.7/32`) are treated as the same network. Changing the network forces a new resource.",
				Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 64), networkValidator{client.BanNetworkKey}},
				PlanModifiers: []planmodifier.String{networkRequiresReplace(client.BanNetworkKey)}},
			"reason": schema.StringAttribute{MarkdownDescription: "Why the network is banned (at most 256 characters). Changing it " +
				"forces a new resource.", Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Validators:    []validator.String{stringvalidator.LengthAtMost(256)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"duration_hours": schema.Int64Attribute{MarkdownDescription: "How long the ban lasts, in hours (at most 8760). `0` (the " +
				"default) means it never expires. Changing it forces a new resource, which starts a new period.", Optional: true,
				Computed: true, Default: int64default.StaticInt64(0), Validators: []validator.Int64{int64validator.Between(0, 8760)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"expires_at": computedString("When the ban expires (RFC 3339); null for a permanent ban."),
		},
	}
}

func (r *banResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planNetworkID(ctx, &resp.Plan, client.BanNetworkKey)
}

func (m *banModel) fill(b *client.Ban) {
	m.ID = types.StringValue(b.Network)
	m.Reason = types.StringValue(b.Reason)
	if b.ExpiresAt.IsZero() {
		m.ExpiresAt = types.StringNull()
	} else {
		m.ExpiresAt = types.StringValue(b.ExpiresAt.UTC().Format(time.RFC3339))
	}
}

func (r *banResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan banModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.CreateBan(ctx, client.BanInput{Network: plan.Network.ValueString(), Reason: plan.Reason.ValueString(),
		DurationHours: plan.DurationHours.ValueInt64()})
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot ban network", err)
		return
	}
	plan.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *banResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state banModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetBan(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx) // lifted or expired
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read ban", err)
		return
	}
	state.fill(b)
	if state.Network.IsNull() { // import
		state.Network = types.StringValue(b.Network)
		hours := int64(0)
		if !b.ExpiresAt.IsZero() {
			hours = int64(math.Round(b.ExpiresAt.Sub(b.CreatedAt).Hours()))
		}
		state.DurationHours = types.Int64Value(hours)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only runs when the network is rewritten in an equivalent form (everything else forces a new resource):
// nothing changes in Pulse.
func (r *banResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state banModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.ExpiresAt = state.ID, state.ExpiresAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *banResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state banModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.LiftBan(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot lift ban", err)
	}
}

func (r *banResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, err := client.BanNetworkKey(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected an IP address or CIDR: "+err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), key)...)
}
