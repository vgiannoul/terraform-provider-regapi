data "regapi_domain" "client" {
  name = "client.gr"
}

output "expires" {
  value = data.regapi_domain.client.expiry_date
}
