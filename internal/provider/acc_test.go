package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// accConfig returns a provider block for the RegAPI environment named by REGAPI_ENDPOINT and
// REGAPI_API_KEY (the demo cluster: `make demo-up` in the RegAPI repository), or skips.
func accConfig(t *testing.T) string {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1, REGAPI_ENDPOINT and REGAPI_API_KEY to run acceptance tests")
	}
	endpoint, key := os.Getenv("REGAPI_ENDPOINT"), os.Getenv("REGAPI_API_KEY")
	if endpoint == "" || key == "" {
		t.Skip("REGAPI_ENDPOINT and REGAPI_API_KEY are required")
	}
	return `provider "regapi" {
  endpoint = "` + endpoint + `"
  api_key  = "` + key + `"
}
`
}

func TestAccContactLifecycle(t *testing.T) {
	cfg := accConfig(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg + contactConfig("Ana", "+30.2100000000"),
				Check:  resource.TestCheckResourceAttrSet("regapi_contact.a", "id"),
			},
			{ResourceName: "regapi_contact.a", ImportState: true, ImportStateVerify: true},
			{
				Config: cfg + contactConfig("Ana", "+30.2100000009"),
				Check:  resource.TestCheckResourceAttr("regapi_contact.a", "phone", "+30.2100000009"),
			},
			{
				Config: cfg + contactConfig("Maria", "+30.2100000009"),
				Check:  resource.TestCheckResourceAttr("regapi_contact.a", "first_name", "Maria"),
			},
		},
	})
}

func TestAccRegistrationAwaitsPayment(t *testing.T) {
	cfg := accConfig(t)
	name := fmt.Sprintf("acc-%d.gr", time.Now().Unix())
	config := cfg + `
resource "regapi_contact" "owner" {
  first_name   = "Acc"
  last_name    = "Owner"
  email        = "acc@example.gr"
  country_code = "GR"
  street       = "Odos"
  number       = "1"
  city         = "Athens"
  postal_code  = "10431"
  state        = "Attica"
}
resource "regapi_domain_registration" "r" {
  domain_name           = "` + name + `"
  registrant_contact_id = regapi_contact.owner.id
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile(`(?i)waiting for payment`),
		}},
	})
}
