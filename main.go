// Command terraform-provider-pulse is the Terraform provider for Pulse, a personal durable event bus on NATS JetStream.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/provider"
)

// version is set by the release build (-ldflags "-X main.version=…").
var version = "dev"

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name pulse

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/aurimasniekis/pulse",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
