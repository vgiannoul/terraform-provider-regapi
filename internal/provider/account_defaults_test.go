package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

const agencyContact = `
resource "regapi_contact" "agency" {
  first_name = "Agency"
  last_name = "Ops"
  email = "ops@x.gr"
  country_code = "GR"
  street = "Odos"
  number = "2"
  city = "Athens"
  postal_code = "10431"
  state = "Attica"
}
`

// The registrant default cannot be cleared through the API, so the registrant contact is
// created outside Terraform: managing it would make every destroy fail with contact_in_use.
var ownerContact = client.Contact{FirstName: "Ana", LastName: "Test", Email: "a@x.gr",
	CountryCode: "GR", Street: "Odos", Number: "1", City: "Athens", PostalCode: "10431",
	State: "Attica"}

func defaultsConfig(owner, extra string) string {
	return agencyContact + `
resource "regapi_account_defaults" "d" {
  registrant_contact_id = "` + owner + `"
` + extra + `}
`
}

func TestAccountDefaultsLifecycle(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	all := `  admin_contact_id   = regapi_contact.agency.id
  tech_contact_id    = regapi_contact.agency.id
  billing_contact_id = regapi_contact.agency.id
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg + defaultsConfig(owner, all),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("regapi_account_defaults.d",
						"admin_contact_id", "regapi_contact.agency", "id"),
					resource.TestCheckResourceAttr("regapi_account_defaults.d",
						"registrant_contact_id", owner)),
			},
			{ResourceName: "regapi_account_defaults.d", ImportState: true,
				ImportStateVerify: true},
			{
				Config: cfg + defaultsConfig(owner, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("regapi_account_defaults.d", "admin_contact_id"),
					resource.TestCheckNoResourceAttr("regapi_account_defaults.d", "tech_contact_id")),
			},
		},
	})
}

func TestDestroyingDefaultsClearsOnlyAdminTechAndBilling(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: cfg + defaultsConfig(owner, "  admin_contact_id = regapi_contact.agency.id\n")},
			{
				Config: cfg + agencyContact,
				Check: func(*terraform.State) error {
					account := f.Account()
					if account.DefaultContactID != owner || account.DefaultAdminContactID != "" {
						return fmt.Errorf("defaults after destroy = %+v", account)
					}
					return nil
				},
			},
		},
	})
}

func TestDestroyingDefaultsWarnsThatTheRegistrantStays(t *testing.T) {
	state := &defaultsModel{ID: types.StringValue("acc_1")}
	got := warningText(defaultsWarnings(state, nil))
	if !strings.Contains(got, "registrant") {
		t.Errorf("warning = %q", got)
	}
	if ds := defaultsWarnings(state, state); len(ds) != 0 {
		t.Errorf("an unchanged plan warned: %v", ds)
	}
}
