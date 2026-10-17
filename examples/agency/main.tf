# An agency that registers and manages the domains of a client.

terraform {
  required_providers {
    regapi = {
      source = "vgiannoul/regapi"
    }
  }
}

variable "regapi_api_key" {
  type      = string
  sensitive = true
}

variable "owner_contact_id" {
  type        = string
  description = "An existing contact: the account's default registrant."
}

provider "regapi" {
  api_key = var.regapi_api_key
}

# The agency itself: administrative, technical and billing contact of every domain.
resource "regapi_contact" "agency" {
  first_name   = "Agency"
  last_name    = "Operations"
  company_name = "Agency IKE"
  email        = "ops@agency.example"
  phone        = "+30.2100000001"
  country_code = "GR"
  street       = "Panepistimiou"
  number       = "10"
  city         = "Athens"
  postal_code  = "10564"
  state        = "Attica"
}

# The client is the holder (registrant) of its domain.
resource "regapi_contact" "client" {
  first_name   = "Maria"
  last_name    = "Papadopoulou"
  company_name = "Papadopoulou IKE"
  email        = "maria@client.example"
  phone        = "+30.2100000002"
  country_code = "GR"
  street       = "Ermou"
  number       = "12"
  city         = "Athens"
  postal_code  = "10563"
  state        = "Attica"
}

# New orders use the agency for the roles they leave out.
resource "regapi_account_defaults" "agency" {
  registrant_contact_id = var.owner_contact_id
  admin_contact_id      = regapi_contact.agency.id
  tech_contact_id       = regapi_contact.agency.id
  billing_contact_id    = regapi_contact.agency.id
}

data "regapi_domain_quote" "client" {
  domain_name = "client.gr"
}

# Registers the domain once. If it is not paid the apply fails with the payment instructions;
# pay, then apply again.
resource "regapi_domain_registration" "client" {
  domain_name           = "client.gr"
  registrant_contact_id = regapi_contact.client.id
  expected_price_cents  = data.regapi_domain_quote.client.price_cents
  wait_for_active       = true
  depends_on            = [regapi_account_defaults.agency]
}

# Settings afterwards.
resource "regapi_domain" "client" {
  name        = "client.gr"
  nameservers = ["ns1.client.example", "ns2.client.example"]
  auto_renew  = true
  depends_on  = [regapi_domain_registration.client]
}
