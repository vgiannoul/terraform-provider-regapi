# Terraform provider for RegAPI

Status: draft for review, 2026-10-16. Third of four specs (see Context).

## Context

RegAPI's customers are agencies and organisations that manage more than one domain. They want
contacts, domains and their settings as code: reviewable in a pull request, repeatable, and
visible as a plan before anything changes. A Terraform (and OpenTofu) provider gives them that
without a web session.

Related work, in order:

1. **Contact model** (merged): first/last name, prefix, company, street/number/suffix; names
   immutable.
2. **Domain contacts API** (merged, !65): `contacts` on `GET`/`PATCH /v1/domains/{name}`,
   account default admin/tech/billing contacts, `DELETE /v1/contacts/{id}`.
3. **This spec:** the provider, written against the released API.
4. **Portability:** Greek and Latin spellings, tax office, registry person codes, export
   bundle, event log, backups.

## Goals

- Create, change, replace and delete contacts as code.
- Set an account's default contacts as code.
- Register a domain as code, safely: a price guard, no duplicate orders on re-apply, and a
  clear failure when payment is still due.
- Manage an existing domain's nameservers, auto-renew and contacts as code.
- Read domains and price quotes in a configuration.

## Non-goals

- Paying: the provider never pays or confirms payment; it reports how to pay.
- Transfers in, transfer-out auth codes, renewals as resources, trades.
- DNS records (RegAPI does not host DNS).
- Account creation, API keys and billing documents.

## Repository and distribution

- Repository `github.com/vgiannoul/terraform-provider-regapi`, public. The Terraform and OpenTofu
  registries publish only from a public GitHub repository named `terraform-provider-<name>`;
  RegAPI itself stays on GitLab. The owner is the registry namespace
  (`registry.terraform.io/providers/vgiannoul/regapi`) and cannot be changed cheaply later;
  decide it before the first release.
- Go, Terraform Plugin Framework v1.19.0, `terraform-plugin-testing` v1.16.0, protocol 6.
  Versions are looked up again when the repository is created.
- Releases: GoReleaser, GPG-signed checksum file, `terraform-registry-manifest.json`. Versions
  are `v0.x` until the API is stable, then `v1.0.0`.
- Docs generated with `tfplugindocs` from schema descriptions and `examples/`, including an
  agency example (defaults, several contacts, domains).
- CI on GitHub Actions: lint (`golangci-lint`), tests, `tfplugindocs` freshness, actions pinned
  to SHAs with `persist-credentials: false`, scanned with `zizmor`; Dependabot with 7-day
  cooldowns, grouped.

## Provider

```hcl
provider "regapi" {
  endpoint = "https://api.regapi.dev"   # optional
  api_key  = var.regapi_api_key         # sensitive; or REGAPI_API_KEY
}
```

- `api_key` is a RegAPI API key of the account. `endpoint` defaults to production; staging and
  the demo override it.
- Every request sends `User-Agent: terraform-provider-regapi/<version>`. Every write sends an
  `Idempotency-Key`: random per logical request, reused for that request's own retries.
- Retries: 429, 5xx and network errors, up to 3 attempts with backoff, honouring `Retry-After`.
  4xx other than 429 never retry.

## Resources

### `regapi_contact`

| Attribute | Type | Notes |
|---|---|---|
| `id` | string, computed | |
| `first_name`, `last_name` | string, required | change forces replacement |
| `name_prefix`, `company_name` | string, optional | change forces replacement |
| `email`, `country_code` | string, required | |
| `phone`, `street`, `number`, `suffix`, `city`, `postal_code`, `state`, `tax_id` | string, optional | Greek addresses need street, number, city, postal code and state: the API refuses otherwise and the error is shown at apply |

- Create: `POST /v1/contacts`. Update: `PATCH /v1/contacts/{id}` with the full object.
- Delete: `DELETE /v1/contacts/{id}`. On 409 `contact_in_use` the diagnostic lists `used_by`.
  The registrar keeps its handle (see the domain contacts spec).
- Import: by contact ID.
- Replacement creates the new contact before destroying the old one (`create_before_destroy`
  is documented as the recommended lifecycle) so a domain can be moved first.

### `regapi_account_defaults`

| Attribute | Type | Notes |
|---|---|---|
| `registrant_contact_id` | string, required | the default registrant |
| `admin_contact_id`, `tech_contact_id`, `billing_contact_id` | string, optional | |

- Singleton; the ID is the account ID. Create and update: `PATCH /v1/account`. An attribute
  removed from the configuration is cleared (`""`).
- Destroy clears admin, tech and billing and leaves the registrant default, which the API
  cannot clear.
- Import: by account ID.

### `regapi_domain`

Settings of a domain that already exists in the account.

| Attribute | Type | Notes |
|---|---|---|
| `name` | string, required | change forces replacement |
| `nameservers` | list of string, optional, computed | |
| `auto_renew` | bool, optional, computed | |
| `registrant_contact_id`, `admin_contact_id`, `tech_contact_id`, `billing_contact_id` | string, optional, computed | a role left out of the configuration is left as it is |
| `phase`, `expiry_date`, `nameservers_source` | computed | |

- Create adopts: `GET /v1/domains/{name}`; 404 fails with "register it with
  `regapi_domain_registration` first". Then one `PATCH` applies the configured values.
- Update: `PATCH`. The API applies changes asynchronously, so the provider polls `GET` until
  the requested nameservers and contacts are visible (timeout 5 minutes, configurable per
  resource as `timeouts.update`).
- A registrant change to another holder fails with the API's `registrant_change_needs_trade`.
- Destroy removes the resource from state only and warns that the domain stays registered.
- Import: by name.

### `regapi_domain_registration`

The one-time act of registering a domain.

| Attribute | Type | Notes |
|---|---|---|
| `domain_name` | string, required | change forces replacement |
| `years` | number, optional | default: the TLD's minimum term |
| `nameservers`, `auto_renew` | optional | as `regapi_domain` |
| `registrant_contact_id`, `admin_contact_id`, `tech_contact_id`, `billing_contact_id` | string, optional | left out = the account defaults |
| `payment_method` | string, optional | `saved_card` or `checkout`; default per the API |
| `expected_price_cents` | number, optional | the price guard |
| `wait_for_active` | bool, optional, default false | |
| `order_id`, `order_state`, `amount_cents` | computed | |

Create:

1. Look for an existing register order for the name: `awaiting_payment` or `provisioning`, or
   `completed` within the last 24 hours (`GET /v1/orders` has no domain filter, so the
   provider lists by state and matches `domain_name` itself). If one exists, use it; this is
   how an apply repeated after paying finds the order instead of buying twice. Idempotency
   keys are not a retention guarantee, so they are not relied on for this.
2. Otherwise, if the domain already exists in the account, fail: "already registered; manage
   it with `regapi_domain`".
3. Otherwise place an order with `POST /v1/domains`, passing `expected_price_cents` so a
   changed price returns 409 `price_changed` and nothing is charged.
4. Read the order with `GET /v1/orders/{id}` (the list omits payment details). If it is
   `awaiting_payment`, fail the apply with the checkout URL or the bank-transfer
   instructions. The order is kept; paying and applying again finds it.
5. If it is `provisioning` or `completed`, the resource is created. A `failed` order fails the
   apply with its `error_code`. With `wait_for_active`
   the provider polls the domain until it is Active, a failed order or domain fails the
   apply, timeout `timeouts.create` (default 30 minutes).

Only `domain_name` forces replacement. The other attributes are the values the domain was
registered with: changing one later updates state only, and the plan warns "changes after
registration are not applied; use `regapi_domain`". Destroy removes the resource from state
only. No import (a registration is an event, not a configuration).

`regapi_domain_registration` plus `regapi_domain` for the same name is the intended pattern;
the registration's nameservers and contacts are the initial values and `regapi_domain` takes
over afterwards. Both set in conflict is documented, not prevented.

## Data sources

- `regapi_domain`: `name` in; `phase`, `expiry_date`, `nameservers`, `auto_renew` and the
  four contact IDs out.
- `regapi_domain_quote`: `domain_name` in (and optional `years`); `registrable`, `reason`,
  `price_cents`, `currency`, `years` out, from `POST /v1/domains/check`. Its result is
  intended for `expected_price_cents`.

## Warnings before destroy and replace

Each resource implements `ModifyPlan` and adds a warning diagnostic during `terraform plan`
(not only at apply), so a reviewer sees it in the pull request:

- **`regapi_contact` replaced** (a name, prefix or company change): "Changing the name or
  company creates a new contact and deletes this one. Domains that use it must be moved to the
  new contact first, or the delete fails with `contact_in_use`." The warning names the
  attributes that force the replacement.
- **`regapi_contact` destroyed:** "Deleting the contact removes it from RegAPI. It fails with
  `contact_in_use` while a domain, an unfinished order or an account default uses it."
- **`regapi_domain_registration` replaced** (the domain name changed): "This registers a
  second domain and a new order; the old domain stays registered and is charged for."
- **`regapi_domain_registration` changed in place** (any other attribute): "Changes after
  registration are not applied; use `regapi_domain`."
- **`regapi_domain_registration` or `regapi_domain` destroyed:** "This removes the resource
  from Terraform state only. The domain stays registered and keeps renewing."
- **`regapi_domain` replaced** (the name changed): "The old domain is not changed; the new
  name must already be registered."
- **`regapi_account_defaults` destroyed:** "Clears the default admin, tech and billing
  contacts; the default registrant stays."

Warnings never block a plan. Tests assert each warning appears in the plan for its case and
does not appear for an in-place update.

## Error handling

- Each API error code becomes one diagnostic carrying the API's message; the provider adds
  context, not raw provider text. `contact_in_use` lists `used_by`;
  `registrant_change_needs_trade` says a trade is needed; `price_changed` shows both prices;
  `default_contact_missing` says to set `regapi_account_defaults` or a registrant on the
  registration.
- 401: "check `api_key` or `REGAPI_API_KEY`".
- Reads that find a deleted contact or domain remove it from state (drift), so the next plan
  shows it.

## Testing

- **Unit and plan tests** with `terraform-plugin-testing` against an in-memory fake of the
  RegAPI HTTP API in `internal/fakeapi` (contacts, account, domains, check, orders). They run
  on every push, offline.
- **Acceptance tests** behind `TF_ACC=1` against the RegAPI demo environment (`make demo-up`
  in the RegAPI repository: fake providers, fake checkout). They cover create, update and
  replace of contacts, `contact_in_use`, domain adopt, registration with a saved card, and the
  awaiting-payment failure followed by a re-apply after paying. Run manually and nightly.
- **Contract test:** the fake API's responses are checked against RegAPI's `openapi.yaml`
  (vendored with a version pin), so an API change fails the provider's CI.
- **Mutation checks:** break the price guard and the unfinished-order reuse, see a test fail,
  restore.
- Plan warnings: one test per warning in the section above, and one that an in-place update
  carries none.
- Edge cases with tests: empty and unknown values, a domain that disappears, 429 and 5xx
  retries with the same idempotency key, a refused registrant swap, a contact replaced while
  in use.

## Release

First release `v0.1.0` after the acceptance suite passes against the released API. The API
needs no changes for this release. Later API changes that the provider uses are additive.
