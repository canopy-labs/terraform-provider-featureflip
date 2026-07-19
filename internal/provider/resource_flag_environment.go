package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type flagEnvironmentResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*flagEnvironmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*flagEnvironmentResource)(nil)
	_ resource.ResourceWithImportState = (*flagEnvironmentResource)(nil)
)

func newFlagEnvironmentResource() resource.Resource { return &flagEnvironmentResource{} }

type flagEnvModel struct {
	ID               types.String `tfsdk:"id"`
	Project          types.String `tfsdk:"project"`
	Flag             types.String `tfsdk:"flag"`
	Environment      types.String `tfsdk:"environment"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	DefaultVariation types.String `tfsdk:"default_variation"`
	Strategy         types.String `tfsdk:"strategy"`
	Prerequisites    types.List   `tfsdk:"prerequisites"`
	Rules            types.List   `tfsdk:"rules"`
}

type prereqModel struct {
	Flag      types.String `tfsdk:"flag"`
	Variation types.String `tfsdk:"variation"`
}

func prereqAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"flag": types.StringType, "variation": types.StringType}
}

type ruleModel struct {
	ID                types.String `tfsdk:"id"`
	Description       types.String `tfsdk:"description"`
	Variation         types.String `tfsdk:"variation"`
	RolloutPercentage types.Int64  `tfsdk:"rollout_percentage"`
	Segment           types.String `tfsdk:"segment"`
	ConditionGroups   types.List   `tfsdk:"condition_groups"`
}

type conditionGroupModel struct {
	Operator   types.String `tfsdk:"operator"`
	Conditions types.List   `tfsdk:"conditions"`
}

func conditionGroupAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"operator":   types.StringType,
		"conditions": types.ListType{ElemType: types.ObjectType{AttrTypes: conditionAttrTypes()}},
	}
}

func ruleAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                 types.StringType,
		"description":        types.StringType,
		"variation":          types.StringType,
		"rollout_percentage": types.Int64Type,
		"segment":            types.StringType,
		"condition_groups":   types.ListType{ElemType: types.ObjectType{AttrTypes: conditionGroupAttrTypes()}},
	}
}

func (r *flagEnvironmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flag_environment"
}

func (r *flagEnvironmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Per-environment configuration of a feature flag: on/off state, fallthrough, prerequisites, and targeting rules. " +
			"When `rules` is set it is AUTHORITATIVE: Terraform owns every rule in this flag-environment and removes rules created elsewhere. " +
			"Import with `<project>/<flag>/<environment>` (imports identifiers only; add rules/prerequisites to config to manage them). " +
			"Destroying this resource deletes the managed targeting rules and DISABLES the flag in this environment; the fallthrough " +
			"configuration (default variation/strategy) is left in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
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
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"default_variation": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Variation KEY served as fallthrough. Defaults to the server-side default when omitted.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"strategy": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.OneOf("SingleVariation", "PercentageRollout", "TargetedRollout")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"prerequisites": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Prerequisite flags. Omit to leave prerequisites unmanaged; set (even empty) to replace wholesale.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"flag":      schema.StringAttribute{Required: true, Description: "Prerequisite flag key."},
						"variation": schema.StringAttribute{Required: true, Description: "Expected variation key of the prerequisite flag."},
					},
				},
			},
			"rules": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Ordered targeting rules (list position = priority). Authoritative when set.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true},
						"description": schema.StringAttribute{Optional: true},
						"variation":   schema.StringAttribute{Required: true, Description: "Variation KEY served when the rule matches."},
						"rollout_percentage": schema.Int64Attribute{
							Optional:   true,
							Validators: []validator.Int64{int64validator.Between(0, 100)},
						},
						"segment": schema.StringAttribute{Optional: true, Description: "Segment KEY this rule targets."},
						"condition_groups": schema.ListNestedAttribute{
							Optional: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"operator": schema.StringAttribute{
										Required:   true,
										Validators: []validator.String{stringvalidator.OneOf("And", "Or")},
									},
									"conditions": conditionsSchemaAttribute(true),
								},
							},
						},
					},
				},
			},
		},
	}
}

func flagEnvID(project, flag, env string) string { return project + "/" + flag + "/" + env }

// variationMaps returns key→id and id→key lookups for a flag's variations.
func (r *flagEnvironmentResource) variationMaps(ctx context.Context, project, flag string) (map[string]string, map[string]string, error) {
	f, err := r.client.GetFlag(ctx, project, flag)
	if err != nil {
		return nil, nil, err
	}
	idByKey := make(map[string]string, len(f.Variations))
	keyByID := make(map[string]string, len(f.Variations))
	for _, v := range f.Variations {
		idByKey[v.Key] = v.ID
		keyByID[v.ID] = v.Key
	}
	return idByKey, keyByID, nil
}

// apply converges the server to the plan and fills computed fields in *plan.
// Shared by Create and Update.
func (r *flagEnvironmentResource) apply(ctx context.Context, plan *flagEnvModel, diags *diag.Diagnostics) {
	project, flag, env := plan.Project.ValueString(), plan.Flag.ValueString(), plan.Environment.ValueString()

	varIDByKey, varKeyByID, err := r.variationMaps(ctx, project, flag)
	if err != nil {
		diags.AddError(fmt.Sprintf("Reading flag %q for featureflip_flag_environment failed", flag), err.Error())
		return
	}
	cfg, err := r.client.GetFlagEnvConfig(ctx, project, flag, env)
	if err != nil {
		diags.AddError(fmt.Sprintf("Reading flag environment config for %s/%s/%s failed", project, flag, env), err.Error())
		return
	}

	// --- fallthrough config PUT ---
	defaultVarID := cfg.DefaultVariationID
	if !plan.DefaultVariation.IsNull() && !plan.DefaultVariation.IsUnknown() {
		id, ok := varIDByKey[plan.DefaultVariation.ValueString()]
		if !ok {
			diags.AddAttributeError(path.Root("default_variation"), "Unknown variation key",
				fmt.Sprintf("flag %q has no variation with key %q", flag, plan.DefaultVariation.ValueString()))
			return
		}
		defaultVarID = id
	}
	var strategy *string
	switch {
	case !plan.Strategy.IsNull() && !plan.Strategy.IsUnknown():
		strategy = plan.Strategy.ValueStringPointer()
	case cfg.Strategy != "":
		strategy = &cfg.Strategy
	}
	var prereqs []client.Prerequisite // nil → "prerequisites":null → unmanaged upstream
	if !plan.Prerequisites.IsNull() && !plan.Prerequisites.IsUnknown() {
		var pm []prereqModel
		diags.Append(plan.Prerequisites.ElementsAs(ctx, &pm, false)...)
		if diags.HasError() {
			return
		}
		prereqs = make([]client.Prerequisite, 0, len(pm))
		for _, p := range pm {
			prereqs = append(prereqs, client.Prerequisite{
				PrerequisiteFlagKey:  p.Flag.ValueString(),
				ExpectedVariationKey: p.Variation.ValueString(),
			})
		}
	}
	if err := r.client.UpdateFlagEnvConfig(ctx, project, flag, env, client.UpdateFlagEnvConfigRequest{
		DefaultVariationID: defaultVarID,
		Strategy:           strategy,
		Prerequisites:      prereqs,
	}); err != nil {
		diags.AddError(fmt.Sprintf("Updating flag environment config for %s/%s/%s failed", project, flag, env), err.Error())
		return
	}

	// --- toggle ---
	if cfg.IsEnabled != plan.Enabled.ValueBool() {
		if err := r.client.ToggleFlag(ctx, project, flag, env, plan.Enabled.ValueBool()); err != nil {
			diags.AddError(fmt.Sprintf("Toggling flag %s/%s/%s failed", project, flag, env), err.Error())
			return
		}
	}

	// --- rules (authoritative when set) ---
	if !plan.Rules.IsNull() && !plan.Rules.IsUnknown() {
		var rm []ruleModel
		diags.Append(plan.Rules.ElementsAs(ctx, &rm, false)...)
		if diags.HasError() {
			return
		}
		segIDByKey := map[string]string{}
		inputs := make([]client.RuleInput, len(rm))
		for i, m := range rm {
			in, ok := r.ruleInput(ctx, project, flag, m, varIDByKey, segIDByKey, diags)
			if !ok {
				return
			}
			inputs[i] = in
		}

		tc, err := r.client.GetTargeting(ctx, project, flag, env)
		if err != nil {
			diags.AddError(fmt.Sprintf("Reading targeting rules for %s/%s/%s failed", project, flag, env), err.Error())
			return
		}
		server := tc.Rules

		finalIDs := make([]string, 0, len(inputs))
		for i, in := range inputs {
			if i < len(server) {
				if err := r.client.UpdateRule(ctx, project, flag, env, server[i].ID, in); err != nil {
					diags.AddError(fmt.Sprintf("Updating targeting rule %d for %s/%s/%s failed", i, project, flag, env), err.Error())
					return
				}
				finalIDs = append(finalIDs, server[i].ID)
			} else {
				created, err := r.client.CreateRule(ctx, project, flag, env, in)
				if err != nil {
					diags.AddError(fmt.Sprintf("Creating targeting rule %d for %s/%s/%s failed", i, project, flag, env), err.Error())
					return
				}
				finalIDs = append(finalIDs, created.ID)
			}
		}
		for i := len(inputs); i < len(server); i++ {
			if err := r.client.DeleteRule(ctx, project, flag, env, server[i].ID); err != nil {
				diags.AddError(fmt.Sprintf("Deleting surplus targeting rule for %s/%s/%s failed", project, flag, env), err.Error())
				return
			}
		}
		if len(finalIDs) > 1 {
			if err := r.client.ReorderRules(ctx, project, flag, env, finalIDs); err != nil {
				diags.AddError(fmt.Sprintf("Reordering targeting rules for %s/%s/%s failed", project, flag, env), err.Error())
				return
			}
		}

		// Fill computed rule IDs back into the plan, preserving plan order/values.
		for i := range rm {
			rm[i].ID = types.StringValue(finalIDs[i])
		}
		rules, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ruleAttrTypes()}, rm)
		diags.Append(d...)
		plan.Rules = rules
	}

	// --- fill remaining computed fields from the server ---
	cfg, err = r.client.GetFlagEnvConfig(ctx, project, flag, env)
	if err != nil {
		diags.AddError(fmt.Sprintf("Re-reading flag environment config for %s/%s/%s failed", project, flag, env), err.Error())
		return
	}
	plan.ID = types.StringValue(flagEnvID(project, flag, env))
	if plan.DefaultVariation.IsNull() || plan.DefaultVariation.IsUnknown() {
		plan.DefaultVariation = types.StringValue(varKeyByID[cfg.DefaultVariationID])
	}
	if plan.Strategy.IsNull() || plan.Strategy.IsUnknown() {
		if cfg.Strategy == "" {
			plan.Strategy = types.StringNull()
		} else {
			plan.Strategy = types.StringValue(cfg.Strategy)
		}
	}
	plan.Enabled = types.BoolValue(cfg.IsEnabled)
}

func (r *flagEnvironmentResource) ruleInput(ctx context.Context, project, flag string, m ruleModel, varIDByKey, segIDByKey map[string]string, diags *diag.Diagnostics) (client.RuleInput, bool) {
	in := client.RuleInput{
		Description:     m.Description.ValueStringPointer(),
		ConditionGroups: []client.ConditionGroup{},
	}
	varID, ok := varIDByKey[m.Variation.ValueString()]
	if !ok {
		diags.AddError("Unknown variation key in targeting rule",
			fmt.Sprintf("flag %q has no variation with key %q", flag, m.Variation.ValueString()))
		return in, false
	}
	in.VariationID = varID
	if !m.RolloutPercentage.IsNull() {
		in.RolloutPercentage = m.RolloutPercentage.ValueInt64Pointer()
	}
	if !m.Segment.IsNull() {
		key := m.Segment.ValueString()
		segID, cached := segIDByKey[key]
		if !cached {
			seg, err := r.client.GetSegment(ctx, project, key)
			if err != nil {
				diags.AddError(fmt.Sprintf("Unknown segment key %q in targeting rule for flag %q", key, flag), err.Error())
				return in, false
			}
			segID = seg.ID
			segIDByKey[key] = segID
		}
		in.UserSegmentID = &segID
	}
	if !m.ConditionGroups.IsNull() {
		var gm []conditionGroupModel
		diags.Append(m.ConditionGroups.ElementsAs(ctx, &gm, false)...)
		if diags.HasError() {
			return in, false
		}
		for _, g := range gm {
			conds, ok := conditionsFromList(ctx, g.Conditions, diags)
			if !ok {
				return in, false
			}
			in.ConditionGroups = append(in.ConditionGroups, client.ConditionGroup{
				Operator:   g.Operator.ValueString(),
				Conditions: conds,
			})
		}
	}
	return in, true
}

func (r *flagEnvironmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan flagEnvModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flagEnvironmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan flagEnvModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flagEnvironmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state flagEnvModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env := state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString()

	_, varKeyByID, err := r.variationMaps(ctx, project, flag)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading flag %q for featureflip_flag_environment failed", flag), err.Error())
		return
	}
	cfg, err := r.client.GetFlagEnvConfig(ctx, project, flag, env)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading flag environment config for %s/%s/%s failed", project, flag, env), err.Error())
		return
	}

	state.ID = types.StringValue(flagEnvID(project, flag, env))
	state.Enabled = types.BoolValue(cfg.IsEnabled)
	state.DefaultVariation = types.StringValue(varKeyByID[cfg.DefaultVariationID])
	if cfg.Strategy == "" {
		state.Strategy = types.StringNull()
	} else {
		state.Strategy = types.StringValue(cfg.Strategy)
	}

	// Prerequisites: only refreshed when managed (non-null in state).
	if !state.Prerequisites.IsNull() {
		pms := make([]prereqModel, 0, len(cfg.Prerequisites))
		for _, p := range cfg.Prerequisites {
			pms = append(pms, prereqModel{
				Flag:      types.StringValue(p.PrerequisiteFlagKey),
				Variation: types.StringValue(p.ExpectedVariationKey),
			})
		}
		l, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: prereqAttrTypes()}, pms)
		resp.Diagnostics.Append(d...)
		state.Prerequisites = l
	}

	// Rules: only refreshed when managed (non-null in state).
	if !state.Rules.IsNull() {
		// Index prior state rules by ID so each server rule can be realigned
		// (condition-group/condition order) and normalized (null-vs-"" for
		// description) against its last known-good shape.
		var priorRules []ruleModel
		resp.Diagnostics.Append(state.Rules.ElementsAs(ctx, &priorRules, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		priorByID := make(map[string]ruleModel, len(priorRules))
		for _, p := range priorRules {
			if !p.ID.IsNull() && !p.ID.IsUnknown() {
				priorByID[p.ID.ValueString()] = p
			}
		}

		tc, err := r.client.GetTargeting(ctx, project, flag, env)
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Reading targeting rules for %s/%s/%s failed", project, flag, env), err.Error())
			return
		}
		segKeyByID := map[string]string{}
		needSegments := false
		for _, rule := range tc.Rules {
			if rule.UserSegmentID != nil {
				needSegments = true
			}
		}
		if needSegments {
			segs, err := r.client.ListSegments(ctx, project)
			if err != nil {
				resp.Diagnostics.AddError(fmt.Sprintf("Listing segments to resolve rule targets for %s failed", project), err.Error())
				return
			}
			for _, s := range segs {
				segKeyByID[s.ID] = s.Key
			}
		}
		rms := make([]ruleModel, 0, len(tc.Rules))
		for _, rule := range tc.Rules {
			prior, hasPrior := priorByID[rule.ID]

			m := ruleModel{
				ID:          types.StringValue(rule.ID),
				Description: types.StringPointerValue(rule.Description),
				Variation:   types.StringValue(varKeyByID[rule.VariationID]),
			}
			// The backend normalizes an absent description to "" — don't
			// manufacture drift for rules configured without one (same guard
			// as resource_segment.go's description handling).
			if hasPrior && prior.Description.IsNull() && rule.Description != nil && *rule.Description == "" {
				m.Description = types.StringNull()
			}
			if rule.RolloutPercentage != nil {
				m.RolloutPercentage = types.Int64Value(*rule.RolloutPercentage)
			} else {
				m.RolloutPercentage = types.Int64Null()
			}
			if rule.UserSegmentID != nil {
				m.Segment = types.StringValue(segKeyByID[*rule.UserSegmentID])
			} else {
				m.Segment = types.StringNull()
			}
			// Realign condition groups (and their nested conditions) to the
			// prior state's order — the API does not preserve condition-list
			// order (see reorderConditionsLike / reorderConditionGroupsLike).
			if hasPrior && !prior.ConditionGroups.IsNull() && !prior.ConditionGroups.IsUnknown() {
				var pgm []conditionGroupModel
				resp.Diagnostics.Append(prior.ConditionGroups.ElementsAs(ctx, &pgm, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				priorGroups := make([]client.ConditionGroup, 0, len(pgm))
				for _, pg := range pgm {
					conds, ok := conditionsFromList(ctx, pg.Conditions, &resp.Diagnostics)
					if !ok {
						return
					}
					priorGroups = append(priorGroups, client.ConditionGroup{
						Operator:   pg.Operator.ValueString(),
						Conditions: conds,
					})
				}
				rule.ConditionGroups = reorderConditionGroupsLike(priorGroups, rule.ConditionGroups)
			}
			gms := make([]conditionGroupModel, 0, len(rule.ConditionGroups))
			for _, g := range rule.ConditionGroups {
				gms = append(gms, conditionGroupModel{
					Operator:   types.StringValue(g.Operator),
					Conditions: conditionsToList(ctx, g.Conditions, &resp.Diagnostics),
				})
			}
			gl, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: conditionGroupAttrTypes()}, gms)
			resp.Diagnostics.Append(d...)
			// Only coerce an empty result to null when there's no prior state to
			// tell us the config held an explicit `[]` — an explicit empty list
			// must round-trip as `[]`, not null (same pattern as segmentToModel's
			// keepNullConditions guard).
			if len(rule.ConditionGroups) == 0 && (!hasPrior || prior.ConditionGroups.IsNull()) {
				gl = types.ListNull(types.ObjectType{AttrTypes: conditionGroupAttrTypes()})
			}
			m.ConditionGroups = gl
			rms = append(rms, m)
		}
		rl, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ruleAttrTypes()}, rms)
		resp.Diagnostics.Append(d...)
		state.Rules = rl
	}

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *flagEnvironmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state flagEnvModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env := state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString()

	if !state.Rules.IsNull() {
		tc, err := r.client.GetTargeting(ctx, project, flag, env)
		if err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError(fmt.Sprintf("Reading targeting rules for %s/%s/%s destroy failed", project, flag, env), err.Error())
			return
		}
		if tc != nil {
			for _, rule := range tc.Rules {
				if err := r.client.DeleteRule(ctx, project, flag, env, rule.ID); err != nil && !client.IsNotFound(err) {
					resp.Diagnostics.AddError(fmt.Sprintf("Deleting targeting rule for %s/%s/%s failed", project, flag, env), err.Error())
					return
				}
			}
		}
	}
	if err := r.client.ToggleFlag(ctx, project, flag, env, false); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Disabling flag %s/%s/%s on destroy failed", project, flag, env), err.Error())
	}
}

func (r *flagEnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportIDTail(req.ID, 3, "<project>/<flag>/<environment>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("flag"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), flagEnvID(parts[0], parts[1], parts[2]))...)
}
