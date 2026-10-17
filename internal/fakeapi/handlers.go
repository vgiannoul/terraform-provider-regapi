package fakeapi

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

// Account returns the account as the API shows it.
func (s *Server) Account() client.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account
}

// DeleteContact removes a contact, as if it were deleted outside Terraform.
func (s *Server) DeleteContact(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.contacts, id)
}

// HasDomain reports whether the domain exists.
func (s *Server) HasDomain(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.domains[name] != nil
}

// LastOrderID is the ID of the newest order.
func (s *Server) LastOrderID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orders[len(s.orders)-1].ID
}

// FailNextOrder makes the next order placed end failed with code.
func (s *Server) FailNextOrder(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = code
}

// AddOldCompletedOrder stores a completed register order created age ago.
func (s *Server) AddOldCompletedOrder(name string, age time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders = append(s.orders, &order{Order: client.Order{ID: s.id("ord"), Kind: "register",
		DomainName: name, State: "completed", Currency: "EUR", AmountCents: PriceCents,
		CreatedAt: time.Now().UTC().Add(-age)}})
}

func (s *Server) getAccount(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"account": s.account,
		"onboarding": map[string]any{}})
}

type defaultField struct {
	val       *string
	dst       *string
	clearable bool
}

// refuseDefault answers the API's refusal of one default contact change, if any.
func (s *Server) refuseDefault(w http.ResponseWriter, f defaultField) bool {
	switch {
	case f.val == nil:
		return false
	case *f.val == "" && !f.clearable:
		writeError(w, http.StatusUnprocessableEntity, "invalid_request",
			"the default registrant cannot be cleared", nil)
	case *f.val != "" && !s.hasContact(*f.val):
		notFound(w, "contact")
	default:
		return false
	}
	return true
}

func (s *Server) hasContact(id string) bool {
	_, ok := s.contacts[id]
	return ok
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	var in client.DefaultsPatch
	if !decode(w, r, &in) {
		return
	}
	fields := []defaultField{
		{in.Registrant, &s.account.DefaultContactID, false},
		{in.Admin, &s.account.DefaultAdminContactID, true},
		{in.Tech, &s.account.DefaultTechContactID, true},
		{in.Billing, &s.account.DefaultBillingContactID, true},
	}
	for _, f := range fields {
		if s.refuseDefault(w, f) {
			return
		}
	}
	for _, f := range fields {
		if f.val != nil {
			*f.dst = *f.val
		}
	}
	s.getAccount(w, r)
}

func missingFields(c client.Contact) []string {
	need := []struct{ name, val string }{{"first_name", c.FirstName},
		{"last_name", c.LastName}, {"email", c.Email}, {"country_code", c.CountryCode}}
	if c.CountryCode == "GR" {
		need = append(need, struct{ name, val string }{"street", c.Street},
			struct{ name, val string }{"number", c.Number}, struct{ name, val string }{"city", c.City},
			struct{ name, val string }{"postal_code", c.PostalCode},
			struct{ name, val string }{"state", c.State})
	}
	var missing []string
	for _, n := range need {
		if strings.TrimSpace(n.val) == "" {
			missing = append(missing, n.name)
		}
	}
	return missing
}

// name is what a contact cannot change after creation.
func name(c client.Contact) string {
	return strings.Join([]string{strings.TrimSpace(c.FirstName), strings.TrimSpace(c.NamePrefix),
		strings.TrimSpace(c.LastName), strings.TrimSpace(c.CompanyName)}, "|")
}

// holder is the name compared without case, as the registrant rule does.
func holder(c client.Contact) string { return strings.ToLower(name(c)) }

func (s *Server) createContact(w http.ResponseWriter, r *http.Request) {
	var in client.Contact
	if !decode(w, r, &in) {
		return
	}
	if missing := missingFields(in); len(missing) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request",
			"Please fill in: "+strings.Join(missing, ", "), nil)
		return
	}
	in.ID = s.id("con")
	s.contacts[in.ID] = in
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) getContact(w http.ResponseWriter, r *http.Request) {
	c, ok := s.contacts[r.PathValue("id")]
	if !ok {
		notFound(w, "contact")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) patchContact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	current, ok := s.contacts[id]
	if !ok {
		notFound(w, "contact")
		return
	}
	var in client.Contact
	if !decode(w, r, &in) {
		return
	}
	if name(in) != name(current) {
		writeError(w, http.StatusUnprocessableEntity, "contact_name_immutable",
			"the name and company of a contact cannot be changed; create a new contact", nil)
		return
	}
	if missing := missingFields(in); len(missing) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request",
			"Please fill in: "+strings.Join(missing, ", "), nil)
		return
	}
	in.ID = id
	s.contacts[id] = in
	writeJSON(w, http.StatusOK, in)
}

func usesContact(c client.DomainContacts, id string) bool {
	for _, p := range []*string{c.Registrant, c.Admin, c.Tech, c.Billing} {
		if p != nil && *p == id {
			return true
		}
	}
	return false
}

func (s *Server) usedBy(id string) []client.Usage {
	var uses []client.Usage
	for _, domainName := range slices.Sorted(maps.Keys(s.domains)) {
		if usesContact(s.domains[domainName].d.Contacts, id) {
			uses = append(uses, client.Usage{Kind: "domain", ID: domainName})
		}
	}
	for _, d := range []struct{ role, id string }{{"registrant", s.account.DefaultContactID},
		{"admin", s.account.DefaultAdminContactID}, {"tech", s.account.DefaultTechContactID},
		{"billing", s.account.DefaultBillingContactID}} {
		if d.id == id {
			uses = append(uses, client.Usage{Kind: "account_default", Role: d.role})
		}
	}
	return uses
}

func (s *Server) deleteContact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.contacts[id]; !ok {
		notFound(w, "contact")
		return
	}
	if uses := s.usedBy(id); len(uses) > 0 {
		writeError(w, http.StatusConflict, "contact_in_use",
			"the contact is still in use; change or remove what uses it first", uses)
		return
	}
	delete(s.contacts, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.domains[r.PathValue("name")]
	if !ok {
		notFound(w, "domain")
		return
	}
	if d.pending != nil {
		if d.lag == 0 {
			s.applyPatch(d, *d.pending)
			d.pending = nil
		} else {
			d.lag--
		}
	}
	writeJSON(w, http.StatusOK, d.d)
}

func (s *Server) patchDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.domains[r.PathValue("name")]
	if !ok {
		notFound(w, "domain")
		return
	}
	var in client.DomainPatch
	if !decode(w, r, &in) {
		return
	}
	if in.Contacts != nil && !s.checkContactsPatch(w, d, *in.Contacts) {
		return
	}
	if s.PatchLag == 0 {
		s.applyPatch(d, in)
	} else {
		d.pending, d.lag = &in, s.PatchLag
	}
	writeJSON(w, http.StatusAccepted, d.d)
}

func needsTrade(w http.ResponseWriter) {
	writeError(w, http.StatusUnprocessableEntity, "registrant_change_needs_trade",
		"the registrant would change holder; a trade is not supported yet", nil)
}

// checkContactsPatch applies the API's rules to a contacts change and answers the refusal.
func (s *Server) checkContactsPatch(
	w http.ResponseWriter, d *domain, p client.ContactsPatch,
) bool {
	for _, id := range []*string{p.Registrant, p.Admin, p.Tech, p.Billing} {
		if id == nil || *id == "" {
			continue
		}
		if _, ok := s.contacts[*id]; !ok {
			notFound(w, "contact")
			return false
		}
	}
	if p.Registrant == nil {
		return true
	}
	if *p.Registrant == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request",
			"the registrant cannot be cleared", nil)
		return false
	}
	current := d.d.Contacts.Registrant
	if current == nil || holder(s.contacts[*current]) != holder(s.contacts[*p.Registrant]) {
		needsTrade(w)
		return false
	}
	return true
}

func (s *Server) applyPatch(d *domain, p client.DomainPatch) {
	if p.Nameservers != nil {
		d.d.Nameservers, d.d.NameserversSource = *p.Nameservers, "custom"
	}
	if p.AutoRenew != nil {
		d.d.AutoRenew = *p.AutoRenew
	}
	if p.Contacts == nil {
		return
	}
	for _, role := range []struct {
		val *string
		dst **string
	}{{p.Contacts.Registrant, &d.d.Contacts.Registrant}, {p.Contacts.Admin, &d.d.Contacts.Admin},
		{p.Contacts.Tech, &d.d.Contacts.Tech}, {p.Contacts.Billing, &d.d.Contacts.Billing}} {
		if role.val != nil {
			*role.dst = nilIfEmpty(*role.val)
		}
	}
}

// AddContact stores a contact without going through the API and returns its ID.
func (s *Server) AddContact(c client.Contact) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = s.id("con")
	s.contacts[c.ID] = c
	return c.ID
}
