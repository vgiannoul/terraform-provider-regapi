package provider

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/vgiannoul/terraform-provider-regapi/internal/fakeapi"
)

func init() { pollInterval = time.Millisecond }

func testFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"regapi": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// startFake runs a fake API and returns it with a provider block pointing at it.
func startFake(t *testing.T) (*fakeapi.Server, string) {
	t.Helper()
	f := fakeapi.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, fmt.Sprintf("provider \"regapi\" {\n  endpoint = %q\n  api_key  = %q\n}\n",
		srv.URL, fakeapi.APIKey)
}
