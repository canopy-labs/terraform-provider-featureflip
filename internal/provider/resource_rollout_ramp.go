package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type rolloutRampResource struct{ resourceWithClient }

var (
	_ resource.Resource                   = (*rolloutRampResource)(nil)
	_ resource.ResourceWithConfigure      = (*rolloutRampResource)(nil)
	_ resource.ResourceWithImportState    = (*rolloutRampResource)(nil)
	_ resource.ResourceWithValidateConfig = (*rolloutRampResource)(nil)
)

func newRolloutRampResource() resource.Resource { return &rolloutRampResource{} }

// Hold limits the API enforces on every step but the last.
const (
	minHoldMinutes = 5
	maxHoldMinutes = 43200
)

type rolloutRampModel struct {
	ID                types.String `tfsdk:"id"`
	Project           types.String `tfsdk:"project"`
	Flag              types.String `tfsdk:"flag"`
	Environment       types.String `tfsdk:"environment"`
	RuleID            types.String `tfsdk:"rule_id"`
	DefaultRule       types.Bool   `tfsdk:"default_rule"`
	Steps             types.List   `tfsdk:"steps"`
	Variation         types.String `tfsdk:"variation"`
	BucketBy          types.String `tfsdk:"bucket_by"`
	AbortPercent      types.Int64  `tfsdk:"abort_percent"`
	Status            types.String `tfsdk:"status"`
	CurrentStepIndex  types.Int64  `tfsdk:"current_step_index"`
	NextStepAt        types.String `tfsdk:"next_step_at"`
	PausedReason      types.String `tfsdk:"paused_reason"`
	PausedDetail      types.String `tfsdk:"paused_detail"`
	TargetVariationID types.String `tfsdk:"target_variation_id"`
}

type rampStepModel struct {
	Percent     types.Int64 `tfsdk:"percent"`
	HoldMinutes types.Int64 `tfsdk:"hold_minutes"`
}

var rampStepAttrTypes = map[string]attr.Type{
	"percent":      types.Int64Type,
	"hold_minutes": types.Int64Type,
}

func (r *rolloutRampResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rollout_ramp"
}

// startOnly replaces the ramp only when a start-only input is set to a new
// value. Adding one to an imported ramp (the API does not report bucket_by) or
// removing one adopts the config in place: neither changes what the running
// ramp does, so neither should abort it.
func startOnly() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && !req.ConfigValue.IsNull() && !req.PlanValue.Equal(req.StateValue)
		},
		"Changing this on a started ramp aborts it and starts a new one.",
		"Changing this on a started ramp aborts it and starts a new one.",
	)
}

// ruleReplace replaces the ramp when rule_id names a different rule, but not
// when it is merely unknown: featureflip_flag_environment plans every rule id
// unknown on any change to it, though an edited rule keeps its id. Update then
// reads the real id back. A rule that really was replaced took the ramp with it.
func ruleReplace() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.PlanValue.IsUnknown() && !req.PlanValue.Equal(req.StateValue)
		},
		"Pointing the ramp at another rule aborts it and starts a new one there.",
		"Pointing the ramp at another rule aborts it and starts a new one there.",
	)
}

func (r *rolloutRampResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A rollout ramp: raises a percentage rollout on one targeting rule, or on the default rule, one step at " +
			"a time on a timer. Creating it starts the ramp, which applies the first step at once and each next step when " +
			"the previous hold runs out. The flag must be on in the environment. Needs the Team plan or above.\n\n" +
			"A ramp is not edited: changing `rule_id`, `default_rule`, `steps`, `variation` or `bucket_by` aborts it and " +
			"starts a new one, whose first step must not be below the rule's current rollout. Refreshing reads its " +
			"progress (`status`, `current_step_index`, `next_step_at`, `paused_reason`); a finished ramp stays in state " +
			"and is not restarted. Destroying it aborts a ramp that is still running or paused and leaves the rule at its " +
			"current percentage, or at `abort_percent`; destroying a finished one changes nothing.\n\n" +
			"The ramp changes the rule as it goes. If `featureflip_flag_environment` manages the same rule, add its " +
			"`rollout_percentage` to that resource's `lifecycle.ignore_changes` (for a default-rule ramp, `strategy` and " +
			"`default_variation`), or the next apply undoes the ramp's progress, and editing the rule by hand pauses the " +
			"ramp. Deleting the rule ends the ramp, and the next plan starts a new one. `featureflip_flag_environment` " +
			"writes only the rules whose configuration changed, so editing another rule leaves the ramp running; " +
			"changing the ramped rule itself pauses it, as a hand edit does. Resume it from the app or the MCP " +
			"server's manage_rollout_ramp.\n\n" +
			"Import with `<project>/<flag>/<environment>/<ramp-id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The ramp id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"flag": schema.StringAttribute{
				Required:      true,
				Description:   "Flag key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"environment": schema.StringAttribute{
				Required:      true,
				Description:   "Environment key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"rule_id": schema.StringAttribute{
				Optional: true,
				Description: "Id of the targeting rule to ramp, e.g. `featureflip_flag_environment.x.rules[0].id`. The rule " +
					"must serve a percentage rollout. Set exactly one of `rule_id` and `default_rule`.",
				PlanModifiers: []planmodifier.String{ruleReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"default_rule": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				Description:   "Ramp the default rule instead of a targeting rule. On reaching 100% the ramp makes its variation the default.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"steps": schema.ListNestedAttribute{
				Required: true,
				Description: "The ladder, 1 to 20 steps. Percentages must rise, the first not below the rule's current " +
					"rollout. Every hold but the last is 5 minutes to 30 days (43200 minutes).",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				Validators:    []validator.List{listvalidator.SizeBetween(1, 20)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"percent": schema.Int64Attribute{
							Required:    true,
							Description: "Rollout percentage for this step, 1-100.",
							Validators:  []validator.Int64{int64validator.Between(1, 100)},
						},
						"hold_minutes": schema.Int64Attribute{
							Required:    true,
							Description: "Minutes to hold this step before the next. Not used on the last step; set it to 0 there.",
							Validators:  []validator.Int64{int64validator.Between(0, maxHoldMinutes)},
						},
					},
				},
			},
			"variation": schema.StringAttribute{
				Optional: true,
				Description: "Default-rule ramps only: the variation KEY to ramp to. Needed when the default rule has no " +
					"rollout yet; otherwise the existing rollout's variation is used.",
				PlanModifiers: []planmodifier.String{startOnly()},
			},
			"bucket_by": schema.StringAttribute{
				Optional:      true,
				Description:   "Default-rule ramps only: the context attribute to bucket by when setting up a new rollout.",
				PlanModifiers: []planmodifier.String{startOnly()},
			},
			"abort_percent": schema.Int64Attribute{
				Optional: true,
				Description: "Percentage (0-100) to leave the rule at when this resource is destroyed while the ramp is still " +
					"running or paused. Must not be above the current rollout. Omit to keep the current percentage. " +
					"Changing it does not touch the ramp. It also applies when a change to the ramp replaces it, before the " +
					"new ramp starts: with `abort_percent = 0`, editing `steps` drops the rule to 0% first.",
				Validators: []validator.Int64{int64validator.Between(0, 100)},
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Running, Paused, Completed or Aborted.",
			},
			"current_step_index": schema.Int64Attribute{
				Computed:    true,
				Description: "Index in `steps` of the last step applied; -1 before the first.",
			},
			"next_step_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the next step applies (RFC 3339), while the ramp is running.",
			},
			"paused_reason": schema.StringAttribute{
				Computed:    true,
				Description: "Why the ramp paused, e.g. the rule was edited by hand or the flag was turned off.",
			},
			"paused_detail": schema.StringAttribute{Computed: true},
			"target_variation_id": schema.StringAttribute{
				Computed:    true,
				Description: "Default-rule ramps: id of the variation being ramped to.",
			},
		},
	}
}

func (r *rolloutRampResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg rolloutRampModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ruleSet := !cfg.RuleID.IsNull()
	defaultSet := cfg.DefaultRule.ValueBool()
	if !cfg.RuleID.IsUnknown() && !cfg.DefaultRule.IsUnknown() && ruleSet == defaultSet {
		resp.Diagnostics.AddAttributeError(path.Root("rule_id"), "Set exactly one of rule_id and default_rule",
			"A ramp runs on one targeting rule (rule_id) or on the default rule (default_rule = true).")
	}
	if ruleSet && (!cfg.Variation.IsNull() || !cfg.BucketBy.IsNull()) {
		resp.Diagnostics.AddAttributeError(path.Root("variation"), "variation and bucket_by apply only to default-rule ramps",
			"A targeting-rule ramp keeps the rule's own variation and bucketing.")
	}
	if cfg.Steps.IsUnknown() || cfg.Steps.IsNull() {
		return
	}
	for _, e := range cfg.Steps.Elements() {
		if e.IsUnknown() {
			return
		}
	}
	var steps []rampStepModel
	resp.Diagnostics.Append(cfg.Steps.ElementsAs(ctx, &steps, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for i, s := range steps {
		if s.Percent.IsUnknown() || s.HoldMinutes.IsUnknown() {
			continue
		}
		if i > 0 && !steps[i-1].Percent.IsUnknown() && s.Percent.ValueInt64() <= steps[i-1].Percent.ValueInt64() {
			resp.Diagnostics.AddAttributeError(path.Root("steps").AtListIndex(i).AtName("percent"), "Ramp steps must rise",
				fmt.Sprintf("Step %d is %d%%, not above the previous step's %d%%.", i, s.Percent.ValueInt64(), steps[i-1].Percent.ValueInt64()))
		}
		if i < len(steps)-1 && s.HoldMinutes.ValueInt64() < minHoldMinutes {
			resp.Diagnostics.AddAttributeError(path.Root("steps").AtListIndex(i).AtName("hold_minutes"), "Hold too short",
				fmt.Sprintf("Every hold but the last must be %d to %d minutes; step %d holds %d.", minHoldMinutes, maxHoldMinutes, i, s.HoldMinutes.ValueInt64()))
		}
	}
}

// rampGateHint explains the two refusals a correct request can still get.
func rampGateHint(err error) string {
	switch {
	case client.HasCode(err, "PLAN_FEATURE_UNAVAILABLE"):
		return "\n\nStarting a rollout ramp needs the Team plan or above."
	case client.HasCode(err, "FEATURE_NOT_ENABLED"):
		return "\n\nRollout ramps are not enabled for this organization yet."
	}
	return ""
}

func (r *rolloutRampResource) variationIDs(ctx context.Context, project, flag string) (byKey, byID map[string]string, err error) {
	f, err := r.client.GetFlag(ctx, project, flag)
	if err != nil {
		return nil, nil, err
	}
	byKey, byID = make(map[string]string, len(f.Variations)), make(map[string]string, len(f.Variations))
	for _, v := range f.Variations {
		byKey[v.Key], byID[v.ID] = v.ID, v.Key
	}
	return byKey, byID, nil
}

// applyRamp copies what the API reports into m. Steps, rule and default_rule
// are only filled when absent (import): otherwise they came from config, and
// the API echoes them unchanged.
func applyRamp(m *rolloutRampModel, ramp *client.RolloutRamp, diags *diag.Diagnostics) {
	m.ID = types.StringValue(ramp.ID)
	m.Status = types.StringValue(ramp.Status)
	m.CurrentStepIndex = types.Int64Value(ramp.CurrentStepIndex)
	m.NextStepAt = types.StringPointerValue(ramp.NextStepAt)
	m.PausedReason = types.StringPointerValue(ramp.PausedReason)
	m.PausedDetail = types.StringPointerValue(ramp.PausedDetail)
	m.TargetVariationID = types.StringPointerValue(ramp.TargetVariationID)
	if m.Steps.IsNull() || m.Steps.IsUnknown() {
		elems := make([]attr.Value, len(ramp.Steps))
		for i, s := range ramp.Steps {
			elems[i] = types.ObjectValueMust(rampStepAttrTypes, map[string]attr.Value{
				"percent": types.Int64Value(s.Percent), "hold_minutes": types.Int64Value(s.HoldMinutes),
			})
		}
		list, d := types.ListValue(types.ObjectType{AttrTypes: rampStepAttrTypes}, elems)
		diags.Append(d...)
		m.Steps = list
		m.DefaultRule = types.BoolValue(ramp.IsDefaultRule)
		if !ramp.IsDefaultRule {
			m.RuleID = types.StringPointerValue(ramp.RuleID)
		}
	}
}

func (r *rolloutRampResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rolloutRampModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env := plan.Project.ValueString(), plan.Flag.ValueString(), plan.Environment.ValueString()
	var steps []rampStepModel
	resp.Diagnostics.Append(plan.Steps.ElementsAs(ctx, &steps, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ladder := make([]client.RampStep, len(steps))
	for i, s := range steps {
		ladder[i] = client.RampStep{Percent: s.Percent.ValueInt64(), HoldMinutes: s.HoldMinutes.ValueInt64()}
	}

	var ramp *client.RolloutRamp
	var err error
	if plan.DefaultRule.ValueBool() {
		in := client.StartDefaultRuleRampRequest{Steps: ladder, BucketBy: plan.BucketBy.ValueStringPointer()}
		if !plan.Variation.IsNull() {
			byKey, _, verr := r.variationIDs(ctx, project, flag)
			if verr != nil {
				resp.Diagnostics.AddError(fmt.Sprintf("Reading flag %q for featureflip_rollout_ramp failed", flag), verr.Error())
				return
			}
			id, ok := byKey[plan.Variation.ValueString()]
			if !ok {
				resp.Diagnostics.AddAttributeError(path.Root("variation"), "Unknown variation key",
					fmt.Sprintf("Flag %q has no variation %q.", flag, plan.Variation.ValueString()))
				return
			}
			in.VariationID = &id
		}
		ramp, err = r.client.StartDefaultRuleRamp(ctx, project, flag, env, in)
	} else {
		ramp, err = r.client.StartRuleRamp(ctx, project, flag, env, plan.RuleID.ValueString(), client.StartRuleRampRequest{Steps: ladder})
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Starting featureflip_rollout_ramp on flag %q in %q failed", flag, env), err.Error()+rampGateHint(err))
		return
	}
	applyRamp(&plan, ramp, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *rolloutRampResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state rolloutRampModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag := state.Project.ValueString(), state.Flag.ValueString()
	ramp, err := r.client.GetRolloutRamp(ctx, project, flag, state.Environment.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		// The rule, environment or flag was deleted, and the ramp with it.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_rollout_ramp %q failed", state.ID.ValueString()), err.Error())
		return
	}
	importing := state.Steps.IsNull()
	applyRamp(&state, ramp, &resp.Diagnostics)
	if importing && ramp.IsDefaultRule && ramp.TargetVariationID != nil {
		_, byID, verr := r.variationIDs(ctx, project, flag)
		if verr != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Reading flag %q for featureflip_rollout_ramp failed", flag), verr.Error())
			return
		}
		if key, ok := byID[*ramp.TargetVariationID]; ok {
			state.Variation = types.StringValue(key)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only ever changes abort_percent, or adopts variation/bucket_by after
// an import: everything else replaces. It refreshes progress and touches nothing.
func (r *rolloutRampResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state rolloutRampModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	ramp, err := r.client.GetRolloutRamp(ctx, plan.Project.ValueString(), plan.Flag.ValueString(), plan.Environment.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("featureflip_rollout_ramp %q no longer exists", state.ID.ValueString()),
			"The ramp ended with its rule, environment or flag. Run terraform apply again to start a new one.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_rollout_ramp %q failed", state.ID.ValueString()), err.Error())
		return
	}
	applyRamp(&plan, ramp, &resp.Diagnostics)
	if plan.RuleID.IsUnknown() {
		// Planned unknown because featureflip_flag_environment changed; the ramp's rule is the answer.
		plan.RuleID = types.StringPointerValue(ramp.RuleID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *rolloutRampResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state rolloutRampModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env, id := state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString(), state.ID.ValueString()
	ramp, err := r.client.GetRolloutRamp(ctx, project, flag, env, id)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_rollout_ramp %q before destroy failed", id), err.Error())
		return
	}
	if ramp.Finished() {
		return
	}

	percent, err := r.abortPercent(ctx, state, ramp)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Aborting featureflip_rollout_ramp %q failed", id), err.Error())
		return
	}
	_, err = r.client.AbortRolloutRamp(ctx, project, flag, env, id, percent)
	if err == nil || client.IsNotFound(err) {
		return
	}
	// 409: it finished between the read and the abort, which is what destroy wanted anyway.
	var ae *client.APIError
	if errors.As(err, &ae) && ae.StatusCode == http.StatusConflict {
		if again, gerr := r.client.GetRolloutRamp(ctx, project, flag, env, id); client.IsNotFound(gerr) || (gerr == nil && again.Finished()) {
			return
		}
	}
	detail := err.Error()
	if state.AbortPercent.IsNull() && ramp.IsDefaultRule {
		detail += "\n\nThe default rule's live rollout is not readable, so destroy aborts at the last step applied. " +
			"If the rollout was lowered since, set abort_percent to a value at or below it."
	}
	resp.Diagnostics.AddError(fmt.Sprintf("Aborting featureflip_rollout_ramp %q failed", id), detail)
}

// abortPercent is the percentage destroy leaves the rule at: abort_percent
// when set, otherwise wherever the rule is now, so nobody's assignment changes.
func (r *rolloutRampResource) abortPercent(ctx context.Context, state rolloutRampModel, ramp *client.RolloutRamp) (int64, error) {
	if !state.AbortPercent.IsNull() {
		return state.AbortPercent.ValueInt64(), nil
	}
	if !ramp.IsDefaultRule && ramp.RuleID != nil {
		targeting, err := r.client.GetTargeting(ctx, state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString())
		if err != nil {
			return 0, fmt.Errorf("reading the ramped rule's current rollout: %w", err)
		}
		for _, rule := range targeting.Rules {
			if rule.ID == *ramp.RuleID && rule.RolloutPercentage != nil {
				return *rule.RolloutPercentage, nil
			}
		}
		// The rule no longer serves a rollout: the API ignores the percentage.
		return 0, nil
	}
	// The public API does not expose the default rule's rollout; the last step
	// applied is where the ramp left it unless someone has changed it since.
	if ramp.CurrentStepIndex < 0 || int(ramp.CurrentStepIndex) >= len(ramp.Steps) {
		return 0, errors.New("the ramp has not applied a step yet, so its rollout is unknown: set abort_percent")
	}
	return ramp.Steps[ramp.CurrentStepIndex].Percent, nil
}

func (r *rolloutRampResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	project, flag, environment, rampID, err := splitFlagTriggerImportID(req.ID, "<project>/<flag>/<environment>/<ramp-id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), project)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("flag"), flag)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment"), environment)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), rampID)...)
}
