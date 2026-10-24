package provider

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewHTTPTokenResource) }

var _ resource.ResourceWithImportState = (*httpTokenResource)(nil)

type httpTokenResource struct{ baseResource }

// NewHTTPTokenResource returns the pulse_http_token resource.
func NewHTTPTokenResource() resource.Resource { return &httpTokenResource{} }

type httpTokenModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Grants        types.Set    `tfsdk:"grants"`
	CIDRs         types.Set    `tfsdk:"cidrs"`
	ExpiresInDays types.Int64  `tfsdk:"expires_in_days"`
	Secret        types.String `tfsdk:"secret"`
	Prefix        types.String `tfsdk:"prefix"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	Status        types.String `tfsdk:"status"`
}

// canonicalCIDR accepts only the form Pulse stores (an address with its prefix length, host bits zero), so plans never
// show a diff after the server normalises a value.
type canonicalCIDR struct{}

func (canonicalCIDR) Description(context.Context) string {
	return "must be a CIDR in canonical form, e.g. 192.0.2.0/24 or 2001:db8::/32 (a single address: 192.0.2.10/32)"
}
func (v canonicalCIDR) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v canonicalCIDR) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	p, err := netip.ParsePrefix(s)
	if err != nil || p.Masked().String() != s {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid network", s+" "+v.Description(ctx))
	}
}

func (r *httpTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_http_token"
}

func (r *httpTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceSet := []planmodifier.Set{setplanmodifier.RequiresReplace()}
	grants := grantsAttribute("What the token may do over the HTTP API: `publish` (and batch publish) and `request` take a "+
		"`subject`; `subscribe` takes a `subject` and optionally a `stream` (replay and the live tail). Changing it forces a "+
		"new token.", []string{"publish", "subscribe", "request"})
	grants.PlanModifiers = replaceSet
	resp.Schema = schema.Schema{
		MarkdownDescription: "A bearer token for the public HTTP API (`Authorization: Bearer pulse_…`), for tools that cannot use " +
			"NATS. Pulse cannot edit tokens, so every change replaces the token (the old one is revoked, a new secret is issued).\n\n" +
			"The secret is returned only at creation; it is kept in state (sensitive) and never re-read. Destroying the resource " +
			"revokes the token and removes its record. A token revoked outside Terraform is planned for re-creation. Import with " +
			"the token ID; the secret is then unknown.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Token ID."),
			"name": requiredImmutableString("Unique name: lowercase letters, digits, `_`, `.` and `-`, at most 64.",
				stringvalidator.RegexMatches(clientNameRE, "must start with a lowercase letter or digit, then up to 63 of a-z, 0-9, '_', '.', '-'")),
			"description": schema.StringAttribute{MarkdownDescription: "Description. Changing it forces a new token.", Optional: true,
				Computed: true, Default: stringdefault.StaticString(""), Validators: []validator.String{stringvalidator.LengthAtMost(512)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"grants": grants,
			"cidrs": schema.SetAttribute{MarkdownDescription: "Source networks allowed to use the token, in canonical CIDR form " +
				"(`192.0.2.0/24`, `2001:db8::/32`, a single address as `/32` or `/128`). Empty (the default) allows any address. " +
				"Behind Cloudflare the client's real address is checked. Changing it forces a new token.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: setdefault.StaticValue(setOf(nil)),
				Validators:    []validator.Set{setvalidator.SizeAtMost(32), setvalidator.ValueStringsAre(canonicalCIDR{})},
				PlanModifiers: replaceSet},
			"expires_in_days": schema.Int64Attribute{MarkdownDescription: "Lifetime in days (1-3650, default 365). Changing it " +
				"forces a new token.", Optional: true, Computed: true, Default: int64default.StaticInt64(365),
				Validators:    []validator.Int64{int64validator.Between(1, 3650)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"secret": schema.StringAttribute{MarkdownDescription: "The token (`pulse_…`). Only known after create; never re-read.",
				Computed: true, Sensitive: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"prefix":     computedString("First characters of the token, as the dashboard shows them."),
			"expires_at": computedString("When the token expires (RFC 3339)."),
			"status": schema.StringAttribute{MarkdownDescription: "`active` or `expired`.", Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (m *httpTokenModel) fill(t *client.HTTPToken) {
	m.ID = types.StringValue(t.ID)
	m.Name = types.StringValue(t.Name)
	m.Description = types.StringValue(t.Description)
	m.Grants = grantsSet(t.Grants)
	m.CIDRs = setOf(t.CIDRs)
	m.Prefix = types.StringValue(t.Prefix)
	m.ExpiresAt = types.StringValue(t.ExpiresAt.UTC().Format(time.RFC3339))
	m.Status = types.StringValue(t.Status)
	if m.Secret.IsUnknown() {
		m.Secret = types.StringNull()
	}
	if m.ExpiresInDays.IsNull() || m.ExpiresInDays.IsUnknown() {
		m.ExpiresInDays = types.Int64Value(365)
	}
}

func (r *httpTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan httpTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.HTTPTokenInput{Name: plan.Name.ValueString(), Description: plan.Description.ValueString(),
		Grants: grantsFrom(ctx, plan.Grants, &resp.Diagnostics), CIDRs: stringsOf(ctx, plan.CIDRs, &resp.Diagnostics),
		ExpiresInDays: plan.ExpiresInDays.ValueInt64()}
	if resp.Diagnostics.HasError() {
		return
	}
	t, secret, err := r.client.CreateHTTPToken(ctx, in)
	if err != nil && (client.HasStatus(err, http.StatusConflict) || client.HasStatus(err, http.StatusUnprocessableEntity)) {
		// A revoked or expired token keeps its name until its record is deleted: free it.
		if old := r.findByName(ctx, in.Name); old != nil && old.Status != "active" {
			if derr := r.client.DeleteHTTPToken(ctx, old.ID); derr == nil {
				resp.Diagnostics.AddWarning("Removed an inactive token with the same name",
					"HTTP token "+old.ID+" was "+old.Status+" and still held the name "+in.Name+"; its record was deleted.")
				t, secret, err = r.client.CreateHTTPToken(ctx, in)
			}
		}
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create HTTP token", err)
		return
	}
	plan.Secret = types.StringValue(secret)
	plan.fill(t)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *httpTokenResource) findByName(ctx context.Context, name string) *client.HTTPToken {
	all, err := r.client.ListHTTPTokens(ctx)
	if err != nil {
		return nil
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}

func (r *httpTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state httpTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.GetHTTPToken(ctx, state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && t.Status == "revoked") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read HTTP token", err)
		return
	}
	state.fill(t)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a change (every argument forces replacement); it only stores the plan.
func (r *httpTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan httpTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *httpTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state httpTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.client.RevokeHTTPToken(ctx, id); err != nil && !client.IsNotFound(err) && !client.HasStatus(err, http.StatusConflict) {
		apiError(&resp.Diagnostics, "Cannot revoke HTTP token", err)
		return
	}
	if err := r.client.DeleteHTTPToken(ctx, id); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "HTTP token revoked but its record could not be deleted", err)
	}
}

func (r *httpTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("expires_in_days"), types.Int64Value(365))...)
}
