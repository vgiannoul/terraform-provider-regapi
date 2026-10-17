package provider

import (
	"context"

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
	_ resource.Resource                = (*defaultsResource)(nil)
	_ resource.ResourceWithConfigure   = (*defaultsResource)(nil)
	_ resource.ResourceWithImportState = (*defaultsResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*defaultsResource)(nil)
)

// NewAccountDefaultsResource returns the regapi_account_defaults resource.
func NewAccountDefaultsResource() resource.Resource { return &defaultsResource{} }

type defaultsResource struct{ client *client.Client }

type defaultsModel struct {
	ID                  types.String `tfsdk:"id"`
	RegistrantContactID types.String `tfsdk:"registrant_contact_id"`
	AdminContactID      types.String `tfsdk:"admin_contact_id"`
	TechContactID       types.String `tfsdk:"tech_contact_id"`
	BillingContactID    types.String `tfsdk:"billing_contact_id"`
}

func defaultsFromAPI(a client.Account) defaultsModel {
	return defaultsModel{ID: types.StringValue(a.ID),
		RegistrantContactID: optString(a.DefaultContactID),
		AdminContactID:      optString(a.DefaultAdminContactID),
		TechContactID:       optString(a.DefaultTechContactID),
		BillingContactID:    optString(a.DefaultBillingContactID)}
}

// patchFor builds the API patch that makes the account match plan. A role removed from the
// configuration is cleared; state is nil when the resource is being created.
func patchFor(plan defaultsModel, state *defaultsModel) client.DefaultsPatch {
	prior := defaultsModel{}
	if state != nil {
		prior = *state
	}
	role := func(want, have types.String) *string {
		switch {
		case !want.IsNull():
			v := want.ValueString()
			return &v
		case !have.IsNull():
			empty := ""
			return &empty
		}
		return nil
	}
	registrant := plan.RegistrantContactID.ValueString()
	return client.DefaultsPatch{
		Registrant: &registrant,
		Admin:      role(plan.AdminContactID, prior.AdminContactID),
		Tech:       role(plan.TechContactID, prior.TechContactID),
		Billing:    role(plan.BillingContactID, prior.BillingContactID),
	}
}

func (r *defaultsResource) Metadata(
	_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_account_defaults"
}

func (r *defaultsResource) Schema(
	_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse,
) {
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	role := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Validators: nonEmpty, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "The account's default contacts. Orders that name no contact for a role " +
			"use these. Destroying the resource clears the administrative, technical and " +
			"billing defaults; the default registrant stays because the API cannot clear it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"registrant_contact_id": schema.StringAttribute{Required: true, Validators: nonEmpty,
				Description: "Contact ID used as registrant when an order names none."},
			"admin_contact_id":   role("Contact ID used as administrative contact."),
			"tech_contact_id":    role("Contact ID used as technical contact."),
			"billing_contact_id": role("Contact ID used as billing contact."),
		},
	}
}

func (r *defaultsResource) Configure(
	_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func (r *defaultsResource) save(
	ctx context.Context, plan defaultsModel, state *defaultsModel, set func(any) diag.Diagnostics,
	diags *diag.Diagnostics,
) {
	account, err := r.client.SetDefaults(ctx, patchFor(plan, state))
	if err != nil {
		addAPIError(diags, "Could not set the default contacts", err)
		return
	}
	diags.Append(set(defaultsFromAPI(account))...)
}

func (r *defaultsResource) Create(
	ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse,
) {
	var plan defaultsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.save(ctx, plan, nil, func(v any) diag.Diagnostics { return resp.State.Set(ctx, v) },
		&resp.Diagnostics)
}

func (r *defaultsResource) Update(
	ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse,
) {
	var plan, state defaultsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.save(ctx, plan, &state, func(v any) diag.Diagnostics { return resp.State.Set(ctx, v) },
		&resp.Diagnostics)
}

func (r *defaultsResource) Read(
	ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse,
) {
	account, err := r.client.GetAccount(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the account", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, defaultsFromAPI(account))...)
}

func (r *defaultsResource) Delete(
	ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse,
) {
	empty := ""
	_, err := r.client.SetDefaults(ctx, client.DefaultsPatch{Admin: &empty, Tech: &empty,
		Billing: &empty})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not clear the default contacts", err)
	}
}

func (r *defaultsResource) ImportState(
	ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *defaultsResource) ModifyPlan(
	ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse,
) {
	state, plan, diags := planModels[defaultsModel](ctx, req)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.Diagnostics.Append(defaultsWarnings(state, plan)...)
	}
}

// defaultsWarnings is the plan warning for destroying the defaults.
func defaultsWarnings(state, plan *defaultsModel) []diag.Diagnostic {
	if state == nil || plan != nil {
		return nil
	}
	return []diag.Diagnostic{diag.NewWarningDiagnostic("Clearing the default contacts",
		"The default administrative, technical and billing contacts are cleared. The default "+
			"registrant stays, because the API cannot clear it.")}
}
