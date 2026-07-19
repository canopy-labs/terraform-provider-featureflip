package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/provider"
)

// version is set by GoReleaser via ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/canopy-labs/featureflip",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
