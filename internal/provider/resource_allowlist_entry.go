package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewAllowlistEntryResource) }

var (
	_ resource.ResourceWithImportState = (*allowlistEntryResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*allowlistEntryResource)(nil)
)

type allowlistEntryResource struct{ baseResource }

// NewAllowlistEntryResource returns the pulse_allowlist_entry resource.
func NewAllowlistEntryResource() resource.Resource { return &allowlistEntryResource{} }

type allowlistEntryModel struct {
	ID      types.String `tfsdk:"id"`
	Network types.String `tfsdk:"network"`
	Note    types.String `tfsdk:"note"`
}

func (r *allowlistEntryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_allowlist_entry"
}

func (r *allowlistEntryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A network that Pulse never bans, e.g. your home or office address. Adding it also lifts active bans " +
			"inside it. Loopback and private ranges are never banned anyway and need no entry. Import with the network in the " +
			"form Pulse stores it (a CIDR such as `198.51.100.7/32`).",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("The network as Pulse stores it: a CIDR with the host bits cleared; a single IPv4 address becomes " +
				"`/32` and a single IPv6 address `/128`."),
			"network": schema.StringAttribute{MarkdownDescription: "IP address or CIDR (IPv4 or IPv6). Spellings of the same network " +
				"(e.g. `192.0.2.1` and `192.0.2.1/32`) are treated as the same network. Changing the network forces a new resource.", Required: true,
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 64), networkValidator{client.AllowNetworkKey}},
				PlanModifiers: []planmodifier.String{networkRequiresReplace(client.AllowNetworkKey)}},
			"note": schema.StringAttribute{MarkdownDescription: "Why the network is allowlisted (at most 256 characters).",
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Validators: []validator.String{stringvalidator.LengthAtMost(256)}},
		},
	}
}

func (r *allowlistEntryResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planNetworkID(ctx, &resp.Plan, client.AllowNetworkKey)
}

func (r *allowlistEntryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan allowlistEntryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := client.AllowNetworkKey(plan.Network.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("network"), "Invalid network", err.Error())
		return
	}
	// Adding an existing entry would silently take it over (Pulse only updates its note).
	if _, err := r.client.GetAllowEntry(ctx, key); err == nil {
		resp.Diagnostics.AddError("Allowlist entry already exists",
			key+" is already on the allowlist. Import it with `terraform import` to manage it.")
		return
	} else if !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot read the allowlist", err)
		return
	}
	if err := r.client.AllowNetwork(ctx, plan.Network.ValueString(), plan.Note.ValueString()); err != nil {
		apiError(&resp.Diagnostics, "Cannot add allowlist entry", err)
		return
	}
	e, err := r.client.GetAllowEntry(ctx, key)
	if err != nil {
		apiError(&resp.Diagnostics, "Allowlist entry added but cannot be read back", err)
		return
	}
	plan.ID = types.StringValue(e.Network)
	plan.Note = types.StringValue(e.Note)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *allowlistEntryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state allowlistEntryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.GetAllowEntry(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read allowlist entry", err)
		return
	}
	state.ID = types.StringValue(e.Network)
	if state.Network.IsNull() {
		state.Network = types.StringValue(e.Network) // import
	}
	state.Note = types.StringValue(e.Note)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *allowlistEntryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan allowlistEntryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Adding the same network again changes its note.
	if err := r.client.AllowNetwork(ctx, plan.ID.ValueString(), plan.Note.ValueString()); err != nil {
		apiError(&resp.Diagnostics, "Cannot update allowlist entry", err)
		return
	}
	e, err := r.client.GetAllowEntry(ctx, plan.ID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read allowlist entry", err)
		return
	}
	plan.Note = types.StringValue(e.Note)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *allowlistEntryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state allowlistEntryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DisallowNetwork(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot remove allowlist entry", err)
	}
}

func (r *allowlistEntryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, err := client.AllowNetworkKey(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected an IP address or CIDR: "+err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), key)...)
}
