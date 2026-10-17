data "regapi_domain_quote" "client" {
  domain_name = "client.gr"
}

output "price" {
  value = data.regapi_domain_quote.client.registrable ? data.regapi_domain_quote.client.price_cents : null
}
