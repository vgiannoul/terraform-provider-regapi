// Command terraform-provider-regapi is the Terraform and OpenTofu provider for RegAPI.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/vgiannoul/terraform-provider-regapi/internal/provider"
)

// version is set by GoReleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version),
		providerserver.ServeOpts{Address: "registry.terraform.io/vgiannoul/regapi", Debug: debug})
	if err != nil {
		log.Fatal(err)
	}
}
