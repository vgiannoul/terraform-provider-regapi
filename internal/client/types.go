package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Contact is a RegAPI contact.
type Contact struct {
	ID          string `json:"id,omitempty"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	NamePrefix  string `json:"name_prefix,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	Email       string `json:"email"`
	Phone       string `json:"phone,omitempty"`
	Street      string `json:"street,omitempty"`
	Number      string `json:"number,omitempty"`
	Suffix      string `json:"suffix,omitempty"`
	City        string `json:"city,omitempty"`
	PostalCode  string `json:"postal_code,omitempty"`
	State       string `json:"state,omitempty"`
	CountryCode string `json:"country_code"`
	TaxID       string `json:"tax_id,omitempty"`
}

// CreateContact stores a contact.
func (c *Client) CreateContact(ctx context.Context, in Contact) (Contact, error) {
	var out Contact
	err := c.do(ctx, http.MethodPost, "/v1/contacts", in, &out)
	return out, err
}

// GetContact returns one contact.
func (c *Client) GetContact(ctx context.Context, id string) (Contact, error) {
	var out Contact
	err := c.do(ctx, http.MethodGet, "/v1/contacts/"+url.PathEscape(id), nil, &out)
	return out, err
}

// UpdateContact replaces a contact's details (its name cannot change).
func (c *Client) UpdateContact(ctx context.Context, id string, in Contact) (Contact, error) {
	var out Contact
	err := c.do(ctx, http.MethodPatch, "/v1/contacts/"+url.PathEscape(id), in, &out)
	return out, err
}

// DeleteContact removes a contact nothing uses.
func (c *Client) DeleteContact(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/contacts/"+url.PathEscape(id), nil, nil)
}

// Account is the calling account and its default contacts ("" when unset).
type Account struct {
	ID                      string `json:"id"`
	DefaultContactID        string `json:"default_contact_id"`
	DefaultAdminContactID   string `json:"default_admin_contact_id"`
	DefaultTechContactID    string `json:"default_tech_contact_id"`
	DefaultBillingContactID string `json:"default_billing_contact_id"`
}

// GetAccount returns the calling account.
func (c *Client) GetAccount(ctx context.Context) (Account, error) {
	var out struct {
		Account Account `json:"account"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/account", nil, &out)
	return out.Account, err
}

// DefaultsPatch changes default contacts; nil fields are left alone, "" clears one.
type DefaultsPatch struct {
	Registrant *string `json:"default_contact_id,omitempty"`
	Admin      *string `json:"default_admin_contact_id,omitempty"`
	Tech       *string `json:"default_tech_contact_id,omitempty"`
	Billing    *string `json:"default_billing_contact_id,omitempty"`
}

// SetDefaults applies p and returns the account.
func (c *Client) SetDefaults(ctx context.Context, p DefaultsPatch) (Account, error) {
	var out struct {
		Account Account `json:"account"`
	}
	err := c.do(ctx, http.MethodPatch, "/v1/account", p, &out)
	return out.Account, err
}

// DomainContacts are the contact IDs of a domain; nil when the role is unset.
type DomainContacts struct {
	Registrant *string `json:"registrant"`
	Admin      *string `json:"admin"`
	Tech       *string `json:"tech"`
	Billing    *string `json:"billing"`
}

// Domain is a registered domain.
type Domain struct {
	DomainName        string         `json:"domain_name"`
	Phase             string         `json:"phase"`
	ExpiryDate        string         `json:"expiry_date"`
	Nameservers       []string       `json:"nameservers"`
	NameserversSource string         `json:"nameservers_source"`
	AutoRenew         bool           `json:"auto_renew"`
	Contacts          DomainContacts `json:"contacts"`
}

// GetDomain returns one of the account's domains.
func (c *Client) GetDomain(ctx context.Context, name string) (Domain, error) {
	var out Domain
	err := c.do(ctx, http.MethodGet, "/v1/domains/"+url.PathEscape(name), nil, &out)
	return out, err
}

// ContactsPatch changes a domain's contacts; nil roles are left alone, "" clears one.
type ContactsPatch struct {
	Registrant *string `json:"registrant,omitempty"`
	Admin      *string `json:"admin,omitempty"`
	Tech       *string `json:"tech,omitempty"`
	Billing    *string `json:"billing,omitempty"`
}

// DomainPatch changes a domain; nil fields are left alone.
type DomainPatch struct {
	Nameservers *[]string      `json:"nameservers,omitempty"`
	AutoRenew   *bool          `json:"auto_renew,omitempty"`
	Contacts    *ContactsPatch `json:"contacts,omitempty"`
}

// Empty reports whether p changes nothing.
func (p DomainPatch) Empty() bool {
	return p.Nameservers == nil && p.AutoRenew == nil && p.Contacts == nil
}

// Matches reports whether d already shows everything p asks for. The API applies a patch
// asynchronously, so callers poll until it does.
func (p DomainPatch) Matches(d Domain) bool {
	if p.Nameservers != nil && !sameNameservers(*p.Nameservers, d.Nameservers) {
		return false
	}
	if p.AutoRenew != nil && *p.AutoRenew != d.AutoRenew {
		return false
	}
	if p.Contacts == nil {
		return true
	}
	return roleMatches(p.Contacts.Registrant, d.Contacts.Registrant) &&
		roleMatches(p.Contacts.Admin, d.Contacts.Admin) &&
		roleMatches(p.Contacts.Tech, d.Contacts.Tech) &&
		roleMatches(p.Contacts.Billing, d.Contacts.Billing)
}

func roleMatches(want, have *string) bool {
	if want == nil {
		return true
	}
	if *want == "" {
		return have == nil
	}
	return have != nil && *have == *want
}

func sameNameservers(want, have []string) bool {
	if len(want) != len(have) {
		return false
	}
	for i := range want {
		if !strings.EqualFold(want[i], have[i]) {
			return false
		}
	}
	return true
}

// UpdateDomain patches a domain.
func (c *Client) UpdateDomain(ctx context.Context, name string, p DomainPatch) error {
	return c.do(ctx, http.MethodPatch, "/v1/domains/"+url.PathEscape(name), p, nil)
}

// ContactIDs name the contacts of an order; empty roles use the account's defaults.
type ContactIDs struct {
	Registrant string `json:"registrant,omitempty"`
	Admin      string `json:"admin,omitempty"`
	Tech       string `json:"tech,omitempty"`
	Billing    string `json:"billing,omitempty"`
}

// RegisterRequest orders a registration.
type RegisterRequest struct {
	DomainName         string     `json:"domain_name"`
	Years              int        `json:"years,omitempty"`
	Contacts           ContactIDs `json:"contacts"`
	Nameservers        []string   `json:"nameservers,omitempty"`
	AutoRenew          bool       `json:"auto_renew,omitempty"`
	PaymentMethod      string     `json:"payment_method,omitempty"`
	ExpectedPriceCents *int64     `json:"expected_price_cents,omitempty"`
}

// BankInstructions say how to pay an order by bank transfer.
type BankInstructions struct {
	Beneficiary string `json:"beneficiary"`
	IBAN        string `json:"iban"`
	BIC         string `json:"bic"`
	Reference   string `json:"reference"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// Payment is the latest attempt to collect an order's price.
type Payment struct {
	Method       string            `json:"method"`
	Status       string            `json:"status"`
	CheckoutURL  string            `json:"checkout_url"`
	Instructions *BankInstructions `json:"instructions"`
}

// Order is a paid operation on a domain.
type Order struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	DomainName   string    `json:"domain_name"`
	Years        int       `json:"years"`
	AmountCents  int64     `json:"amount_cents"`
	Currency     string    `json:"currency"`
	State        string    `json:"state"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	Payment      *Payment  `json:"payment"`
}

// RegisterDomain places a registration order.
func (c *Client) RegisterDomain(ctx context.Context, req RegisterRequest) (Order, error) {
	var out struct {
		Order Order `json:"order"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/domains", req, &out)
	return out.Order, err
}

// GetOrder returns one order with its payment details.
func (c *Client) GetOrder(ctx context.Context, id string) (Order, error) {
	var out Order
	err := c.do(ctx, http.MethodGet, "/v1/orders/"+url.PathEscape(id), nil, &out)
	return out, err
}

// VisitOrders calls visit for the account's orders in state, newest first, until visit
// returns true or the orders run out. The list omits payment details.
func (c *Client) VisitOrders(
	ctx context.Context, state string, visit func(Order) (stop bool),
) error {
	after := ""
	for {
		q := url.Values{"state": {state}, "limit": {"100"}}
		if after != "" {
			q.Set("after", after)
		}
		var page struct {
			Orders []Order `json:"orders"`
			Next   string  `json:"next"`
		}
		if err := c.do(ctx, http.MethodGet, "/v1/orders?"+q.Encode(), nil, &page); err != nil {
			return err
		}
		for _, o := range page.Orders {
			if visit(o) {
				return nil
			}
		}
		if page.Next == "" {
			return nil
		}
		after = page.Next
	}
}

// Price is a quoted price.
type Price struct {
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Years       int    `json:"years"`
}

// CheckResult is the live availability of a name.
type CheckResult struct {
	DomainName  string `json:"domain_name"`
	Registrable bool   `json:"registrable"`
	Reason      string `json:"reason"`
	Price       *Price `json:"price"`
}

// CheckDomains asks the registry about names.
func (c *Client) CheckDomains(ctx context.Context, names []string) ([]CheckResult, error) {
	var out struct {
		Results []CheckResult `json:"results"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/domains/check",
		map[string][]string{"domains": names}, &out)
	return out.Results, err
}
