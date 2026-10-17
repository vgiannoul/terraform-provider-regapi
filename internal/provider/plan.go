package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// planModels reads the prior state and the planned values of a resource; either is nil when
// the plan creates (no state) or destroys (no plan) the resource.
func planModels[T any](
	ctx context.Context, req resource.ModifyPlanRequest,
) (state, plan *T, diags diag.Diagnostics) {
	if !req.State.Raw.IsNull() {
		state = new(T)
		diags.Append(req.State.Get(ctx, state)...)
	}
	if !req.Plan.Raw.IsNull() {
		plan = new(T)
		diags.Append(req.Plan.Get(ctx, plan)...)
	}
	return state, plan, diags
}
