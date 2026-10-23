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
  phone = "+30.2100000001"
  country_code = "GR"
  street = "Odos"
  number = "2"
  city = "Athens"
  postal_code = "10431"
  state = "Attica"
}
`

// ownerContact is a registrant that tests create outside Terraform where they need one that
// survives the test.
var ownerContact = client.Contact{FirstName: "Ana", LastName: "Test", Email: "a@x.gr",
	CountryCode: "GR", Street: "Odos", Number: "1", City: "Athens", PostalCode: "10431",
	State: "Attica"}

const ownerResource = `
resource "regapi_contact" "owner" {
  first_name = "Ana"
  last_name = "Test"
  email = "a@x.gr"
  phone = "+30.2100000000"
  country_code = "GR"
  street = "Odos"
  number = "1"
  city = "Athens"
  postal_code = "10431"
  state = "Attica"
}
`

func defaultsConfig(extra string) string {
	return agencyContact + ownerResource + `
resource "regapi_account_defaults" "d" {
  registrant_contact_id = regapi_contact.owner.id
` + extra + `}
`
}

func TestAccountDefaultsLifecycle(t *testing.T) {
	_, cfg := startFake(t)
	all := `  admin_contact_id   = regapi_contact.agency.id
  tech_contact_id    = regapi_contact.agency.id
  billing_contact_id = regapi_contact.agency.id
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg + defaultsConfig(all),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("regapi_account_defaults.d",
						"admin_contact_id", "regapi_contact.agency", "id"),
					resource.TestCheckResourceAttrPair("regapi_account_defaults.d",
						"registrant_contact_id", "regapi_contact.owner", "id")),
			},
			{ResourceName: "regapi_account_defaults.d", ImportState: true,
				ImportStateVerify: true},
			{
				Config: cfg + defaultsConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("regapi_account_defaults.d", "admin_contact_id"),
					resource.TestCheckNoResourceAttr("regapi_account_defaults.d", "tech_contact_id")),
			},
		},
	})
}

// Destroying the whole configuration works because the defaults, registrant included, are
// cleared before the contacts are deleted.
func TestDestroyingDefaultsClearsAllFourSoTheContactsCanGo(t *testing.T) {
	f, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: cfg + defaultsConfig("  admin_contact_id = regapi_contact.agency.id\n")},
			{
				Config: cfg,
				Check: func(*terraform.State) error {
					account := f.Account()
					if account != (client.Account{ID: account.ID}) {
						return fmt.Errorf("defaults after destroy = %+v", account)
					}
					if n := len(f.ContactIDs()); n != 0 {
						return fmt.Errorf("%d contacts left after destroy", n)
					}
					return nil
				},
			},
		},
	})
}

func TestDestroyingDefaultsWarnsThatAllFourAreCleared(t *testing.T) {
	state := &defaultsModel{ID: types.StringValue("acc_1")}
	got := warningText(defaultsWarnings(state, nil))
	if !strings.Contains(got, "All four") {
		t.Errorf("warning = %q", got)
	}
	if ds := defaultsWarnings(state, state); len(ds) != 0 {
		t.Errorf("an unchanged plan warned: %v", ds)
	}
}
