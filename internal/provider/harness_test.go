package provider

import (
	"context"
	"testing"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// TestAccHarness checks that the harness's server answers with both tokens and that the viewer token is read-only.
func TestAccHarness(t *testing.T) {
	testAccPreCheck(t)
	info, err := adminClient(t).GetSystemInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Ready || info.Profile != "dev" || info.Connect.CACert == "" {
		t.Fatalf("system info %+v", info)
	}
	viewer, err := client.New(client.Config{Endpoint: acc.AdminURL, Token: acc.ViewerToken})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.GetSystemInfo(context.Background()); err != nil {
		t.Fatalf("viewer: %v", err)
	}
}
