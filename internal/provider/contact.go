package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

var (
	_ resource.Resource                = (*contactResource)(nil)
	_ resource.ResourceWithConfigure   = (*contactResource)(nil)
	_ resource.ResourceWithImportState = (*contactResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*contactResource)(nil)
)

// NewContactResource returns the regapi_contact resource.
func NewContactResource() resource.Resource { return &contactResource{} }

type contactResource struct{ client *client.Client }

type contactModel struct {
	ID          types.String `tfsdk:"id"`
	FirstName   types.String `tfsdk:"first_name"`
	LastName    types.String `tfsdk:"last_name"`
	NamePrefix  types.String `tfsdk:"name_prefix"`
	CompanyName types.String `tfsdk:"company_name"`
	Email       types.String `tfsdk:"email"`
	Phone       types.String `tfsdk:"phone"`
	Street      types.String `tfsdk:"street"`
	Number      types.String `tfsdk:"number"`
	Suffix      types.String `tfsdk:"suffix"`
	City        types.String `tfsdk:"city"`
	PostalCode  types.String `tfsdk:"postal_code"`
	State       types.String `tfsdk:"state"`
	CountryCode types.String `tfsdk:"country_code"`
	TaxID       types.String `tfsdk:"tax_id"`
}

// optString is null for "" and the string otherwise.
func optString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func (m contactModel) toAPI() client.Contact {
	return client.Contact{
		FirstName: m.FirstName.ValueString(), LastName: m.LastName.ValueString(),
		NamePrefix: m.NamePrefix.ValueString(), CompanyName: m.CompanyName.ValueString(),
		Email: m.Email.ValueString(), Phone: m.Phone.ValueString(),
		Street: m.Street.ValueString(), Number: m.Number.ValueString(),
		Suffix: m.Suffix.ValueString(), City: m.City.ValueString(),
		PostalCode: m.PostalCode.ValueString(), State: m.State.ValueString(),
		CountryCode: m.CountryCode.ValueString(), TaxID: m.TaxID.ValueString(),
	}
}

func contactFromAPI(c client.Contact) contactModel {
	return contactModel{
		ID: types.StringValue(c.ID), FirstName: types.StringValue(c.FirstName),
		LastName: types.StringValue(c.LastName), NamePrefix: optString(c.NamePrefix),
		CompanyName: optString(c.CompanyName), Email: types.StringValue(c.Email),
		Phone: optString(c.Phone), Street: optString(c.Street), Number: optString(c.Number),
		Suffix: optString(c.Suffix), City: optString(c.City),
		PostalCode: optString(c.PostalCode), State: optString(c.State),
		CountryCode: types.StringValue(c.CountryCode), TaxID: optString(c.TaxID),
	}
}

func (r *contactResource) Metadata(
	_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (r *contactResource) Schema(
	_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse,
) {
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	optional := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: desc, Validators: nonEmpty}
	}
	resp.Schema = schema.Schema{
		Description: "A RegAPI contact. A contact's name, prefix and company cannot be " +
			"changed once it exists; changing them replaces the contact.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"first_name": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Validators: nonEmpty, Description: "First name. Changing it replaces the contact."},
			"last_name": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Validators: nonEmpty, Description: "Last name. Changing it replaces the contact."},
			"name_prefix": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				Validators:  nonEmpty,
				Description: "Name prefix, for example \"van der\". Changing it replaces the contact."},
			"company_name": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				Validators: nonEmpty,
				Description: "Company name with its legal form. Changing it replaces the " +
					"contact."},
			"email": schema.StringAttribute{Required: true, Validators: nonEmpty,
				Description: "Email address."},
			"phone":       optional("Phone number as +CC.NUMBER, for example +30.2100000000."),
			"street":      optional("Street name without the number."),
			"number":      optional("House number."),
			"suffix":      optional("House number suffix, for example A."),
			"city":        optional("City."),
			"postal_code": optional("Postal code."),
			"state":       optional("Region, required for Greek addresses."),
			"country_code": schema.StringAttribute{Required: true,
				Validators: []validator.String{stringvalidator.RegexMatches(
					regexp.MustCompile(`^[A-Z]{2}$`), "must be a two-letter upper-case country code")},
				Description: "ISO 3166 country code in upper case, for example GR."},
			"tax_id": optional("Tax ID, for example the Greek ΑΦΜ."),
		},
	}
}

func (r *contactResource) Configure(
	_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func (r *contactResource) Create(
	ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse,
) {
	var plan contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateContact(ctx, plan.toAPI())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the contact", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, contactFromAPI(created))...)
}

func (r *contactResource) Read(
	ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse,
) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetContact(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the contact", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, contactFromAPI(c))...)
}

func (r *contactResource) Update(
	ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse,
) {
	var plan, state contactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateContact(ctx, state.ID.ValueString(), plan.toAPI())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the contact", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, contactFromAPI(updated))...)
}

func (r *contactResource) Delete(
	ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse,
) {
	var state contactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteContact(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Could not delete the contact", err)
	}
}

func (r *contactResource) ImportState(
	ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *contactResource) ModifyPlan(
	ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse,
) {
	state, plan, diags := planModels[contactModel](ctx, req)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.Diagnostics.Append(contactWarnings(state, plan)...)
	}
}

// contactWarnings are the plan warnings for replacing or destroying a contact.
func contactWarnings(state, plan *contactModel) []diag.Diagnostic {
	switch {
	case state == nil:
		return nil
	case plan == nil:
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Deleting a RegAPI contact",
			"The contact is removed from RegAPI. The delete fails with contact_in_use while a "+
				"domain, an unfinished order or an account default still uses it.")}
	}
	var changed []string
	for _, a := range []struct {
		name      string
		old, next types.String
	}{{"first_name", state.FirstName, plan.FirstName}, {"last_name", state.LastName, plan.LastName},
		{"name_prefix", state.NamePrefix, plan.NamePrefix},
		{"company_name", state.CompanyName, plan.CompanyName}} {
		if !a.old.Equal(a.next) {
			changed = append(changed, a.name)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return []diag.Diagnostic{diag.NewWarningDiagnostic("Replacing a RegAPI contact",
		"Changing "+strings.Join(changed, ", ")+" creates a new contact and deletes this one. "+
			"Domains that use it must be moved to the new contact first, or the delete fails "+
			"with contact_in_use.")}
}
