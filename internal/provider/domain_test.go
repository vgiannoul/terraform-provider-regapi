package provider

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
	"github.com/vgiannoul/terraform-provider-regapi/internal/fakeapi"
)

func domainConfig(adminID, nameservers, autoRenew string) string {
	return `
resource "regapi_domain" "d" {
  name             = "acme.gr"
  nameservers      = ` + nameservers + `
  auto_renew       = ` + autoRenew + `
  admin_contact_id = "` + adminID + `"
}
`
}

// agency creates the administrative contact outside Terraform: a contact a registered domain
// uses cannot be deleted, so a managed one could not be destroyed together with the domain.
func agency(f *fakeapi.Server) string {
	return f.AddContact(client.Contact{FirstName: "Agency", LastName: "Ops", Email: "ops@x.gr",
		CountryCode: "GR", Street: "Odos", Number: "2", City: "Athens", PostalCode: "10431",
		State: "Attica"})
}

func TestDomainAdoptsAndUpdatesAnExistingDomain(t *testing.T) {
	f, cfg := startFake(t)
	admin := agency(f)
	f.PatchLag = 2
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				PreConfig: func() { f.AddDomain("acme.gr", client.DomainContacts{}) },
				Config:    cfg + domainConfig(admin, `["ns1.acme.test", "ns2.acme.test"]`, "true"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("regapi_domain.d", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("regapi_domain.d", "auto_renew", "true"),
					resource.TestCheckResourceAttr("regapi_domain.d", "admin_contact_id", admin),
					resource.TestCheckResourceAttr("regapi_domain.d", "phase", "Active")),
			},
			{ResourceName: "regapi_domain.d", ImportState: true, ImportStateId: "acme.gr",
				ImportStateVerify: true, ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore: []string{"timeouts"}},
			{
				Config: cfg + domainConfig(admin, `["ns1.other.test", "ns2.other.test"]`, "false"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("regapi_domain.d", "nameservers.0", "ns1.other.test"),
					resource.TestCheckResourceAttr("regapi_domain.d", "auto_renew", "false")),
			},
		},
	})
}

func TestDomainFailsWhenTheNameIsNotRegistered(t *testing.T) {
	f, cfg := startFake(t)
	admin := agency(f)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg + domainConfig(admin, `["ns1.acme.test"]`, "true"),
			ExpectError: regexp.MustCompile(`regapi_domain_registration`),
		}},
	})
}

func TestDestroyingADomainLeavesItRegistered(t *testing.T) {
	f, cfg := startFake(t)
	admin := agency(f)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				PreConfig: func() { f.AddDomain("acme.gr", client.DomainContacts{}) },
				Config:    cfg + domainConfig(admin, `["ns1.acme.test"]`, "true"),
			},
			{Config: cfg},
		},
	})
	if !f.HasDomain("acme.gr") {
		t.Error("destroying the resource removed the domain")
	}
}

func TestADomainDataSourceReadsADomain(t *testing.T) {
	f, cfg := startFake(t)
	f.AddDomain("acme.gr", client.DomainContacts{})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `data "regapi_domain" "d" { name = "acme.gr" }`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.regapi_domain.d", "phase", "Active"),
				resource.TestCheckResourceAttr("data.regapi_domain.d", "nameservers.0",
					"ns1.default.test")),
		}},
	})
}

func domainModelFor(name string) *domainModel {
	return &domainModel{Name: types.StringValue(name)}
}

func TestDomainWarnings(t *testing.T) {
	destroy := warningText(domainWarnings(domainModelFor("a.gr"), nil))
	if !strings.Contains(destroy, "stays registered") {
		t.Errorf("destroy warning = %q", destroy)
	}
	replace := warningText(domainWarnings(domainModelFor("a.gr"), domainModelFor("b.gr")))
	if !strings.Contains(replace, "already be registered") {
		t.Errorf("replace warning = %q", replace)
	}
	if ds := domainWarnings(domainModelFor("a.gr"), domainModelFor("a.gr")); len(ds) != 0 {
		t.Errorf("no change warned: %v", ds)
	}
}
