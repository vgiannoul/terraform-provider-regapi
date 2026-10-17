# terraform-provider-regapi

A Terraform and OpenTofu provider for [RegAPI](https://regapi.dev), the registrar for agencies
and organisations that manage more than one domain. Define contacts, default contacts, domain
settings and registrations as code, review the plan, apply it.

```hcl
terraform {
  required_providers {
    regapi = {
      source = "vgiannoul/regapi"
    }
  }
}

provider "regapi" {
  api_key = var.regapi_api_key # or REGAPI_API_KEY
}

resource "regapi_contact" "client" {
  first_name   = "Maria"
  last_name    = "Papadopoulou"
  email        = "maria@client.example"
  country_code = "GR"
  street       = "Ermou"
  number       = "12"
  city         = "Athens"
  postal_code  = "10563"
  state        = "Attica"
}
```

A complete agency example is in [`examples/agency/main.tf`](examples/agency/main.tf).

## Resources and data sources

| Name | What it does |
|---|---|
| [`regapi_contact`](docs/resources/contact.md) | A contact. Its name, prefix and company cannot change; changing them replaces it. |
| [`regapi_account_defaults`](docs/resources/account_defaults.md) | Default registrant, administrative, technical and billing contacts for new orders. |
| [`regapi_domain`](docs/resources/domain.md) | Nameservers, auto-renew and contacts of a domain that is already registered. |
| [`regapi_domain_registration`](docs/resources/domain_registration.md) | Registers a domain, once. |
| [`regapi_domain`](docs/data-sources/domain.md) (data) | Reads a domain. |
| [`regapi_domain_quote`](docs/data-sources/domain_quote.md) (data) | Whether a name is registrable and its price. |

## Behaviour worth knowing

- **The provider never pays.** If a registration order is not paid, `apply` fails and prints
  the checkout URL or the bank-transfer instructions. The order is kept. Pay it, then run
  `apply` again: the provider finds the order instead of placing a second one.
- **Price guard.** Set `expected_price_cents` (from `regapi_domain_quote`) so a changed price
  is refused before anything is charged.
- **Warnings at plan time.** Replacing a contact, destroying a contact, registering a second
  domain, and destroying a domain or registration resource all print a warning in
  `terraform plan`. Destroying a `regapi_domain` or `regapi_domain_registration` only removes
  it from state; the domain stays registered.
- **The default registrant cannot be cleared through the API.** Use a registrant contact that
  Terraform does not manage, otherwise destroying the whole configuration fails with
  `contact_in_use` for that contact.
- **A contact a domain uses cannot be deleted** (`contact_in_use`, with the list of what uses
  it). Move the domain to another contact first; use `create_before_destroy` when you replace
  a contact.
- **Registrant changes** are accepted only between contacts of the same holder. A different
  holder needs a trade, which RegAPI does not support yet.

## Development

```bash
make check   # lint and tests (offline: they run against an in-memory fake of the RegAPI API)
make docs    # regenerate docs/ with tfplugindocs
```

The tests need a Terraform or OpenTofu binary on the path. With OpenTofu the Makefile sets
`TF_ACC_TERRAFORM_PATH`, `TF_ACC_PROVIDER_HOST=registry.opentofu.org` and
`TF_ACC_PROVIDER_NAMESPACE=hashicorp`.

The fake API in `internal/fakeapi` is checked against RegAPI's `openapi.yaml`
(`internal/fakeapi/testdata/openapi.yaml`, refreshed with `scripts/vendor-openapi.sh
<path to a RegAPI checkout>`), so an API change fails this repository's tests.

### Acceptance tests

They run against a real RegAPI, never pay, and are skipped unless `TF_ACC` is set:

```bash
# in the RegAPI repository
make demo-up && deploy/demo/demo.sh api
# create an account with the admin API (see deploy/demo/api-scenario.sh) and note its api_key
TF_ACC=1 REGAPI_ENDPOINT=http://localhost:8080 REGAPI_API_KEY=rk_... make test
```

## Releases

Pushing a tag `vX.Y.Z` runs GoReleaser (`.github/workflows/release.yml`), which signs the
checksums with the GPG key in the repository secrets `GPG_PRIVATE_KEY` and `PASSPHRASE`.
