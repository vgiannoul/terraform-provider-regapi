# Changelog

## Unreleased

- `regapi_contact.phone` is required: RegAPI and the registries reject a contact without one.
- The acceptance tests ran against a RegAPI demo environment: contacts, account defaults and an
  unpaid registration pass.

- Resources `regapi_contact`, `regapi_account_defaults`, `regapi_domain`,
  `regapi_domain_registration`; data sources `regapi_domain`, `regapi_domain_quote`.
- Destroying `regapi_account_defaults` clears all four defaults; it needs a RegAPI that allows
  clearing the default registrant.
