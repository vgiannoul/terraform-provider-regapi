package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

func errorText(t *testing.T, err error) string {
	t.Helper()
	var d diag.Diagnostics
	addAPIError(&d, "Could not do it", err)
	if len(d) != 1 || !d.HasError() {
		t.Fatalf("diagnostics = %v", d)
	}
	return d[0].Summary() + " | " + d[0].Detail()
}

func TestContactInUseListsWhatUsesIt(t *testing.T) {
	got := errorText(t, &client.APIError{Status: 409, Code: "contact_in_use", Message: "in use",
		UsedBy: []client.Usage{{Kind: "domain", ID: "a.gr"},
			{Kind: "account_default", Role: "tech"}}})
	for _, want := range []string{"contact_in_use", "domain a.gr", "account default (tech)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

func TestUnauthenticatedPointsAtTheKey(t *testing.T) {
	got := errorText(t, &client.APIError{Status: 401, Code: "unauthenticated", Message: "no"})
	if !strings.Contains(got, "REGAPI_API_KEY") {
		t.Errorf("%q does not mention the key setting", got)
	}
}

func TestRegistrantTradeAndMissingDefaultHaveAdvice(t *testing.T) {
	trade := errorText(t, &client.APIError{Status: 422, Code: "registrant_change_needs_trade",
		Message: "x"})
	missing := errorText(t, &client.APIError{Status: 422, Code: "default_contact_missing",
		Message: "x"})
	if !strings.Contains(trade, "trade") || !strings.Contains(missing, "regapi_account_defaults") {
		t.Errorf("advice missing: %q / %q", trade, missing)
	}
}

func TestOtherErrorsKeepTheirText(t *testing.T) {
	got := errorText(t, &client.APIError{Status: 500, Code: "internal", Message: "boom"})
	if !strings.Contains(got, "boom") || !strings.Contains(got, "internal") {
		t.Errorf("got %q", got)
	}
}
