// Package fakeapi is an in-memory fake of the parts of the RegAPI HTTP API the provider uses.
// It follows the real API's rules closely enough for provider tests; a contract test keeps
// its routes and fields inside RegAPI's openapi.yaml.
package fakeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

// APIKey is the only key the fake accepts.
const APIKey = "fake-key"

// PriceCents is the price of every registration.
const PriceCents = 2000

const accountID = "acc_1"

type order struct {
	client.Order
	req client.RegisterRequest
}

type domain struct {
	d       client.Domain
	pending *client.DomainPatch
	lag     int
}

// Server is the fake API.
type Server struct {
	// SavedCard makes registrations charge a saved card and complete at once.
	SavedCard bool
	// PatchLag is the number of reads of a domain before a PATCH becomes visible.
	PatchLag int

	mu       sync.Mutex
	failNext string
	next     int
	contacts map[string]client.Contact
	account  client.Account
	domains  map[string]*domain
	orders   []*order // oldest first
	mux      *http.ServeMux
	routes   []string
}

// New returns an empty fake.
func New() *Server {
	s := &Server{contacts: map[string]client.Contact{}, domains: map[string]*domain{},
		account: client.Account{ID: accountID}, mux: http.NewServeMux()}
	for _, r := range []struct {
		pattern string
		h       http.HandlerFunc
	}{
		{"GET /v1/account", s.getAccount},
		{"PATCH /v1/account", s.patchAccount},
		{"POST /v1/contacts", s.createContact},
		{"GET /v1/contacts/{id}", s.getContact},
		{"PATCH /v1/contacts/{id}", s.patchContact},
		{"DELETE /v1/contacts/{id}", s.deleteContact},
		{"POST /v1/domains/check", s.check},
		{"POST /v1/domains", s.register},
		{"GET /v1/domains/{name}", s.getDomain},
		{"PATCH /v1/domains/{name}", s.patchDomain},
		{"GET /v1/orders", s.listOrders},
		{"GET /v1/orders/{id}", s.getOrder},
	} {
		s.routes = append(s.routes, r.pattern)
		s.mux.HandleFunc(r.pattern, r.h)
	}
	return s
}

// Routes lists the "METHOD /path" patterns the fake serves.
func (s *Server) Routes() []string { return s.routes }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+APIKey {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing or invalid API key", nil)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mux.ServeHTTP(w, r)
}

func (s *Server) id(prefix string) string {
	s.next++
	return fmt.Sprintf("%s_%d", prefix, s.next)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, usedBy []client.Usage) {
	body := map[string]any{"code": code, "message": msg}
	if len(usedBy) > 0 {
		body["used_by"] = usedBy
	}
	writeJSON(w, status, map[string]any{"error": body})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "invalid JSON body", nil)
		return false
	}
	return true
}

func notFound(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, "not_found", what+" not found", nil)
}

// ContactIDs lists the IDs of the stored contacts.
func (s *Server) ContactIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id := range s.contacts {
		ids = append(ids, id)
	}
	return ids
}

// OrderCount is the number of orders placed.
func (s *Server) OrderCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.orders)
}

// AddDomain stores an active domain with the given contacts.
func (s *Server) AddDomain(name string, contacts client.DomainContacts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.domains[name] = &domain{d: client.Domain{DomainName: name, Phase: "Active",
		ExpiryDate: "2030-01-01T00:00:00Z", Nameservers: []string{"ns1.default.test"},
		NameserversSource: "default", Contacts: contacts}}
}

// Pay completes an awaiting order and creates its domain.
func (s *Server) Pay(orderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.ID == orderID && o.State == "awaiting_payment" {
			s.complete(o)
		}
	}
}

// FailOrder fails an unfinished order with a code.
func (s *Server) FailOrder(orderID, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.ID == orderID {
			o.State, o.ErrorCode, o.ErrorMessage = "failed", code, "registration failed"
		}
	}
}

func (s *Server) complete(o *order) {
	o.State = "completed"
	o.Payment = &client.Payment{Method: "checkout", Status: "completed"}
	c := o.req.Contacts
	s.domains[o.DomainName] = &domain{d: client.Domain{DomainName: o.DomainName, Phase: "Active",
		ExpiryDate: "2030-01-01T00:00:00Z", Nameservers: o.req.Nameservers,
		NameserversSource: "custom", AutoRenew: o.req.AutoRenew,
		Contacts: client.DomainContacts{Registrant: nilIfEmpty(c.Registrant),
			Admin: nilIfEmpty(c.Admin), Tech: nilIfEmpty(c.Tech), Billing: nilIfEmpty(c.Billing)}}}
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domains []string `json:"domains"`
	}
	if !decode(w, r, &in) {
		return
	}
	results := []client.CheckResult{}
	for _, name := range in.Domains {
		res := client.CheckResult{DomainName: name}
		switch {
		case !strings.HasSuffix(name, ".gr"):
			res.Reason = "extension_not_supported"
		case s.domains[name] != nil:
			res.Reason = "domain_unavailable"
		default:
			res.Registrable = true
			res.Price = &client.Price{AmountCents: PriceCents, Currency: "EUR", Years: 2}
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	out := []client.Order{}
	for i := len(s.orders) - 1; i >= 0; i-- { // newest first
		o := s.orders[i].Order
		if state == "" || o.State == state {
			o.Payment = nil
			out = append(out, o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out, "next": ""})
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	for _, o := range s.orders {
		if o.ID == r.PathValue("id") {
			writeJSON(w, http.StatusOK, o.Order)
			return
		}
	}
	notFound(w, "order")
}

// refuseRegistration answers the API's refusal of a registration request, if any.
func (s *Server) refuseRegistration(w http.ResponseWriter, req client.RegisterRequest) bool {
	switch {
	case !strings.HasSuffix(req.DomainName, ".gr"):
		writeError(w, http.StatusUnprocessableEntity, "extension_not_supported",
			"RegAPI does not register names under that extension", nil)
	case s.domains[req.DomainName] != nil:
		writeError(w, http.StatusConflict, "domain_taken", req.DomainName+" is taken", nil)
	case req.Contacts.Registrant == "":
		writeError(w, http.StatusUnprocessableEntity, "default_contact_missing",
			"name contacts.registrant or set a default contact with PATCH /v1/account", nil)
	case req.ExpectedPriceCents != nil && *req.ExpectedPriceCents != PriceCents:
		writeError(w, http.StatusConflict, "price_changed",
			fmt.Sprintf("the price is %d cents, not %d", PriceCents, *req.ExpectedPriceCents), nil)
	default:
		return false
	}
	return true
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req client.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	s.fillDefaults(&req.Contacts)
	if s.refuseRegistration(w, req) {
		return
	}
	if req.Years == 0 {
		req.Years = 2
	}
	o := &order{req: req, Order: client.Order{ID: s.id("ord"), Kind: "register",
		DomainName: req.DomainName, Years: req.Years, AmountCents: PriceCents, Currency: "EUR",
		State: "awaiting_payment", CreatedAt: time.Now().UTC()}}
	o.Payment = &client.Payment{Method: "checkout", Status: "pending",
		CheckoutURL: "https://pay.test/checkout/" + o.ID}
	switch {
	case s.failNext != "":
		o.State, o.ErrorCode, o.ErrorMessage = "failed", s.failNext, "registration failed"
		s.failNext = ""
	case s.SavedCard && req.PaymentMethod != "checkout":
		s.complete(o)
		o.Payment.Method = "saved_card"
	}
	s.orders = append(s.orders, o)
	writeJSON(w, http.StatusAccepted, map[string]any{"order": o.Order})
}

func (s *Server) fillDefaults(c *client.ContactIDs) {
	for _, f := range []struct {
		dst *string
		def string
	}{{&c.Registrant, s.account.DefaultContactID}, {&c.Admin, s.account.DefaultAdminContactID},
		{&c.Tech, s.account.DefaultTechContactID}, {&c.Billing, s.account.DefaultBillingContactID}} {
		if *f.dst == "" {
			*f.dst = f.def
		}
	}
}
