package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
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

// The registration test leaves an unpaid order behind, and a contact an unfinished order uses
// cannot be deleted, so its registrant is created outside Terraform.
func TestAccRegistrationAwaitsPayment(t *testing.T) {
	cfg := accConfig(t)
	api := client.New(os.Getenv("REGAPI_ENDPOINT"), os.Getenv("REGAPI_API_KEY"), "acc-test")
	owner, err := api.CreateContact(t.Context(), client.Contact{FirstName: "Acc", LastName: "Owner",
		Email: "acc@example.gr", Phone: "+30.2100000003", CountryCode: "GR", Street: "Odos",
		Number: "1", City: "Athens", PostalCode: "10431", State: "Attica"})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("acc-%d.gr", time.Now().Unix())
	config := cfg + `
resource "regapi_domain_registration" "r" {
  domain_name           = "` + name + `"
  registrant_contact_id = "` + owner.ID + `"
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
