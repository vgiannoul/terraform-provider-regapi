package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

func TestAQuoteShowsThePriceOfARegistrableName(t *testing.T) {
	_, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `data "regapi_domain_quote" "q" { domain_name = "free.gr" }`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "registrable", "true"),
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "price_cents", "2000"),
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "currency", "EUR"),
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "years", "2")),
		}},
	})
}

func TestAQuoteSaysWhyANameCannotBeRegistered(t *testing.T) {
	f, cfg := startFake(t)
	f.AddDomain("taken.gr", client.DomainContacts{})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `data "regapi_domain_quote" "q" { domain_name = "taken.gr" }`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "registrable", "false"),
				resource.TestCheckResourceAttr("data.regapi_domain_quote.q", "reason",
					"domain_unavailable"),
				resource.TestCheckNoResourceAttr("data.regapi_domain_quote.q", "price_cents")),
		}},
	})
}
