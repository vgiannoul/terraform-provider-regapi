package fakeapi_test

import (
	"net/http/httptest"
	"testing"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
	"github.com/vgiannoul/terraform-provider-regapi/internal/fakeapi"
)

func setup(t *testing.T) (*fakeapi.Server, *client.Client) {
	t.Helper()
	f := fakeapi.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, client.New(srv.URL, fakeapi.APIKey, "test")
}

var greek = client.Contact{FirstName: "Ana", LastName: "Test", Email: "a@x.gr",
	Phone: "+30.2100000000", CountryCode: "GR", Street: "Odos", Number: "1", City: "Athens",
	PostalCode: "10431",
	State:      "Attica"}

func TestTheFakeRefusesAWrongKey(t *testing.T) {
	f := fakeapi.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, "wrong", "test")
	if _, err := c.GetAccount(t.Context()); !client.IsCode(err, "unauthenticated") {
		t.Fatalf("err = %v", err)
	}
}

func TestContactsFollowTheRealRules(t *testing.T) {
	_, c := setup(t)
	if _, err := c.CreateContact(t.Context(), client.Contact{FirstName: "Ana", LastName: "T",
		Email: "a@x.gr", Phone: "+30.2100000000", CountryCode: "GR"}); !client.IsCode(err,
		"invalid_request") {
		t.Fatalf("Greek contact without an address: %v", err)
	}
	created, err := c.CreateContact(t.Context(), greek)
	if err != nil {
		t.Fatal(err)
	}
	renamed := created
	renamed.LastName = "Other"
	if _, err := c.UpdateContact(t.Context(), created.ID, renamed); !client.IsCode(err,
		"contact_name_immutable") {
		t.Fatalf("renaming: %v", err)
	}
	if err := c.DeleteContact(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetContact(t.Context(), created.ID); !client.IsNotFound(err) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestAContactInUseCannotBeDeleted(t *testing.T) {
	f, c := setup(t)
	created, err := c.CreateContact(t.Context(), greek)
	if err != nil {
		t.Fatal(err)
	}
	reg := created.ID
	f.AddDomain("a.gr", client.DomainContacts{Registrant: &reg})
	err = c.DeleteContact(t.Context(), created.ID)
	if !client.IsCode(err, "contact_in_use") {
		t.Fatalf("err = %v", err)
	}
}

func registered(t *testing.T, c *client.Client) client.Contact {
	t.Helper()
	contact, err := c.CreateContact(t.Context(), greek)
	if err != nil {
		t.Fatal(err)
	}
	patch := client.DefaultsPatch{Registrant: &contact.ID}
	if _, err := c.SetDefaults(t.Context(), patch); err != nil {
		t.Fatal(err)
	}
	return contact
}

func TestAnUnpaidOrderCreatesNoDomainUntilItIsPaid(t *testing.T) {
	f, c := setup(t)
	contact := registered(t, c)
	order, err := c.RegisterDomain(t.Context(), client.RegisterRequest{DomainName: "a.gr"})
	if err != nil || order.State != "awaiting_payment" {
		t.Fatalf("order = %+v, err %v", order, err)
	}
	if _, err := c.GetDomain(t.Context(), "a.gr"); !client.IsNotFound(err) {
		t.Fatalf("domain exists before payment: %v", err)
	}
	f.Pay(order.ID)
	if paid, err := c.GetOrder(t.Context(), order.ID); err != nil || paid.State != "completed" {
		t.Fatalf("paid = %+v, err %v", paid, err)
	}
	assertActiveWith(t, c, "a.gr", contact.ID)
}

func assertActiveWith(t *testing.T, c *client.Client, name, registrant string) {
	t.Helper()
	d, err := c.GetDomain(t.Context(), name)
	if err != nil || d.Phase != "Active" || d.Contacts.Registrant == nil ||
		*d.Contacts.Registrant != registrant {
		t.Fatalf("domain = %+v, err %v", d, err)
	}
}

func TestASavedCardCompletesTheOrderAtOnce(t *testing.T) {
	f, c := setup(t)
	registered(t, c)
	f.SavedCard = true
	order, err := c.RegisterDomain(t.Context(), client.RegisterRequest{DomainName: "b.gr"})
	if err != nil || order.State != "completed" {
		t.Fatalf("saved-card order = %+v, err %v", order, err)
	}
}

func TestThePriceGuardRefusesAChangedPrice(t *testing.T) {
	_, c := setup(t)
	contact, err := c.CreateContact(t.Context(), greek)
	if err != nil {
		t.Fatal(err)
	}
	wrong := int64(fakeapi.PriceCents + 1)
	_, err = c.RegisterDomain(t.Context(), client.RegisterRequest{DomainName: "a.gr",
		Contacts: client.ContactIDs{Registrant: contact.ID}, ExpectedPriceCents: &wrong})
	if !client.IsCode(err, "price_changed") {
		t.Fatalf("err = %v", err)
	}
	if got := fakeapi.PriceCents; got != 2000 {
		t.Fatalf("price = %d", got)
	}
}

func TestDomainChangesFollowTheRegistrantRule(t *testing.T) {
	f, c := setup(t)
	a, err := c.CreateContact(t.Context(), greek)
	if err != nil {
		t.Fatal(err)
	}
	same := greek
	same.FirstName = "ANA"
	b, err := c.CreateContact(t.Context(), same)
	if err != nil {
		t.Fatal(err)
	}
	other := greek
	other.LastName = "Other"
	o, err := c.CreateContact(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	f.AddDomain("a.gr", client.DomainContacts{Registrant: &a.ID})

	patch := func(id string) error {
		return c.UpdateDomain(t.Context(), "a.gr", client.DomainPatch{
			Contacts: &client.ContactsPatch{Registrant: &id}})
	}
	if err := patch(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := patch(o.ID); !client.IsCode(err, "registrant_change_needs_trade") {
		t.Fatalf("different holder: %v", err)
	}
}

func TestPatchesAppearAfterTheConfiguredLag(t *testing.T) {
	f, c := setup(t)
	f.PatchLag = 2
	f.AddDomain("a.gr", client.DomainContacts{})
	ns := []string{"ns1.x.test", "ns2.x.test"}
	if err := c.UpdateDomain(t.Context(), "a.gr", client.DomainPatch{Nameservers: &ns}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		d, err := c.GetDomain(t.Context(), "a.gr")
		if err != nil {
			t.Fatal(err)
		}
		visible := len(d.Nameservers) == 2
		if visible != (i >= 2) {
			t.Fatalf("read %d: visible = %v", i, visible)
		}
	}
}
