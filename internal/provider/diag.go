package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

// addAPIError turns an error from the client into one diagnostic. Known API codes get
// advice; nothing here ever includes the API key.
func addAPIError(d *diag.Diagnostics, summary string, err error) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		d.AddError(summary, err.Error())
		return
	}
	detail := apiErr.Message
	switch apiErr.Code {
	case "unauthenticated":
		detail += " Check the provider's api_key or the REGAPI_API_KEY environment variable."
	case "contact_in_use":
		detail += " Used by: " + describeUsage(apiErr.UsedBy) + ". Move those to another " +
			"contact first."
	case "registrant_change_needs_trade":
		detail += " Changing the registrant to a different holder needs a trade, which " +
			"RegAPI does not support yet."
	case "default_contact_missing":
		detail += " Set a default registrant with regapi_account_defaults or give the " +
			"registration a registrant_contact_id."
	}
	d.AddError(summary, fmt.Sprintf("%s [%s]", detail, apiErr.Code))
}

func describeUsage(uses []client.Usage) string {
	parts := make([]string, 0, len(uses))
	for _, u := range uses {
		if u.Kind == "account_default" {
			parts = append(parts, "account default ("+u.Role+")")
			continue
		}
		parts = append(parts, u.Kind+" "+u.ID)
	}
	return strings.Join(parts, ", ")
}
