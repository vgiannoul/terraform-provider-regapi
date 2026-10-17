package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

var (
	_ resource.Resource                = (*domainResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainResource)(nil)
	_ resource.ResourceWithImportState = (*domainResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*domainResource)(nil)
)

// pollInterval is how often the provider re-reads something it waits for.
var pollInterval = 5 * time.Second

// NewDomainResource returns the regapi_domain resource.
func NewDomainResource() resource.Resource { return &domainResource{} }

type domainResource struct{ client *client.Client }

type domainModel struct {
	Name                types.String   `tfsdk:"name"`
	Nameservers         types.List     `tfsdk:"nameservers"`
	AutoRenew           types.Bool     `tfsdk:"auto_renew"`
	RegistrantContactID types.String   `tfsdk:"registrant_contact_id"`
	AdminContactID      types.String   `tfsdk:"admin_contact_id"`
	TechContactID       types.String   `tfsdk:"tech_contact_id"`
	BillingContactID    types.String   `tfsdk:"billing_contact_id"`
	Phase               types.String   `tfsdk:"phase"`
	ExpiryDate          types.String   `tfsdk:"expiry_date"`
	NameserversSource   types.String   `tfsdk:"nameservers_source"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

func ptrString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// domainFromAPI maps a domain; the caller sets Timeouts.
func domainFromAPI(d client.Domain) domainModel {
	ns, _ := types.ListValueFrom(context.Background(), types.StringType, d.Nameservers)
	return domainModel{
		Name: types.StringValue(d.DomainName), Nameservers: ns,
		AutoRenew:           types.BoolValue(d.AutoRenew),
		RegistrantContactID: ptrString(d.Contacts.Registrant),
		AdminContactID:      ptrString(d.Contacts.Admin),
		TechContactID:       ptrString(d.Contacts.Tech),
		BillingContactID:    ptrString(d.Contacts.Billing),
		Phase:               types.StringValue(d.Phase), ExpiryDate: optString(d.ExpiryDate),
		NameserversSource: types.StringValue(d.NameserversSource),
	}
}

func setting(v attr.Value) bool { return !v.IsNull() && !v.IsUnknown() }

// patchFrom builds the patch that makes the domain match plan; fields equal to state's
// (state is nil on adoption) are left out, so an unchanged resource sends nothing.
func patchFrom(
	ctx context.Context, plan domainModel, state *domainModel,
) (client.DomainPatch, diag.Diagnostics) {
	var patch client.DomainPatch
	var diags diag.Diagnostics
	prior := domainModel{}
	if state != nil {
		prior = *state
	}
	if setting(plan.Nameservers) && !plan.Nameservers.Equal(prior.Nameservers) {
		var ns []string
		diags.Append(plan.Nameservers.ElementsAs(ctx, &ns, false)...)
		patch.Nameservers = &ns
	}
	if setting(plan.AutoRenew) && !plan.AutoRenew.Equal(prior.AutoRenew) {
		v := plan.AutoRenew.ValueBool()
		patch.AutoRenew = &v
	}
	if contacts := contactsPatchFrom(plan, prior); contacts != (client.ContactsPatch{}) {
		patch.Contacts = &contacts
	}
	return patch, diags
}

// contactsPatchFrom lists the contact roles whose planned value differs from prior's.
func contactsPatchFrom(plan, prior domainModel) client.ContactsPatch {
	var contacts client.ContactsPatch
	for _, role := range []struct {
		plan, prior types.String
		dst         **string
	}{
		{plan.RegistrantContactID, prior.RegistrantContactID, &contacts.Registrant},
		{plan.AdminContactID, prior.AdminContactID, &contacts.Admin},
		{plan.TechContactID, prior.TechContactID, &contacts.Tech},
		{plan.BillingContactID, prior.BillingContactID, &contacts.Billing},
	} {
		if setting(role.plan) && !role.plan.Equal(role.prior) {
			v := role.plan.ValueString()
			*role.dst = &v
		}
	}
	return contacts
}

// waitForDomain polls the domain until ok reports true, the domain fails, or ctx ends.
func waitForDomain(
	ctx context.Context, c *client.Client, name string, ok func(client.Domain) bool,
) (client.Domain, error) {
	for {
		d, err := c.GetDomain(ctx, name)
		switch {
		case err != nil:
			return client.Domain{}, err
		case d.Phase == "Failed" || d.Phase == "Released":
			return d, fmt.Errorf("the domain %s is %s", name, d.Phase)
		case ok(d):
			return d, nil
		}
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			return client.Domain{}, fmt.Errorf("timed out waiting for %s to show the requested "+
				"changes (the API applies changes asynchronously): %w", name, ctx.Err())
		}
	}
}

func (r *domainResource) Metadata(
	_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(
	ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse,
) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	contact := func(role string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep,
			Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			Description: "Contact ID of the " + role + " contact. Left out: left as it is."}
	}
	resp.Schema = schema.Schema{
		Description: "Settings of a domain that is already registered in the account. Creating " +
			"the resource adopts the domain; destroying it only removes it from state.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The domain name, for example acme.gr."},
			"nameservers": schema.ListAttribute{Optional: true, Computed: true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				Validators:    []validator.List{listvalidator.SizeAtMost(13)},
				Description:   "Nameservers. Left out: left as they are."},
			"auto_renew": schema.BoolAttribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
				Description:   "Whether RegAPI renews the domain before it expires."},
			"registrant_contact_id": contact("registrant"),
			"admin_contact_id":      contact("administrative"),
			"tech_contact_id":       contact("technical"),
			"billing_contact_id":    contact("billing"),
			"phase": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				Description: "Lifecycle phase, for example Active."},
			"expiry_date": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				Description: "Expiry date and time."},
			"nameservers_source": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				Description: "default when the registrar's nameservers are used, else custom."},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *domainResource) Configure(
	_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

// apply patches the domain towards plan, waits until the API shows it, and stores the result.
func (r *domainResource) apply(
	ctx context.Context, plan domainModel, state *domainModel, st *tfsdk.State,
	diags *diag.Diagnostics,
) {
	patch, d := patchFrom(ctx, plan, state)
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	name := plan.Name.ValueString()
	if !patch.Empty() {
		if err := r.client.UpdateDomain(ctx, name, patch); err != nil {
			addAPIError(diags, "Could not update the domain", err)
			return
		}
	}
	got, err := waitForDomain(ctx, r.client, name, patch.Matches)
	if err != nil {
		addAPIError(diags, "Could not confirm the domain's settings", err)
		return
	}
	model := domainFromAPI(got)
	model.Timeouts = plan.Timeouts
	diags.Append(st.Set(ctx, model)...)
}

func (r *domainResource) Create(
	ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse,
) {
	var plan domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	name := plan.Name.ValueString()
	_, err := r.client.GetDomain(ctx, name)
	if client.IsNotFound(err) {
		resp.Diagnostics.AddError("The domain is not registered in this account",
			name+" is not registered in this account. Register it with "+
				"regapi_domain_registration first.")
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	r.apply(ctx, plan, nil, &resp.State, &resp.Diagnostics)
}

func (r *domainResource) Update(
	ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse,
) {
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r.apply(ctx, plan, &state, &resp.State, &resp.Diagnostics)
}

func (r *domainResource) Read(
	ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse,
) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.GetDomain(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	model := domainFromAPI(d)
	model.Timeouts = state.Timeouts
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

// Delete removes the resource from state only; the domain stays registered.
func (r *domainResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *domainResource) ImportState(
	ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *domainResource) ModifyPlan(
	ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse,
) {
	state, plan, diags := planModels[domainModel](ctx, req)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.Diagnostics.Append(domainWarnings(state, plan)...)
	}
}

// domainWarnings are the plan warnings for destroying or replacing a regapi_domain.
func domainWarnings(state, plan *domainModel) []diag.Diagnostic {
	switch {
	case state == nil:
		return nil
	case plan == nil:
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Removing a domain from Terraform",
			"This removes the resource from Terraform state only. The domain stays registered "+
				"and keeps renewing.")}
	case !state.Name.Equal(plan.Name):
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Replacing a domain resource",
			"The old domain is not changed. The new name must already be registered in this "+
				"account.")}
	}
	return nil
}

// domainDataModel is the data source's view of a domain.
type domainDataModel struct {
	Name                types.String `tfsdk:"name"`
	Nameservers         types.List   `tfsdk:"nameservers"`
	AutoRenew           types.Bool   `tfsdk:"auto_renew"`
	RegistrantContactID types.String `tfsdk:"registrant_contact_id"`
	AdminContactID      types.String `tfsdk:"admin_contact_id"`
	TechContactID       types.String `tfsdk:"tech_contact_id"`
	BillingContactID    types.String `tfsdk:"billing_contact_id"`
	Phase               types.String `tfsdk:"phase"`
	ExpiryDate          types.String `tfsdk:"expiry_date"`
	NameserversSource   types.String `tfsdk:"nameservers_source"`
}
