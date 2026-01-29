package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/skpr/terraform-provider-opensearch/internal/provider"
)

var (
	// set by goreleaser.
	version string = "dev"
)

const (
	providerAddress = "registry.terraform.io/skpr/opensearch"
)

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: providerAddress,
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.NewOpenSearchProvider(version), opts)

	if err != nil {
		log.Fatal(err.Error())
	}
}
