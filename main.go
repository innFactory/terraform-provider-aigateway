package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/innFactory/terraform-provider-aigateway/internal/provider"
)

// version is set by GoReleaser via -ldflags at build time and surfaced in the
// User-Agent of every request so the gateway audit log can attribute changes.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Run as a debug server (for terraform-plugin-debugging)")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/innFactory/aigateway",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
