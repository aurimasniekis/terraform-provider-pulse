package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func init() {
	registerResource(NewKVBucketResource)
	registerResource(NewObjectBucketResource)
}

// Shared bucket attributes.

func bucketNameAttribute() schema.StringAttribute {
	return requiredImmutableString("Bucket name: letters, digits, `_` and `-`, at most 64.",
		stringvalidator.RegexMatches(streamNameRE, "must be 1-64 letters, digits, '_' or '-'"))
}

func bucketDescriptionAttribute() schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: "Description.", Optional: true, Computed: true,
		Default: stringdefault.StaticString(""), Validators: []validator.String{stringvalidator.LengthAtMost(256)}}
}

func bucketTTLAttribute(what string) schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: "How long " + what + " are kept, in seconds; `0` (the default) keeps them " +
		"forever.", Optional: true, Computed: true, Default: int64default.StaticInt64(0),
		Validators: []validator.Int64{int64validator.AtLeast(0)}}
}

func bucketMaxBytesAttribute() schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: "Size limit in bytes; `-1` (the default) is unlimited.", Optional: true,
		Computed: true, Default: int64default.StaticInt64(-1), Validators: []validator.Int64{int64validator.AtLeast(-1),
			int64validator.NoneOf(0)}}
}

// ---- pulse_kv_bucket ----

var _ resource.ResourceWithImportState = (*kvBucketResource)(nil)

type kvBucketResource struct{ baseResource }

// NewKVBucketResource returns the pulse_kv_bucket resource.
func NewKVBucketResource() resource.Resource { return &kvBucketResource{} }

type kvBucketModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	History     types.Int64  `tfsdk:"history"`
	TTLSeconds  types.Int64  `tfsdk:"ttl_seconds"`
	MaxBytes    types.Int64  `tfsdk:"max_bytes"`
}

func (r *kvBucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kv_bucket"
}

func (r *kvBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A JetStream key-value bucket, for shared state such as tool configuration or the latest value of " +
			"something. Clients read and write it with the NATS KV API (their grants must allow its `$KV.<bucket>.>` subjects). " +
			"Deleting the resource deletes the bucket and every key. Import with the bucket name.",
		Attributes: map[string]schema.Attribute{
			"id":          idAttribute("Bucket name."),
			"name":        bucketNameAttribute(),
			"description": bucketDescriptionAttribute(),
			"history": schema.Int64Attribute{MarkdownDescription: "Revisions kept per key (1-64, default 1).", Optional: true,
				Computed: true, Default: int64default.StaticInt64(1), Validators: []validator.Int64{int64validator.Between(1, 64)}},
			"ttl_seconds": bucketTTLAttribute("values"),
			"max_bytes":   bucketMaxBytesAttribute(),
		},
	}
}

func (m *kvBucketModel) input() client.KVBucketInput {
	return client.KVBucketInput{Name: m.Name.ValueString(), Description: m.Description.ValueString(), History: m.History.ValueInt64(),
		TTLSeconds: m.TTLSeconds.ValueInt64(), MaxBytes: m.MaxBytes.ValueInt64()}
}

func (m *kvBucketModel) fill(b *client.KVBucket) {
	m.ID, m.Name = types.StringValue(b.Name), types.StringValue(b.Name)
	m.Description = types.StringValue(b.Description)
	m.History = types.Int64Value(b.History)
	m.TTLSeconds = types.Int64Value(b.TTLSeconds)
	m.MaxBytes = types.Int64Value(b.MaxBytes)
}

func (r *kvBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kvBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.CreateKVBucket(ctx, plan.input()); err != nil {
		apiError(&resp.Diagnostics, "Cannot create KV bucket", err)
		return
	}
	// The create response does not carry every setting; read the bucket back.
	b, err := r.client.GetKVBucket(ctx, plan.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read KV bucket", err)
		return
	}
	plan.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *kvBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kvBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetKVBucket(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read KV bucket", err)
		return
	}
	state.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *kvBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan kvBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.UpdateKVBucket(ctx, plan.Name.ValueString(), plan.input())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot update KV bucket", err)
		return
	}
	plan.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *kvBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state kvBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteKVBucket(ctx, state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete KV bucket", err)
	}
}

func (r *kvBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// ---- pulse_object_bucket ----

var _ resource.ResourceWithImportState = (*objectBucketResource)(nil)

type objectBucketResource struct{ baseResource }

// NewObjectBucketResource returns the pulse_object_bucket resource.
func NewObjectBucketResource() resource.Resource { return &objectBucketResource{} }

type objectBucketModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	TTLSeconds  types.Int64  `tfsdk:"ttl_seconds"`
	MaxBytes    types.Int64  `tfsdk:"max_bytes"`
}

func (r *objectBucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_bucket"
}

func (r *objectBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A JetStream object store bucket, for files and large payloads. (Event payloads above 1 MiB go to " +
			"Pulse's own `blobs` bucket, which is managed by pulsed and cannot be managed here.) Deleting the resource deletes the " +
			"bucket and every object. Import with the bucket name.",
		Attributes: map[string]schema.Attribute{
			"id":          idAttribute("Bucket name."),
			"name":        bucketNameAttribute(),
			"description": bucketDescriptionAttribute(),
			"ttl_seconds": bucketTTLAttribute("objects"),
			"max_bytes":   bucketMaxBytesAttribute(),
		},
	}
}

func (m *objectBucketModel) input() client.ObjectBucketInput {
	return client.ObjectBucketInput{Name: m.Name.ValueString(), Description: m.Description.ValueString(),
		TTLSeconds: m.TTLSeconds.ValueInt64(), MaxBytes: m.MaxBytes.ValueInt64()}
}

func (m *objectBucketModel) fill(b *client.ObjectBucket) {
	m.ID, m.Name = types.StringValue(b.Name), types.StringValue(b.Name)
	m.Description = types.StringValue(b.Description)
	m.TTLSeconds = types.Int64Value(b.TTLSeconds)
	m.MaxBytes = types.Int64Value(b.MaxBytes)
}

func (r *objectBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.CreateObjectBucket(ctx, plan.input()); err != nil {
		apiError(&resp.Diagnostics, "Cannot create object bucket", err)
		return
	}
	b, err := r.client.GetObjectBucket(ctx, plan.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read object bucket", err)
		return
	}
	plan.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetObjectBucket(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot read object bucket", err)
		return
	}
	state.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *objectBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan objectBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.UpdateObjectBucket(ctx, plan.Name.ValueString(), plan.input())
	if err != nil {
		apiError(&resp.Diagnostics, "Cannot update object bucket", err)
		return
	}
	plan.fill(b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteObjectBucket(ctx, state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Cannot delete object bucket", err)
	}
}

func (r *objectBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
