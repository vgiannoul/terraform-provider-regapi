resource "regapi_account_defaults" "agency" {
  registrant_contact_id = regapi_contact.owner.id
  admin_contact_id      = regapi_contact.agency.id
  tech_contact_id       = regapi_contact.agency.id
  billing_contact_id    = regapi_contact.agency.id
}
