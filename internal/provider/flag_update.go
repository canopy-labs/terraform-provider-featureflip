package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// flagUpdateStep is one API call Update makes to move a flag from state to plan.
type flagUpdateStep string

const (
	stepRestore        flagUpdateStep = "restore"
	stepPut            flagUpdateStep = "put"
	stepClearPermanent flagUpdateStep = "clear-permanent"
	stepClearExpiry    flagUpdateStep = "clear-expiry"
	stepSetExpiry      flagUpdateStep = "set-expiry"
	stepSetPermanent   flagUpdateStep = "set-permanent"
	stepClearOwner     flagUpdateStep = "clear-owner"
	stepSetOwner       flagUpdateStep = "set-owner"
	stepVariations     flagUpdateStep = "variations"
	stepArchive        flagUpdateStep = "archive"
)

// planFlagUpdate lists the calls that move a flag from state to plan, in the
// order Update makes them. Only fields that changed get a call: the API
// refuses the flag PUT on an archived flag, so a lifecycle-only change must
// not send it.
//
// Ordering:
//   - Restore comes first, so the edits after it land on an active flag.
//   - Archive comes last, for the same reason.
//   - The API refuses a permanent flag with an expiry, so permanent is dropped
//     before an expiry is set, and set only after the expiry is cleared.
func planFlagUpdate(ctx context.Context, plan, state featureFlagModel, diags *diag.Diagnostics) []flagUpdateStep {
	var steps []flagUpdateStep
	archivedNow, archivedPlanned := state.Archived.ValueBool(), plan.Archived.ValueBool()

	if archivedNow && known(plan.Archived) && !archivedPlanned {
		steps = append(steps, stepRestore)
	}
	if differs(plan.Name, state.Name) || differs(plan.Description, state.Description) ||
		differs(plan.Tags, state.Tags) || differs(plan.ClientSideVisible, state.ClientSideVisible) {
		steps = append(steps, stepPut)
	}
	if known(plan.Permanent) && state.Permanent.ValueBool() && !plan.Permanent.ValueBool() {
		steps = append(steps, stepClearPermanent)
	}
	// Only a real change calls the expiry endpoints, so an expiry that has
	// since passed never trips EXPIRY_IN_PAST on an unrelated update.
	if known(plan.ExpiresAt) && expiryChanged(plan.ExpiresAt, state.ExpiresAt) {
		if plan.ExpiresAt.IsNull() {
			steps = append(steps, stepClearExpiry)
		} else {
			steps = append(steps, stepSetExpiry)
		}
	}
	if known(plan.Permanent) && !state.Permanent.ValueBool() && plan.Permanent.ValueBool() {
		steps = append(steps, stepSetPermanent)
	}
	if known(plan.OwnerEmail) && ownerChanged(plan.OwnerEmail, state.OwnerEmail) {
		if plan.OwnerEmail.IsNull() || plan.OwnerEmail.ValueString() == "" {
			steps = append(steps, stepClearOwner)
		} else {
			steps = append(steps, stepSetOwner)
		}
	}
	if variationsChanged(ctx, plan.Variations, state.Variations, diags) {
		steps = append(steps, stepVariations)
	}
	if !archivedNow && archivedPlanned {
		steps = append(steps, stepArchive)
	}
	return steps
}

// archivedFlagRefusals names what the steps would change that the API refuses
// on an archived flag. Clearing the owner, permanent marker or expiry is
// always allowed; everything else needs the flag restored first.
func archivedFlagRefusals(steps []flagUpdateStep) []string {
	var refused []string
	for _, s := range steps {
		switch s {
		case stepPut:
			refused = append(refused, "name, description, tags or client_side_visible")
		case stepSetExpiry:
			refused = append(refused, "expires_at")
		case stepSetPermanent:
			refused = append(refused, "permanent")
		case stepSetOwner:
			refused = append(refused, "owner_email")
		case stepVariations:
			refused = append(refused, "variations")
		}
	}
	return refused
}

func known(v attr.Value) bool { return !v.IsUnknown() }

// differs reports a known planned value that differs from state. An unknown
// value is not a change yet: plan-time checks skip it, and at apply every
// value Update compares comes from config and is known.
func differs(plan, state attr.Value) bool {
	return known(plan) && !plan.Equal(state)
}

// variationsChanged compares variations by key, name, value and description.
// Ids are computed (unknown in a plan with changes) and the API does not keep
// order, so neither counts. A null or unknown plan (Boolean flags) is no change.
func variationsChanged(ctx context.Context, plan, state types.List, diags *diag.Diagnostics) bool {
	if plan.IsNull() || plan.IsUnknown() {
		return false
	}
	if state.IsNull() || state.IsUnknown() {
		return true
	}
	var pv, sv []variationModel
	diags.Append(plan.ElementsAs(ctx, &pv, false)...)
	diags.Append(state.ElementsAs(ctx, &sv, false)...)
	if diags.HasError() || len(pv) != len(sv) {
		return true
	}
	byKey := make(map[string]variationModel, len(sv))
	for _, v := range sv {
		byKey[v.Key.ValueString()] = v
	}
	for _, p := range pv {
		s, ok := byKey[p.Key.ValueString()]
		if !ok || !p.Name.Equal(s.Name) || !p.Value.Equal(s.Value) || !p.Description.Equal(s.Description) {
			return true
		}
	}
	return false
}
