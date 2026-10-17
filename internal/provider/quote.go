package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

var (
	_ datasource.DataSource              = (*quoteDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*quoteDataSource)(nil)
)

// NewQuoteDataSource returns the regapi_domain_quote data source.
func NewQuoteDataSource() datasource.DataSource { return &quoteDataSource{} }

type quoteDataSource struct{ client *client.Client }

type quoteModel struct {
	DomainName  types.String `tfsdk:"domain_name"`
	Registrable types.Bool   `tfsdk:"registrable"`
	Reason      types.String `tfsdk:"reason"`
	PriceCents  types.Int64  `tfsdk:"price_cents"`
	Currency    types.String `tfsdk:"currency"`
	Years       types.Int64  `tfsdk:"years"`
}

func (d *quoteDataSource) Metadata(
	_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_domain_quote"
}

func (d *quoteDataSource) Schema(
	_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Whether a name can be registered now and what it costs for the default " +
			"term, asked of the registry live. Use price_cents as expected_price_cents.",
		Attributes: map[string]schema.Attribute{
			"domain_name": schema.StringAttribute{Required: true, Description: "The name to check."},
			"registrable": schema.BoolAttribute{Computed: true,
				Description: "Whether the name can be registered."},
			"reason": schema.StringAttribute{Computed: true,
				Description: "Why not, for example domain_unavailable."},
			"price_cents": schema.Int64Attribute{Computed: true,
				Description: "Price in cents for the default term."},
			"currency": schema.StringAttribute{Computed: true, Description: "Price currency."},
			"years":    schema.Int64Attribute{Computed: true, Description: "The default term."},
		},
	}
}

func (d *quoteDataSource) Configure(
	_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		d.client = req.ProviderData.(*client.Client)
	}
}

func (d *quoteDataSource) Read(
	ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse,
) {
	var model quoteModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := model.DomainName.ValueString()
	results, err := d.client.CheckDomains(ctx, []string{name})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not check the domain", err)
		return
	}
	for _, res := range results {
		if !strings.EqualFold(res.DomainName, name) {
			continue
		}
		model.Registrable = types.BoolValue(res.Registrable)
		model.Reason = optString(res.Reason)
		model.PriceCents, model.Currency, model.Years = types.Int64Null(), types.StringNull(),
			types.Int64Null()
		if res.Price != nil {
			model.PriceCents = types.Int64Value(res.Price.AmountCents)
			model.Currency = types.StringValue(res.Price.Currency)
			model.Years = types.Int64Value(int64(res.Price.Years))
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
		return
	}
	resp.Diagnostics.AddError("No answer for the domain", "The API did not answer for "+name+".")
}
