// Package provider is the Terraform provider for RegAPI.
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

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

type regapiProvider struct{ version string }

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

// New returns a constructor for the provider at version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &regapiProvider{version: version} }
}

func (p *regapiProvider) Metadata(
	_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse,
) {
	resp.TypeName = "regapi"
	resp.Version = p.version
}

func (p *regapiProvider) Schema(
	_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manage RegAPI contacts, default contacts, domains and registrations.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "The RegAPI base URL. Defaults to " + client.DefaultEndpoint + ".",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "A RegAPI API key of the account. Defaults to the REGAPI_API_KEY " +
					"environment variable.",
			},
		},
	}
}

func (p *regapiProvider) Configure(
	ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse,
) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.APIKey.IsUnknown() || cfg.Endpoint.IsUnknown() {
		resp.Diagnostics.AddError("Provider configuration is not known yet",
			"endpoint and api_key must be known when the provider is configured.")
		return
	}
	endpoint := client.DefaultEndpoint
	if !cfg.Endpoint.IsNull() && cfg.Endpoint.ValueString() != "" {
		endpoint = cfg.Endpoint.ValueString()
	}
	apiKey := cfg.APIKey.ValueString()
	if cfg.APIKey.IsNull() {
		apiKey = os.Getenv("REGAPI_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing API key",
			"Set api_key in the provider block or the REGAPI_API_KEY environment variable.")
		return
	}
	c := client.New(endpoint, apiKey, p.version)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *regapiProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewContactResource}
}

func (p *regapiProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
