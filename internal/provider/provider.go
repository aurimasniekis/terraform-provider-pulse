// Package provider implements the Pulse Terraform provider with terraform-plugin-framework.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// Environment variables read by the provider (the same ones the pulse CLI uses).
const (
	EnvAdminURL  = "PULSE_ADMIN_URL"
	EnvToken     = "PULSE_ADMIN_TOKEN"      //nolint:gosec // an environment variable name, not a credential
	EnvTokenFile = "PULSE_ADMIN_TOKEN_FILE" //nolint:gosec // an environment variable name, not a credential
)

// DefaultAdminURL is the admin listener's default address (on the server, or the local end of an SSH tunnel).
const DefaultAdminURL = "http://127.0.0.1:8080"

var _ provider.Provider = (*pulseProvider)(nil)

type pulseProvider struct {
	version string
}

type providerModel struct {
	AdminURL  types.String `tfsdk:"admin_url"`
	Token     types.String `tfsdk:"token"`
	TokenFile types.String `tfsdk:"token_file"`
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &pulseProvider{version: version} }
}

func (p *pulseProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pulse"
	resp.Version = p.version
}

func (p *pulseProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a [Pulse](https://aurimasniekis.github.io/go-pulse/) server (a personal durable event bus on NATS " +
			"JetStream) through its admin API: clients and their grants, HTTP tokens, streams, consumers, KV and object buckets, " +
			"webhooks, alert rules, firewall entries, dashboard users and single sign-on.\n\n" +
			"Authenticate with a **personal access token** (`pulse_pat_…`) with the `administrator` role: create one on the " +
			"dashboard's Account page, or on the server with `pulsed admin token`. Pulse accepts these tokens only on its loopback " +
			"admin listener, so run Terraform on the server or through an SSH tunnel (`ssh -L 8080:127.0.0.1:8080 <server>`).",
		Attributes: map[string]schema.Attribute{
			"admin_url": schema.StringAttribute{
				MarkdownDescription: "Admin URL of the Pulse server. Default `" + DefaultAdminURL + "`. May also be set with the `" +
					EnvAdminURL + "` environment variable.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Personal access token (`pulse_pat_…`). May also be set with the `" + EnvToken +
					"` environment variable. Conflicts with `token_file`.",
				Optional:  true,
				Sensitive: true,
			},
			"token_file": schema.StringAttribute{
				MarkdownDescription: "Path to a file holding the personal access token. May also be set with `" + EnvTokenFile + "`.",
				Optional:            true,
			},
		},
	}
}

func (p *pulseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, v := range map[string]types.String{"admin_url": cfg.AdminURL, "token": cfg.Token, "token_file": cfg.TokenFile} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider configuration",
				"The provider cannot be configured with a value that is only known after apply. Set "+name+" statically or via the environment.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Token.ValueString() != "" && cfg.TokenFile.ValueString() != "" {
		resp.Diagnostics.AddAttributeError(path.Root("token_file"), "Conflicting provider configuration", "Set either token or token_file, not both.")
		return
	}
	adminURL := firstNonEmpty(cfg.AdminURL.ValueString(), os.Getenv(EnvAdminURL), DefaultAdminURL)
	// Precedence: token, token_file, PULSE_ADMIN_TOKEN, PULSE_ADMIN_TOKEN_FILE.
	token := cfg.Token.ValueString()
	file := cfg.TokenFile.ValueString()
	if token == "" && file == "" {
		token = os.Getenv(EnvToken)
		if token == "" {
			file = os.Getenv(EnvTokenFile)
		}
	}
	if token == "" && file != "" {
		t, err := client.ReadTokenFile(file)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("token_file"), "Cannot read the token file", err.Error())
			return
		}
		token = t
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing Pulse personal access token",
			"Set token or token_file, or the "+EnvToken+" or "+EnvTokenFile+" environment variable, to a personal access token "+
				"(pulse_pat_…) with the administrator role.")
		return
	}
	if err := client.ValidateToken(token); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Invalid Pulse personal access token", err.Error())
		return
	}
	c, err := client.New(client.Config{
		Endpoint:  adminURL,
		Token:     token,
		UserAgent: "terraform-provider-pulse/" + p.version,
		Observer: func(ctx context.Context, method, apiPath string, status, attempt int, err error) {
			fields := map[string]any{"method": method, "path": apiPath, "status": status, "attempt": attempt}
			if err != nil {
				fields["error"] = err.Error()
			}
			tflog.Debug(ctx, "pulse API request", fields)
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure the Pulse client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *pulseProvider) Resources(context.Context) []func() resource.Resource {
	return resources
}

func (p *pulseProvider) DataSources(context.Context) []func() datasource.DataSource {
	return dataSources
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
