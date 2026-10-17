# The default registrant must be a contact that already exists: the API cannot clear it, so a
# contact managed by Terraform could not be deleted again while it is the default.
resource "regapi_account_defaults" "agency" {
  registrant_contact_id = "con_existing"
  admin_contact_id      = regapi_contact.agency.id
  tech_contact_id       = regapi_contact.agency.id
  billing_contact_id    = regapi_contact.agency.id
}
