package provider

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

// registrationConfig sets the account's default registrant to an existing contact (created
// outside Terraform: the registrant default cannot be cleared) and registers acme.gr.
func registrationConfig(owner, extra string) string {
	return `
resource "regapi_account_defaults" "d" {
  registrant_contact_id = "` + owner + `"
}
resource "regapi_domain_registration" "r" {
  domain_name = "acme.gr"
  depends_on  = [regapi_account_defaults.d]
` + extra + `
}
`
}

func TestRegistrationWithASavedCardCompletes(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	f.SavedCard = true
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: cfg + registrationConfig(owner, `  expected_price_cents = 2000
  wait_for_active      = true`),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("regapi_domain_registration.r", "order_state", "completed"),
				resource.TestCheckResourceAttr("regapi_domain_registration.r", "amount_cents", "2000"),
				resource.TestCheckResourceAttrSet("regapi_domain_registration.r", "order_id")),
		}},
	})
}

func TestAnUnpaidRegistrationFailsWithPaymentInstructionsAndReusesItsOrder(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config:      cfg + registrationConfig(owner, ``),
				ExpectError: regexp.MustCompile(`https://pay\.test/checkout/ord_\d+`),
			},
			{
				PreConfig: func() { f.Pay(f.LastOrderID()) },
				Config:    cfg + registrationConfig(owner, ``),
				Check: resource.TestCheckResourceAttr("regapi_domain_registration.r",
					"order_state", "completed"),
			},
		},
	})
	if n := f.OrderCount(); n != 1 {
		t.Errorf("%d orders were placed, want 1", n)
	}
}

func TestARepeatedApplyDoesNotPlaceASecondOrderWhileAwaitingPayment(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: cfg + registrationConfig(owner, ``), ExpectError: regexp.MustCompile(`checkout`)},
			{Config: cfg + registrationConfig(owner, ``), ExpectError: regexp.MustCompile(`checkout`)},
		},
	})
	if n := f.OrderCount(); n != 1 {
		t.Errorf("%d orders were placed, want 1", n)
	}
}

func TestAChangedPriceIsRefusedBeforeAnyCharge(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	f.SavedCard = true
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg + registrationConfig(owner, `  expected_price_cents = 1500`),
			ExpectError: regexp.MustCompile(`price_changed`),
		}},
	})
	if n := f.OrderCount(); n != 0 {
		t.Errorf("%d orders were placed, want 0", n)
	}
}

func TestADomainRegisteredEarlierPointsAtRegapiDomain(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	f.AddDomain("acme.gr", client.DomainContacts{})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg + registrationConfig(owner, ``),
			ExpectError: regexp.MustCompile(`already registered[\s\S]*regapi_domain`),
		}},
	})
}

func TestAFailedOrderFailsTheApplyWithItsCode(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	f.SavedCard = true
	f.FailNextOrder("invalid_contact")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg + registrationConfig(owner, ``),
			ExpectError: regexp.MustCompile(`invalid_contact`),
		}},
	})
}

func TestAnOldCompletedOrderDoesNotHideAnExistingDomain(t *testing.T) {
	f, cfg := startFake(t)
	owner := f.AddContact(ownerContact)
	f.SavedCard = true
	f.AddOldCompletedOrder("acme.gr", 48*time.Hour)
	f.AddDomain("acme.gr", client.DomainContacts{})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg + registrationConfig(owner, ``),
			ExpectError: regexp.MustCompile(`already registered`),
		}},
	})
}

func registrationModelFor(domain, ns string) *registrationModel {
	list, _ := types.ListValueFrom(context.Background(), types.StringType, []string{ns})
	return &registrationModel{DomainName: types.StringValue(domain), Nameservers: list}
}

func TestRegistrationWarnings(t *testing.T) {
	replace := warningText(registrationWarnings(registrationModelFor("a.gr", "ns1"),
		registrationModelFor("b.gr", "ns1")))
	if !strings.Contains(replace, "second domain") {
		t.Errorf("replace warning = %q", replace)
	}
	inPlace := warningText(registrationWarnings(registrationModelFor("a.gr", "ns1"),
		registrationModelFor("a.gr", "ns2")))
	if !strings.Contains(inPlace, "regapi_domain") {
		t.Errorf("in-place warning = %q", inPlace)
	}
	destroy := warningText(registrationWarnings(registrationModelFor("a.gr", "ns1"), nil))
	if !strings.Contains(destroy, "stays registered") {
		t.Errorf("destroy warning = %q", destroy)
	}
	if ds := registrationWarnings(nil, registrationModelFor("a.gr", "ns1")); len(ds) != 0 {
		t.Errorf("create warned: %v", ds)
	}
}

func TestPaymentInstructionsNameTheCheckoutOrTheBankTransfer(t *testing.T) {
	checkout := paymentInstructions(client.Order{ID: "ord_1", AmountCents: 2000, Currency: "EUR",
		Payment: &client.Payment{Method: "checkout", CheckoutURL: "https://pay.test/x"}})
	bank := paymentInstructions(client.Order{ID: "ord_2", Payment: &client.Payment{
		Method: "bank_transfer", Instructions: &client.BankInstructions{IBAN: "GR00", Reference: "R1",
			AmountCents: 2000, Currency: "EUR", Beneficiary: "2square"}}})
	if !strings.Contains(checkout, "https://pay.test/x") || !strings.Contains(checkout, "20.00") {
		t.Errorf("checkout = %q", checkout)
	}
	for _, want := range []string{"GR00", "R1", "20.00", "2square"} {
		if !strings.Contains(bank, want) {
			t.Errorf("bank = %q lacks %q", bank, want)
		}
	}
}
