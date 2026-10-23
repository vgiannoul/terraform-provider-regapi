package provider

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

func contactConfig(first, phone string) string {
	return `
resource "regapi_contact" "a" {
  first_name   = "` + first + `"
  last_name    = "Test"
  email        = "ana@example.gr"
  phone        = "` + phone + `"
  country_code = "GR"
  street       = "Odos"
  number       = "1"
  city         = "Athens"
  postal_code  = "10431"
  state        = "Attica"
}
`
}

func TestContactLifecycle(t *testing.T) {
	_, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg + contactConfig("Ana", "+30.2100000000"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("regapi_contact.a", "id"),
					resource.TestCheckResourceAttr("regapi_contact.a", "first_name", "Ana"),
					resource.TestCheckResourceAttr("regapi_contact.a", "state", "Attica")),
			},
			{ResourceName: "regapi_contact.a", ImportState: true, ImportStateVerify: true},
			{
				Config: cfg + contactConfig("Ana", "+30.2100000009"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("regapi_contact.a", plancheck.ResourceActionUpdate)}},
				Check: resource.TestCheckResourceAttr("regapi_contact.a", "phone", "+30.2100000009"),
			},
			{
				Config: cfg + contactConfig("Maria", "+30.2100000009"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("regapi_contact.a",
						plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: resource.TestCheckResourceAttr("regapi_contact.a", "first_name", "Maria"),
			},
		},
	})
}

func TestAContactDeletedOutsideTerraformIsRecreated(t *testing.T) {
	f, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: cfg + contactConfig("Ana", "+30.2100000000")},
			{
				PreConfig: func() { f.DeleteContact(f.ContactIDs()[0]) },
				Config:    cfg + contactConfig("Ana", "+30.2100000000"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("regapi_contact.a", plancheck.ResourceActionCreate)}},
			},
		},
	})
}

func TestADomainStillUsingAContactBlocksItsDeletion(t *testing.T) {
	f, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: cfg + contactConfig("Ana", "+30.2100000000")},
			{
				PreConfig: func() {
					id := f.ContactIDs()[0]
					f.AddDomain("a.gr", client.DomainContacts{Registrant: &id})
				},
				Config:      cfg,
				ExpectError: regexp.MustCompile(`domain\s+a\.gr[\s\S]*contact_in_use`),
			},
			{PreConfig: func() { f.AddDomain("a.gr", client.DomainContacts{}) }, Config: cfg},
		},
	})
}

func TestAGreekContactWithoutAnAddressIsRefusedAtApply(t *testing.T) {
	_, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `
resource "regapi_contact" "a" {
  first_name = "Ana"
  last_name = "Test"
  email = "a@x.gr"
  phone = "+30.2100000000"
  country_code = "GR"
}
`,
			ExpectError: regexp.MustCompile(`invalid_request`),
		}},
	})
}

func TestACountryCodeMustBeUpperCase(t *testing.T) {
	_, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `
resource "regapi_contact" "a" {
  first_name = "Ana"
  last_name = "Test"
  email = "a@x.gr"
  phone = "+30.2100000000"
  country_code = "gr"
}
`,
			ExpectError: regexp.MustCompile(`country_code`),
		}},
	})
}

func contactModelFor(first string) *contactModel {
	return &contactModel{ID: types.StringValue("con_1"), FirstName: types.StringValue(first),
		LastName: types.StringValue("Test"), NamePrefix: types.StringNull(),
		CompanyName: types.StringNull(), Email: types.StringValue("a@x.gr"),
		CountryCode: types.StringValue("GR")}
}

func warningText(ds []diag.Diagnostic) string {
	var parts []string
	for _, d := range ds {
		parts = append(parts, d.Summary()+" "+d.Detail())
	}
	return strings.Join(parts, "\n")
}

func TestReplacingAContactWarnsBeforeIt(t *testing.T) {
	got := warningText(contactWarnings(contactModelFor("Ana"), contactModelFor("Maria")))
	for _, want := range []string{"first_name", "contact_in_use", "new contact"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q lacks %q", got, want)
		}
	}
}

func TestDestroyingAContactWarns(t *testing.T) {
	got := warningText(contactWarnings(contactModelFor("Ana"), nil))
	if !strings.Contains(got, "contact_in_use") {
		t.Errorf("warning = %q", got)
	}
}

func TestCreatingOrUpdatingInPlaceWarnsNothing(t *testing.T) {
	updated := contactModelFor("Ana")
	updated.Phone = types.StringValue("+30.2100000009")
	if ds := contactWarnings(nil, contactModelFor("Ana")); len(ds) != 0 {
		t.Errorf("create warned: %v", ds)
	}
	if ds := contactWarnings(contactModelFor("Ana"), updated); len(ds) != 0 {
		t.Errorf("in-place update warned: %v", ds)
	}
}

func TestAContactWithoutAPhoneIsRefusedAtPlan(t *testing.T) {
	_, cfg := startFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + `
resource "regapi_contact" "a" {
  first_name = "Ana"
  last_name = "Test"
  email = "a@x.gr"
  country_code = "GR"
}
`,
			ExpectError: regexp.MustCompile(`(?s)phone.*required|required.*phone`),
		}},
	})
}
