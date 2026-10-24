package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()
	var ps fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &ps)
	if ps.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", ps.Diagnostics)
	}
	if d := ps.Schema.ValidateImplementation(ctx); d.HasError() {
		t.Fatalf("provider schema: %v", d)
	}
	seen := map[string]bool{}
	for _, f := range p.Resources(ctx) {
		r := f()
		var md fwresource.MetadataResponse
		r.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "pulse"}, &md)
		if seen[md.TypeName] {
			t.Fatalf("duplicate resource %s", md.TypeName)
		}
		seen[md.TypeName] = true
		var sr fwresource.SchemaResponse
		r.Schema(ctx, fwresource.SchemaRequest{}, &sr)
		if sr.Diagnostics.HasError() {
			t.Fatalf("%s: %v", md.TypeName, sr.Diagnostics)
		}
		if d := sr.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Fatalf("%s: %v", md.TypeName, d)
		}
		if _, ok := r.(fwresource.ResourceWithImportState); !ok {
			t.Errorf("%s does not support import", md.TypeName)
		}
		if sr.Schema.MarkdownDescription == "" {
			t.Errorf("%s has no description", md.TypeName)
		}
		for name, a := range sr.Schema.Attributes {
			if a.GetMarkdownDescription() == "" {
				t.Errorf("%s.%s has no description", md.TypeName, name)
			}
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), "examples", "resources", md.TypeName, "resource.tf")); err != nil {
			t.Errorf("%s has no example: %v", md.TypeName, err)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var md datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "pulse"}, &md)
		var sr datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &sr)
		if diags := sr.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("%s: %v", md.TypeName, diags)
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), "examples", "data-sources", md.TypeName, "data-source.tf")); err != nil {
			t.Errorf("%s has no example: %v", md.TypeName, err)
		}
	}
}

func TestSplitID(t *testing.T) {
	if p, err := splitID("abc/def", 2, "<a>/<b>"); err != nil || p[0] != "abc" || p[1] != "def" {
		t.Fatalf("got %v %v", p, err)
	}
	for _, bad := range []string{"", "x", "a/", "/b", "a/b/c"} {
		if _, err := splitID(bad, 2, "<a>/<b>"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func errRE(parts ...string) *regexp.Regexp {
	q := make([]string, len(parts))
	for i, p := range parts {
		q[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile(`(?s)` + strings.Join(q, ".*"))
}

func TestProviderConfigValidation(t *testing.T) {
	t.Setenv(EnvAdminURL, "")
	t.Setenv(EnvToken, "")
	t.Setenv(EnvTokenFile, "")
	tokenFile := filepath.Join(t.TempDir(), "admin.token")
	if err := os.WriteFile(tokenFile, []byte("not-a-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const ds = `
data "pulse_system_info" "x" {}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `provider "pulse" {}` + ds,
				ExpectError: errRE("Missing Pulse personal access token"),
			},
			{
				Config:      `provider "pulse" { token = "porch_at_abc" }` + ds,
				ExpectError: errRE("Invalid Pulse personal access token", `starting with "pulse_pat_"`),
			},
			{
				Config:      "provider \"pulse\" {\n  token      = \"pulse_pat_abc_def\"\n  token_file = \"/x\"\n}\n" + ds,
				ExpectError: errRE("Set either token or token_file"),
			},
			{
				Config:      `provider "pulse" { token_file = "/nonexistent/admin.token" }` + ds,
				ExpectError: errRE("Cannot read the token file"),
			},
			{
				Config:      `provider "pulse" { token_file = "` + tokenFile + `" }` + ds,
				ExpectError: errRE("Invalid Pulse personal access token"),
			},
			{
				Config:      "provider \"pulse\" {\n  admin_url = \"127.0.0.1:8080\"\n  token     = \"pulse_pat_abc_def\"\n}\n" + ds,
				ExpectError: errRE("Cannot configure the Pulse client", "admin_url must be an absolute http(s) URL"),
			},
		},
	})
}
