package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

var (
	_ datasource.DataSource              = (*domainDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainDataSource)(nil)
)

// NewDomainDataSource returns the regapi_domain data source.
func NewDomainDataSource() datasource.DataSource { return &domainDataSource{} }

type domainDataSource struct{ client *client.Client }

func (d *domainDataSource) Metadata(
	_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(
	_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse,
) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Reads a domain registered in the account.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true, Description: "The domain name."},
			"nameservers": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				Description: "Nameservers."},
			"auto_renew":            schema.BoolAttribute{Computed: true, Description: "Auto-renew."},
			"registrant_contact_id": computed("Contact ID of the registrant."),
			"admin_contact_id":      computed("Contact ID of the administrative contact."),
			"tech_contact_id":       computed("Contact ID of the technical contact."),
			"billing_contact_id":    computed("Contact ID of the billing contact."),
			"phase":                 computed("Lifecycle phase."),
			"expiry_date":           computed("Expiry date and time."),
			"nameservers_source":    computed("default or custom."),
		},
	}
}

func (d *domainDataSource) Configure(
	_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		d.client = req.ProviderData.(*client.Client)
	}
}

func (d *domainDataSource) Read(
	ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse,
) {
	var config domainDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetDomain(ctx, config.Name.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	m := domainFromAPI(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, domainDataModel{Name: m.Name,
		Nameservers: m.Nameservers, AutoRenew: m.AutoRenew,
		RegistrantContactID: m.RegistrantContactID, AdminContactID: m.AdminContactID,
		TechContactID: m.TechContactID, BillingContactID: m.BillingContactID, Phase: m.Phase,
		ExpiryDate: m.ExpiryDate, NameserversSource: m.NameserversSource})...)
}
