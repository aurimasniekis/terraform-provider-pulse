package provider

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewClientResource) }

// clientNameRE is Pulse's name pattern for clients and HTTP tokens.
var clientNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

var clientActions = []string{"publish", "subscribe", "request", "reply", "durable", "blobs"}

var _ resource.ResourceWithImportState = (*clientResource)(nil)

type clientResource struct{ baseResource }

// NewClientResource returns the pulse_client resource.
func NewClientResource() resource.Resource { return &clientResource{} }

type clientModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Tags          types.Set    `tfsdk:"tags"`
	Grants        types.Set    `tfsdk:"grants"`
	ExpiresInDays types.Int64  `tfsdk:"expires_in_days"`
	CredsVersion  types.String `tfsdk:"creds_version"`
	Creds         types.String `tfsdk:"creds"`
	UserKey       types.String `tfsdk:"user_key"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	Status        types.String `tfsdk:"status"`
}

func (r *clientResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_client"
}

func (r *clientResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	rotated := secretPlanModifier{versionAttr: "creds_version", also: []string{"expires_in_days"}}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A client: one tool's own NATS identity, with permissions built from its grants. A client without " +
			"grants can connect but do nothing.\n\n" +
			"The `.creds` file is returned only when the client is created or its credentials are rotated. It is kept in state " +
			"(sensitive) and never re-read, so protect the state. Change `creds_version` to rotate the credentials: the old ones " +
			"stop working and their connections are closed.\n\n" +
			"Grant changes apply to live connections. Destroying the resource revokes the client permanently and removes its " +
			"record. A client revoked outside Terraform is removed from state and planned for re-creation (under the same name). " +
			"Import with the client ID or name; the credentials are then unknown until rotated.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Client ID."),
			"name": schema.StringAttribute{MarkdownDescription: "Unique name: lowercase letters, digits, `_`, `.` and `-`, at most 64.",
				Required: true, Validators: []validator.String{stringvalidator.RegexMatches(clientNameRE,
					"must start with a lowercase letter or digit, then up to 63 of a-z, 0-9, '_', '.', '-'")}},
			"description": schema.StringAttribute{MarkdownDescription: "Description.", Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), Validators: []validator.String{stringvalidator.LengthAtMost(512)}},
			"tags": schema.SetAttribute{MarkdownDescription: "Free-form tags shown in the dashboard.", ElementType: types.StringType,
				Optional: true, Computed: true, Default: setdefault.StaticValue(setOf(nil)),
				Validators: []validator.Set{setvalidator.SizeAtMost(16)}},
			"grants": grantsAttribute("What the client may do. `publish`, `request` and `reply` take a `subject`; `subscribe` "+
				"takes a `subject` and optionally a `stream`; `durable` takes a `subject`, a `name` and optionally a `stream`; "+
				"`blobs` takes nothing and allows the shared blob store.", clientActions),
			"expires_in_days": schema.Int64Attribute{MarkdownDescription: "Lifetime of the credentials in days (1-3650, default 365). " +
				"Changing it rotates the credentials, which then expire this many days after the rotation.", Optional: true,
				Computed: true, Default: int64default.StaticInt64(365), Validators: []validator.Int64{int64validator.Between(1, 3650)}},
			"creds_version": schema.StringAttribute{MarkdownDescription: "Arbitrary value; changing it (including setting it for " +
				"the first time, e.g. after an import) rotates the credentials and stores the new `.creds`.", Optional: true},
			"creds": schema.StringAttribute{MarkdownDescription: "The `.creds` file (JWT and seed). Only known after create or " +
				"rotation; never re-read.", Computed: true, Sensitive: true, PlanModifiers: []planmodifier.String{rotated}},
			"user_key": schema.StringAttribute{MarkdownDescription: "The client's current NATS user public key (changes on rotation).",
				Computed: true, PlanModifiers: []planmodifier.String{rotated}},
			"expires_at": schema.StringAttribute{MarkdownDescription: "When the current credentials expire (RFC 3339).",
				Computed: true, PlanModifiers: []planmodifier.String{rotated}},
			"status": schema.StringAttribute{MarkdownDescription: "`active`, or `expired` when the credentials have expired " +
				"(rotate them).", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (m *clientModel) fill(c *client.NATSClient) {
	m.ID = types.StringValue(c.ID)
	m.Name = types.StringValue(c.Name)
	m.Description = types.StringValue(c.Description)
	m.Tags = setOf(c.Tags)
	m.Grants = grantsSet(c.Grants)
	m.UserKey = types.StringValue(c.UserKey)
	m.ExpiresAt = types.StringValue(c.ExpiresAt.UTC().Format(time.RFC3339))
	status := c.Status
	if status == "active" && !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt) {
		status = "expired"
	}
	m.Status = types.StringValue(status)
	if m.Creds.IsUnknown() {
		m.Creds = types.StringNull()
	}
	if m.ExpiresInDays.IsNull() || m.ExpiresInDays.IsUnknown() {
		m.ExpiresInDays = types.Int64Value(365)
	}
}

func (r *clientResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan clientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.ClientInput{Name: plan.Name.ValueString(), Description: plan.Description.ValueString(),
		Tags: stringsOf(ctx, plan.Tags, &resp.Diagnostics), Grants: grantsFrom(ctx, plan.Grants, &resp.Diagnostics),
		ExpiresInDays: plan.ExpiresInDays.ValueInt64()}
	if resp.Diagnostics.HasError() {
		return
	}
	issued, err := r.client.CreateClient(ctx, in)
	if client.HasStatus(err, http.StatusConflict) || client.HasStatus(err, http.StatusUnprocessableEntity) {
		// A revoked client keeps its name until its record is deleted (e.g. revoked outside Terraform): free it.
		if old, ferr := r.client.FindClient(ctx, in.Name); ferr == nil && old.Status == "revoked" {
			if derr := r.client.DeleteClient(ctx, old.ID); derr == nil {
				resp.Diagnostics.AddWarning("Removed a revoked client with the same name",
					"Client "+old.ID+" was revoked and still held the name "+in.Name+"; its record was deleted (the revocation stays).")
				issued, err = r.client.CreateClient(ctx, in)
			}
		}
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot create client", err)
		return
	}
	plan.Creds = types.StringValue(issued.Creds)
	plan.fill(&issued.Client)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *clientResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state clientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetClient(ctx, state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && c.Status == "revoked") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read client", err)
		return
	}
	state.fill(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *clientResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state clientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	plan.Creds = state.Creds
	current := func() *client.NATSClient {
		c, err := r.client.GetClient(ctx, id)
		if err != nil {
			apiError(&resp.Diagnostics, "Cannot read client", err)
			return nil
		}
		return c
	}
	c := current()
	if c == nil {
		return
	}
	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) || !plan.Tags.Equal(state.Tags) {
		tags := stringsOf(ctx, plan.Tags, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		if c, err = r.client.UpdateClient(ctx, id, c.Version, plan.Name.ValueString(), plan.Description.ValueString(), tags); err != nil {
			apiError(&resp.Diagnostics, "Cannot update client", err)
			return
		}
	}
	if !plan.Grants.Equal(state.Grants) {
		grants := grantsFrom(ctx, plan.Grants, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		if c, err = r.client.UpdateClientGrants(ctx, id, c.Version, grants); err != nil {
			apiError(&resp.Diagnostics, "Cannot update the client's grants", err)
			return
		}
	}
	if !plan.CredsVersion.Equal(state.CredsVersion) || !plan.ExpiresInDays.Equal(state.ExpiresInDays) {
		issued, err := r.client.RotateClient(ctx, id, plan.ExpiresInDays.ValueInt64())
		if err != nil {
			plan.CredsVersion, plan.ExpiresInDays = state.CredsVersion, state.ExpiresInDays
			plan.fill(c)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			apiError(&resp.Diagnostics, "Cannot rotate the client's credentials", err)
			return
		}
		plan.Creds = types.StringValue(issued.Creds)
		c = &issued.Client
	}
	plan.fill(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *clientResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state clientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.client.RevokeClient(ctx, id); err != nil && !client.IsNotFound(err) && !client.HasStatus(err, http.StatusConflict) {
		apiError(&resp.Diagnostics, "Cannot revoke client", err)
		return
	}
	if err := r.client.DeleteClient(ctx, id); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Client revoked but its record could not be deleted", err)
	}
}

func (r *clientResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	c, err := r.client.GetClient(ctx, req.ID)
	if client.IsNotFound(err) || client.HasStatus(err, http.StatusUnprocessableEntity) {
		c, err = r.client.FindClient(ctx, req.ID)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot import client "+req.ID, err)
		return
	}
	m := clientModel{Creds: types.StringNull(), CredsVersion: types.StringNull(), ExpiresInDays: types.Int64Value(365)}
	m.fill(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
