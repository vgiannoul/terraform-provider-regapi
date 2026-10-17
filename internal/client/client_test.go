package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

type seen struct {
	method, path, key, auth, agent, body string
}

// serve answers each request with the next of statuses/bodies and records what it saw.
func serve(t *testing.T, replies ...reply) (*client.Client, *[]seen, *[]time.Duration) {
	t.Helper()
	var mu sync.Mutex
	var requests []seen
	var waits []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		requests = append(requests, seen{r.Method, r.URL.Path, r.Header.Get("Idempotency-Key"),
			r.Header.Get("Authorization"), r.Header.Get("User-Agent"), string(raw)})
		rep := replies[min(len(requests), len(replies))-1]
		for k, v := range rep.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		_, _ = io.WriteString(w, rep.body)
	}))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, "secret-key", "9.9.9", client.WithSleep(
		func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }))
	return c, &requests, &waits
}

type reply struct {
	status  int
	body    string
	headers map[string]string
}

const contactJSON = `{"id":"con_1","first_name":"Ana","last_name":"Test","email":"a@x.gr",
"country_code":"GR"}`

func TestRequestsCarryAuthAndAgent(t *testing.T) {
	c, requests, _ := serve(t, reply{status: 200, body: contactJSON})
	if _, err := c.GetContact(t.Context(), "con_1"); err != nil {
		t.Fatal(err)
	}
	got := (*requests)[0]
	if got.auth != "Bearer secret-key" || got.agent != "terraform-provider-regapi/9.9.9" ||
		got.path != "/v1/contacts/con_1" || got.key != "" {
		t.Errorf("request = %+v", got)
	}
}

func TestARetriedWriteReusesItsIdempotencyKey(t *testing.T) {
	c, requests, waits := serve(t,
		reply{status: 503, body: `{"error":{"code":"provider_unavailable","message":"x"}}`},
		reply{status: 429, body: `{"error":{"code":"rate_limited","message":"x"}}`,
			headers: map[string]string{"Retry-After": "7"}},
		reply{status: 201, body: contactJSON})
	if _, err := c.CreateContact(t.Context(), client.Contact{FirstName: "Ana"}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 3 {
		t.Fatalf("%d requests, want 3", len(*requests))
	}
	first := (*requests)[0].key
	if first == "" || (*requests)[1].key != first || (*requests)[2].key != first {
		t.Errorf("keys = %q %q %q", first, (*requests)[1].key, (*requests)[2].key)
	}
	if len(*waits) != 2 || (*waits)[1] != 7*time.Second {
		t.Errorf("waits = %v, want the second to honour Retry-After", *waits)
	}
}

func TestTwoWritesUseDifferentKeys(t *testing.T) {
	c, requests, _ := serve(t, reply{status: 201, body: contactJSON})
	for range 2 {
		if _, err := c.CreateContact(t.Context(), client.Contact{FirstName: "Ana"}); err != nil {
			t.Fatal(err)
		}
	}
	if (*requests)[0].key == (*requests)[1].key {
		t.Error("two logical requests shared a key")
	}
}

func TestAClientErrorIsNotRetriedAndKeepsItsCode(t *testing.T) {
	c, requests, _ := serve(t, reply{status: 409, body: `{"error":{"code":"contact_in_use",
"message":"in use","used_by":[{"kind":"domain","id":"a.gr"}]}}`})
	err := c.DeleteContact(t.Context(), "con_1")
	if len(*requests) != 1 {
		t.Fatalf("%d requests, want 1", len(*requests))
	}
	var apiErr *client.APIError
	if !client.IsCode(err, "contact_in_use") || !asAPIError(err, &apiErr) ||
		len(apiErr.UsedBy) != 1 || apiErr.UsedBy[0].ID != "a.gr" || apiErr.Status != 409 {
		t.Fatalf("err = %#v", err)
	}
}

func TestGivingUpAfterThreeAttempts(t *testing.T) {
	c, requests, _ := serve(t, reply{status: 500, body: `not json`})
	err := c.DeleteContact(t.Context(), "con_1")
	if len(*requests) != 3 || err == nil {
		t.Fatalf("%d requests, err %v", len(*requests), err)
	}
}

func TestTheAPIKeyNeverAppearsInErrors(t *testing.T) {
	c, _, _ := serve(t, reply{status: 401,
		body: `{"error":{"code":"unauthenticated","message":"no"}}`})
	_, err := c.GetContact(t.Context(), "con_1")
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("err = %v", err)
	}
}

func TestNotFoundIsRecognised(t *testing.T) {
	c, _, _ := serve(t, reply{status: 404, body: `{"error":{"code":"not_found","message":"gone"}}`})
	_, err := c.GetContact(t.Context(), "con_1")
	if !client.IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}

func asAPIError(err error, target **client.APIError) bool { return errors.As(err, target) }
