package provider

import (
	"context"
	"encoding/base64"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() { registerResource(NewKVEntryResource) }

// kvKeyRE is the NATS KV key alphabet.
var kvKeyRE = regexp.MustCompile(`^[-/_=.a-zA-Z0-9]+$`)

var _ resource.ResourceWithImportState = (*kvEntryResource)(nil)

type kvEntryResource struct{ baseResource }

// NewKVEntryResource returns the pulse_kv_entry resource.
func NewKVEntryResource() resource.Resource { return &kvEntryResource{} }

type kvEntryModel struct {
	ID             types.String `tfsdk:"id"`
	Bucket         types.String `tfsdk:"bucket"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	Encoding       types.String `tfsdk:"encoding"`
	PurgeOnDestroy types.Bool   `tfsdk:"purge_on_destroy"`
	Revision       types.Int64  `tfsdk:"revision"`
}

func (r *kvEntryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kv_entry"
}

func (r *kvEntryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One key in a KV bucket, for configuration that tools read from Pulse. A value changed outside " +
			"Terraform (by a tool or the dashboard) is put back on the next apply. Destroying the resource deletes the key. " +
			"Import with `<bucket>/<key>`.",
		Attributes: map[string]schema.Attribute{
			"id":     idAttribute("`<bucket>/<key>`."),
			"bucket": requiredImmutableString("KV bucket name."),
			"key": requiredImmutableString("Key: letters, digits and `-/_=.` (dots separate levels, as in NATS subjects).",
				stringvalidator.RegexMatches(kvKeyRE, "may contain only letters, digits and -/_=."),
				stringvalidator.LengthBetween(1, 1024)),
			"value": schema.StringAttribute{MarkdownDescription: "The value: text, or base64 when `encoding = \"base64\"` (use " +
				"`base64encode()` or `filebase64()`).", Required: true},
			"encoding": schema.StringAttribute{MarkdownDescription: "`utf8` (default) or `base64`.", Optional: true, Computed: true,
				Default: stringdefault.StaticString("utf8"), Validators: []validator.String{stringvalidator.OneOf("utf8", "base64")}},
			"purge_on_destroy": schema.BoolAttribute{MarkdownDescription: "On destroy, also drop the key's history (purge) " +
				"instead of only writing a delete marker. Default `false`.", Optional: true, Computed: true,
				Default: booldefault.StaticBool(false)},
			"revision": schema.Int64Attribute{MarkdownDescription: "The key's revision after the last write.", Computed: true,
				PlanModifiers: []planmodifier.Int64{keepUnlessChanged{"value", "encoding"}}},
		},
	}
}

// fill records the server's value in the configured encoding (the server reports text as utf8 and anything else as
// base64).
func (m *kvEntryModel) fill(bucket string, e *client.KVEntry) {
	m.ID = types.StringValue(bucket + "/" + e.Key)
	m.Bucket, m.Key = types.StringValue(bucket), types.StringValue(e.Key)
	m.Revision = types.Int64Value(int64(e.Revision)) //nolint:gosec // revisions fit in int64
	raw := []byte(e.Value)
	if e.Encoding == "base64" {
		b, err := base64.StdEncoding.DecodeString(e.Value)
		if err != nil {
			return
		}
		raw = b
	}
	if m.Encoding.ValueString() == "base64" {
		m.Value = types.StringValue(base64.StdEncoding.EncodeToString(raw))
	} else {
		if m.Encoding.IsNull() || m.Encoding.IsUnknown() {
			m.Encoding = types.StringValue("utf8")
		}
		m.Value = types.StringValue(string(raw))
	}
}

func (r *kvEntryResource) write(ctx context.Context, m *kvEntryModel) error {
	rev, err := r.client.PutKVEntry(ctx, m.Bucket.ValueString(), m.Key.ValueString(), m.Value.ValueString(), m.Encoding.ValueString())
	if err != nil {
		return err
	}
	m.ID = types.StringValue(m.Bucket.ValueString() + "/" + m.Key.ValueString())
	m.Revision = types.Int64Value(int64(rev)) //nolint:gosec // revisions fit in int64
	return nil
}

func (r *kvEntryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kvEntryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "Cannot write KV entry", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *kvEntryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kvEntryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.GetKVEntry(ctx, state.Bucket.ValueString(), state.Key.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read KV entry", err)
		return
	}
	state.fill(state.Bucket.ValueString(), e)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *kvEntryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state kvEntryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Value.Equal(state.Value) && plan.Encoding.Equal(state.Encoding) {
		plan.Revision = state.Revision // only purge_on_destroy changed
	} else if err := r.write(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "Cannot write KV entry", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *kvEntryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state kvEntryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteKVEntry(ctx, state.Bucket.ValueString(), state.Key.ValueString(), state.PurgeOnDestroy.ValueBool())
	if err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete KV entry", err)
	}
}

func (r *kvEntryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2, "<bucket>/<key>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("bucket"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("encoding"), "utf8")...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("purge_on_destroy"), false)...)
}
