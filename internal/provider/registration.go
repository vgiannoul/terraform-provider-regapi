package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
)

var (
	_ resource.Resource               = (*registrationResource)(nil)
	_ resource.ResourceWithConfigure  = (*registrationResource)(nil)
	_ resource.ResourceWithModifyPlan = (*registrationResource)(nil)
)

// now is the clock for the 24-hour window in which a completed order is reused.
var now = time.Now

const completedOrderWindow = 24 * time.Hour

var errAlreadyRegistered = errors.New("the domain is already registered in this account")

// NewRegistrationResource returns the regapi_domain_registration resource.
func NewRegistrationResource() resource.Resource { return &registrationResource{} }

type registrationResource struct{ client *client.Client }

type registrationModel struct {
	DomainName          types.String   `tfsdk:"domain_name"`
	Years               types.Int64    `tfsdk:"years"`
	Nameservers         types.List     `tfsdk:"nameservers"`
	AutoRenew           types.Bool     `tfsdk:"auto_renew"`
	RegistrantContactID types.String   `tfsdk:"registrant_contact_id"`
	AdminContactID      types.String   `tfsdk:"admin_contact_id"`
	TechContactID       types.String   `tfsdk:"tech_contact_id"`
	BillingContactID    types.String   `tfsdk:"billing_contact_id"`
	PaymentMethod       types.String   `tfsdk:"payment_method"`
	ExpectedPriceCents  types.Int64    `tfsdk:"expected_price_cents"`
	WaitForActive       types.Bool     `tfsdk:"wait_for_active"`
	OrderID             types.String   `tfsdk:"order_id"`
	OrderState          types.String   `tfsdk:"order_state"`
	AmountCents         types.Int64    `tfsdk:"amount_cents"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

func (r *registrationResource) Metadata(
	_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_domain_registration"
}

func (r *registrationResource) Schema(
	ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse,
) {
	keepStr := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	contact := func(role string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true,
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			Description: "Contact ID of the " + role + " contact. Left out: the account's " +
				"default for the role."}
	}
	resp.Schema = schema.Schema{
		Description: "The one-time act of registering a domain. If the order is not paid the " +
			"apply fails with the payment instructions; pay, then apply again. Afterwards " +
			"manage the domain with regapi_domain. Changes to this resource after " +
			"registration are not applied, and destroying it does not deregister the domain.",
		Attributes: map[string]schema.Attribute{
			"domain_name": schema.StringAttribute{Required: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The name to register, for example acme.gr."},
			"years": schema.Int64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{int64validator.Between(1, 10)},
				Description:   "Term in years. Left out: the TLD's minimum term."},
			"nameservers": schema.ListAttribute{Optional: true, ElementType: types.StringType,
				Validators:  []validator.List{listvalidator.SizeAtMost(13)},
				Description: "Initial nameservers. Left out: the registrar's defaults."},
			"auto_renew": schema.BoolAttribute{Optional: true,
				Description: "Initial auto-renew."},
			"registrant_contact_id": contact("registrant"),
			"admin_contact_id":      contact("administrative"),
			"tech_contact_id":       contact("technical"),
			"billing_contact_id":    contact("billing"),
			"payment_method": schema.StringAttribute{Optional: true,
				Validators:  []validator.String{stringvalidator.OneOf("saved_card", "checkout")},
				Description: "saved_card or checkout. Left out: the saved card if there is one."},
			"expected_price_cents": schema.Int64Attribute{Optional: true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
				Description: "The price guard: if the price differs, nothing is charged and " +
					"the apply fails with price_changed. Take it from regapi_domain_quote."},
			"wait_for_active": schema.BoolAttribute{Optional: true, Computed: true,
				Default:     booldefault.StaticBool(false),
				Description: "Wait until the domain is Active (see timeouts.create)."},
			"order_id": schema.StringAttribute{Computed: true, PlanModifiers: keepStr,
				Description: "The registration order."},
			"order_state": schema.StringAttribute{Computed: true, PlanModifiers: keepStr,
				Description: "State of the order when last read."},
			"amount_cents": schema.Int64Attribute{Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Description:   "What the order costs, in cents."},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *registrationResource) Configure(
	_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse,
) {
	if req.ProviderData != nil {
		r.client = req.ProviderData.(*client.Client)
	}
}

func (r *registrationResource) Create(
	ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse,
) {
	var plan registrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	timeout, diags := plan.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := strings.ToLower(strings.TrimSpace(plan.DomainName.ValueString()))
	order, err := r.obtainOrder(ctx, name, plan)
	switch {
	case errors.Is(err, errAlreadyRegistered):
		resp.Diagnostics.AddError("The domain is already registered", name+" is already "+
			"registered in this account. Manage it with regapi_domain, or import it there.")
		return
	case err != nil:
		addAPIError(&resp.Diagnostics, "Could not register the domain", err)
		return
	}
	if !orderUsable(order, &resp.Diagnostics) {
		return
	}
	if plan.WaitForActive.ValueBool() {
		if err := r.waitActive(ctx, order); err != nil {
			addAPIError(&resp.Diagnostics, "The domain did not become active", err)
			return
		}
	}
	plan.OrderID, plan.OrderState = types.StringValue(order.ID), types.StringValue(order.State)
	plan.AmountCents = types.Int64Value(order.AmountCents)
	plan.Years = types.Int64Value(int64(order.Years))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// obtainOrder returns the order for name with its payment details: an existing one, or a new
// one when the domain is not registered yet.
func (r *registrationResource) obtainOrder(
	ctx context.Context, name string, plan registrationModel,
) (client.Order, error) {
	order, found, err := r.findOrder(ctx, name)
	if err != nil {
		return client.Order{}, err
	}
	if !found {
		if _, err := r.client.GetDomain(ctx, name); err == nil {
			return client.Order{}, errAlreadyRegistered
		} else if !client.IsNotFound(err) {
			return client.Order{}, err
		}
		req, diags := registerRequest(ctx, name, plan)
		if diags.HasError() {
			return client.Order{}, fmt.Errorf("invalid registration settings: %s", diags[0].Detail())
		}
		if order, err = r.client.RegisterDomain(ctx, req); err != nil {
			return client.Order{}, err
		}
	}
	return r.client.GetOrder(ctx, order.ID)
}

// findOrder looks for the register order of name an earlier apply placed: unfinished, or
// completed within the last 24 hours (the apply repeated after paying). The list has no
// domain filter, so it matches the name itself.
func (r *registrationResource) findOrder(
	ctx context.Context, name string,
) (client.Order, bool, error) {
	var match client.Order
	found := false
	for _, state := range []string{"awaiting_payment", "provisioning", "completed"} {
		err := r.client.VisitOrders(ctx, state, func(o client.Order) bool {
			if state == "completed" && now().Sub(o.CreatedAt) > completedOrderWindow {
				return true
			}
			if o.Kind == "register" && strings.EqualFold(o.DomainName, name) {
				match, found = o, true
				return true
			}
			return false
		})
		if err != nil {
			return client.Order{}, false, err
		}
		if found {
			return match, true, nil
		}
	}
	return client.Order{}, false, nil
}

func registerRequest(
	ctx context.Context, name string, plan registrationModel,
) (client.RegisterRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	req := client.RegisterRequest{DomainName: name,
		Contacts: client.ContactIDs{Registrant: plan.RegistrantContactID.ValueString(),
			Admin: plan.AdminContactID.ValueString(), Tech: plan.TechContactID.ValueString(),
			Billing: plan.BillingContactID.ValueString()},
		AutoRenew: plan.AutoRenew.ValueBool(), PaymentMethod: plan.PaymentMethod.ValueString()}
	if setting(plan.Years) {
		req.Years = int(plan.Years.ValueInt64())
	}
	if setting(plan.Nameservers) {
		diags.Append(plan.Nameservers.ElementsAs(ctx, &req.Nameservers, false)...)
	}
	if setting(plan.ExpectedPriceCents) {
		v := plan.ExpectedPriceCents.ValueInt64()
		req.ExpectedPriceCents = &v
	}
	return req, diags
}

// orderUsable adds an error unless the order is paid for and provisioning or completed.
func orderUsable(order client.Order, diags *diag.Diagnostics) bool {
	switch order.State {
	case "failed":
		diags.AddError("The registration order failed", order.ErrorCode+": "+order.ErrorMessage)
		return false
	case "awaiting_payment":
		diags.AddError("The registration is waiting for payment", paymentInstructions(order)+
			"\n\nThe order is kept. Pay it, then run apply again.")
		return false
	}
	return true
}

func money(cents int64, currency string) string {
	return fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency)
}

// paymentInstructions says how to pay an unpaid order.
func paymentInstructions(order client.Order) string {
	amount := money(order.AmountCents, order.Currency)
	p := order.Payment
	switch {
	case p != nil && p.CheckoutURL != "":
		return fmt.Sprintf("Open %s and pay %s (order %s).", p.CheckoutURL, amount, order.ID)
	case p != nil && p.Instructions != nil:
		i := p.Instructions
		return fmt.Sprintf("Wire %s to %s, IBAN %s (BIC %s), with the reference %s.",
			money(i.AmountCents, i.Currency), i.Beneficiary, i.IBAN, i.BIC, i.Reference)
	}
	return fmt.Sprintf("Pay order %s (%s).", order.ID, amount)
}

// waitActive polls until the domain of the order is Active.
func (r *registrationResource) waitActive(ctx context.Context, order client.Order) error {
	for {
		done, err := r.checkActive(ctx, order)
		if done || err != nil {
			return err
		}
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s to become active: %w", order.DomainName,
				ctx.Err())
		}
	}
}

// checkActive reports whether the domain of the order is Active, or why it never will be.
func (r *registrationResource) checkActive(ctx context.Context, order client.Order) (bool, error) {
	current, err := r.client.GetOrder(ctx, order.ID)
	if err != nil {
		return false, err
	}
	if current.State == "failed" {
		return false, fmt.Errorf("the order failed: %s: %s", current.ErrorCode,
			current.ErrorMessage)
	}
	d, err := r.client.GetDomain(ctx, order.DomainName)
	switch {
	case client.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, err
	case d.Phase == "Failed" || d.Phase == "Released":
		return false, fmt.Errorf("the domain is %s", d.Phase)
	}
	return d.Phase == "Active", nil
}

func (r *registrationResource) Read(
	ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse,
) {
	var state registrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.OrderID.IsNull() {
		return
	}
	order, err := r.client.GetOrder(ctx, state.OrderID.ValueString())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the order", err)
		return
	}
	state.OrderState = types.StringValue(order.State)
	state.AmountCents = types.Int64Value(order.AmountCents)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update stores the new values in state only: a registration happens once.
func (r *registrationResource) Update(
	ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse,
) {
	var plan, state registrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.OrderID, plan.OrderState = state.OrderID, state.OrderState
	plan.AmountCents, plan.Years = state.AmountCents, state.Years
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the resource from state only; the domain stays registered.
func (r *registrationResource) Delete(
	context.Context, resource.DeleteRequest, *resource.DeleteResponse,
) {
}

func (r *registrationResource) ModifyPlan(
	ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse,
) {
	state, plan, diags := planModels[registrationModel](ctx, req)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.Diagnostics.Append(registrationWarnings(state, plan)...)
	}
}

// registrationWarnings are the plan warnings for destroying or changing a registration.
func registrationWarnings(state, plan *registrationModel) []diag.Diagnostic {
	switch {
	case state == nil:
		return nil
	case plan == nil:
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Removing a registration from "+
			"Terraform", "This removes the resource from Terraform state only. The domain stays "+
			"registered and keeps renewing.")}
	case !state.DomainName.Equal(plan.DomainName):
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Registering a second domain",
			"This registers a second domain and places a new order. The old domain stays "+
				"registered and is charged for.")}
	case registrationSettingsChanged(state, plan):
		return []diag.Diagnostic{diag.NewWarningDiagnostic("Changes are not applied",
			"Changes after registration are not applied; use regapi_domain to change the "+
				"domain's settings.")}
	}
	return nil
}

func registrationSettingsChanged(state, plan *registrationModel) bool {
	for _, pair := range [][2]attr.Value{
		{state.Years, plan.Years}, {state.Nameservers, plan.Nameservers},
		{state.AutoRenew, plan.AutoRenew},
		{state.RegistrantContactID, plan.RegistrantContactID},
		{state.AdminContactID, plan.AdminContactID}, {state.TechContactID, plan.TechContactID},
		{state.BillingContactID, plan.BillingContactID},
		{state.PaymentMethod, plan.PaymentMethod},
		{state.ExpectedPriceCents, plan.ExpectedPriceCents},
		{state.WaitForActive, plan.WaitForActive},
	} {
		if !pair[0].Equal(pair[1]) {
			return true
		}
	}
	return false
}
