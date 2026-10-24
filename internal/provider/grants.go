package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// grantModel is one element of a `grants` set.
type grantModel struct {
	Action  types.String `tfsdk:"action"`
	Subject types.String `tfsdk:"subject"`
	Stream  types.String `tfsdk:"stream"`
	Name    types.String `tfsdk:"name"`
}

var grantAttrTypes = map[string]attr.Type{
	"action": types.StringType, "subject": types.StringType, "stream": types.StringType, "name": types.StringType,
}

var grantObjectType = types.ObjectType{AttrTypes: grantAttrTypes}

// grantsAttribute is the `grants` set of a client (all actions) or an HTTP token (publish, subscribe, request).
func grantsAttribute(desc string, actions []string) schema.SetNestedAttribute {
	return schema.SetNestedAttribute{
		MarkdownDescription: desc,
		Optional:            true,
		Computed:            true,
		Default:             setdefault.StaticValue(types.SetValueMust(grantObjectType, nil)),
		Validators:          []validator.Set{setvalidator.SizeAtMost(64)},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"action": schema.StringAttribute{MarkdownDescription: "One of " + quoteList(actions) + ".", Required: true,
				Validators: []validator.String{stringvalidator.OneOf(actions...)}},
			"subject": schema.StringAttribute{MarkdownDescription: "Subject or wildcard pattern (`*` one token, `>` the rest), " +
				"for example `evt.home.>`. Required for every action except `blobs`.", Optional: true},
			"stream": schema.StringAttribute{MarkdownDescription: "Stream for `subscribe` and `durable` grants. Default `EVENTS`.",
				Optional: true},
			"name": schema.StringAttribute{MarkdownDescription: "Durable consumer name for `durable` grants: the client may use the " +
				"consumer `<client id>_<name>` (see `pulse_consumer`).", Optional: true},
		}},
	}
}

func quoteList(ss []string) string {
	out := ""
	for i, s := range ss {
		switch {
		case i == 0:
		case i == len(ss)-1:
			out += " or "
		default:
			out += ", "
		}
		out += "`" + s + "`"
	}
	return out
}

// grantsFrom converts the `grants` set to API grants (sorted, so the request is deterministic).
func grantsFrom(ctx context.Context, set types.Set, diags *diag.Diagnostics) []client.Grant {
	out := []client.Grant{}
	if set.IsNull() || set.IsUnknown() {
		return out
	}
	var ms []grantModel
	diags.Append(set.ElementsAs(ctx, &ms, false)...)
	for _, m := range ms {
		out = append(out, client.Grant{Action: m.Action.ValueString(), Subject: m.Subject.ValueString(),
			Stream: m.Stream.ValueString(), Name: m.Name.ValueString()})
	}
	sort.Slice(out, func(i, j int) bool { return grantKey(out[i]) < grantKey(out[j]) })
	return out
}

func grantKey(g client.Grant) string {
	return g.Action + "\x00" + g.Subject + "\x00" + g.Stream + "\x00" + g.Name
}

// grantsSet converts API grants to the `grants` set ("" fields become null).
func grantsSet(gs []client.Grant) types.Set {
	vals := make([]attr.Value, 0, len(gs))
	for _, g := range gs {
		vals = append(vals, types.ObjectValueMust(grantAttrTypes, map[string]attr.Value{
			"action": types.StringValue(g.Action), "subject": optString(g.Subject), "stream": optString(g.Stream), "name": optString(g.Name),
		}))
	}
	return types.SetValueMust(grantObjectType, vals)
}
