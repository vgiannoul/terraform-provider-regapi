resource "regapi_domain" "client" {
  name        = "client.gr"
  nameservers = ["ns1.example.net", "ns2.example.net"]
  auto_renew  = true

  # Same holder only: a registrant of a different holder needs a trade, which RegAPI does not
  # support yet.
  admin_contact_id = regapi_contact.agency.id
}
