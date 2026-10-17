data "regapi_domain_quote" "client" {
  domain_name = "client.gr"
}

resource "regapi_domain_registration" "client" {
  domain_name          = "client.gr"
  expected_price_cents = data.regapi_domain_quote.client.price_cents
  wait_for_active      = true
}
