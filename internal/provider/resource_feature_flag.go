package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type featureFlagResource struct{ resourceWithClient }

var (
	_ resource.Resource                   = (*featureFlagResource)(nil)
	_ resource.ResourceWithConfigure      = (*featureFlagResource)(nil)
	_ resource.ResourceWithImportState    = (*featureFlagResource)(nil)
	_ resource.ResourceWithValidateConfig = (*featureFlagResource)(nil)
)

func newFeatureFlagResource() resource.Resource { return &featureFlagResource{} }

type featureFlagModel struct {
	ID                types.String `tfsdk:"id"`
	Project           types.String `tfsdk:"project"`
	Key               types.String `tfsdk:"key"`
	Name              types.String `tfsdk:"name"`
	Type              types.String `tfsdk:"type"`
	Description       types.String `tfsdk:"description"`
	Tags              types.Set    `tfsdk:"tags"`
	ClientSideVisible types.Bool   `tfsdk:"client_side_visible"`
	Archived          types.Bool   `tfsdk:"archived"`
	Variations        types.List   `tfsdk:"variations"`
	ExpiresAt         types.String `tfsdk:"expires_at"`
}

type variationModel struct {
	ID          types.String `tfsdk:"id"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
}

func variationAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":          types.StringType,
		"key":         types.StringType,
		"name":        types.StringType,
		"value":       types.StringType,
		"description": types.StringType,
	}
}

func (r *featureFlagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_feature_flag"
}

func (r *featureFlagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A feature flag. Import with `<project>/<flag-key>`.",
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
			"key": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{Required: true},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Flag type: Boolean, String, Number, or Json. Immutable.",
				Validators:    []validator.String{stringvalidator.OneOf("Boolean", "String", "Number", "Json")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description":         schema.StringAttribute{Optional: true},
			"tags":                schema.SetAttribute{Optional: true, ElementType: types.StringType},
			"client_side_visible": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"archived":            schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"expires_at": schema.StringAttribute{
				Optional: true,
				Description: "Advisory date by which the flag is expected to be removed, as an RFC 3339 timestamp " +
					"(e.g. `2027-01-31T00:00:00Z`). Evaluation never changes when it passes; the flag is reported as stale " +
					"so it can be cleaned up. It must be in the future when it is set or changed. A date that has since " +
					"passed stays valid and causes no diff. Omitting it clears any expiry set on the flag.",
				Validators: []validator.String{rfc3339Validator{}},
			},
			"variations": schema.ListNestedAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Flag variations. Omit for Boolean flags (auto-seeded with true/false). Required for String/Number/Json flags. Variation keys are immutable; for Json flags, value holds the JSON document as a string.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true},
						"key":         schema.StringAttribute{Required: true},
						"name":        schema.StringAttribute{Required: true},
						"value":       schema.StringAttribute{Required: true},
						"description": schema.StringAttribute{Optional: true},
					},
				},
			},
		},
	}
}

func (r *featureFlagResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg featureFlagModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Type.IsUnknown() || cfg.Type.IsNull() {
		return
	}
	if cfg.Type.ValueString() == "Boolean" {
		if !cfg.Variations.IsNull() && !cfg.Variations.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root("variations"), "variations not allowed for Boolean flags",
				"Boolean flags are auto-seeded with true/false variations by the API. Remove the variations attribute.")
		}
	} else if cfg.Variations.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("variations"), "variations required",
			fmt.Sprintf("%s flags must declare their variations at creation.", cfg.Type.ValueString()))
	}
}

// flagToModel converts an API flag to state. planVariations orders the state
// variation list: when non-null, state follows the plan's order (required for
// plan/state consistency); when null (Boolean flags, import), server order is used.
// priorExpiresAt keeps the configured spelling of an expiry the server echoes back.
func flagToModel(ctx context.Context, project string, f *client.Flag, planVariations types.List, planTags types.Set, priorExpiresAt types.String, diags *diag.Diagnostics) featureFlagModel {
	m := featureFlagModel{
		ID:                types.StringValue(f.ID),
		Project:           types.StringValue(project),
		Key:               types.StringValue(f.Key),
		Name:              types.StringValue(f.Name),
		Type:              types.StringValue(f.Type),
		Description:       types.StringPointerValue(f.Description),
		ClientSideVisible: types.BoolValue(f.ClientSideVisible),
		Archived:          types.BoolValue(f.IsArchived),
		ExpiresAt:         expiryToModel(priorExpiresAt, f.ExpiresAtUtc),
	}

	if planTags.IsNull() && len(f.Tags) == 0 {
		m.Tags = types.SetNull(types.StringType)
	} else {
		tags, d := types.SetValueFrom(ctx, types.StringType, f.Tags)
		diags.Append(d...)
		m.Tags = tags
	}

	byKey := make(map[string]client.Variation, len(f.Variations))
	for _, v := range f.Variations {
		byKey[v.Key] = v
	}
	var ordered []client.Variation
	if !planVariations.IsNull() && !planVariations.IsUnknown() {
		var pm []variationModel
		diags.Append(planVariations.ElementsAs(ctx, &pm, false)...)
		if diags.HasError() {
			return m
		}
		for _, p := range pm {
			if v, ok := byKey[p.Key.ValueString()]; ok {
				ordered = append(ordered, v)
				delete(byKey, p.Key.ValueString())
			}
		}
		for _, v := range f.Variations { // out-of-band additions, appended
			if _, still := byKey[v.Key]; still {
				ordered = append(ordered, v)
			}
		}
	} else {
		ordered = f.Variations
	}

	vms := make([]variationModel, 0, len(ordered))
	for _, v := range ordered {
		vms = append(vms, variationModel{
			ID:          types.StringValue(v.ID),
			Key:         types.StringValue(v.Key),
			Name:        types.StringValue(v.Name),
			Value:       types.StringValue(v.Value),
			Description: types.StringPointerValue(v.Description),
		})
	}
	l, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: variationAttrTypes()}, vms)
	diags.Append(d...)
	m.Variations = l
	return m
}

// tagsFromSet converts the tags set to a slice for the wire request. It
// always returns a non-nil slice: the API treats a JSON `null` tags value as
// "leave unchanged" but an empty array `[]` as "clear", and since tags is
// Optional-only (not Computed) Terraform expects omitting tags from config to
// actually clear them server-side.
func (r *featureFlagResource) tagsFromSet(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	tags := []string{}
	if s.IsNull() || s.IsUnknown() {
		return tags
	}
	diags.Append(s.ElementsAs(ctx, &tags, false)...)
	return tags
}

func (r *featureFlagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan featureFlagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project := plan.Project.ValueString()

	creq := client.CreateFlagRequest{
		Key:               plan.Key.ValueString(),
		Name:              plan.Name.ValueString(),
		Type:              plan.Type.ValueString(),
		Description:       plan.Description.ValueStringPointer(),
		Tags:              r.tagsFromSet(ctx, plan.Tags, &resp.Diagnostics),
		ClientSideVisible: plan.ClientSideVisible.ValueBool(),
	}
	if !plan.ExpiresAt.IsNull() && !plan.ExpiresAt.IsUnknown() {
		wire, err := expiryWireValue(plan.ExpiresAt)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("expires_at"), "Invalid expires_at", err.Error())
			return
		}
		creq.ExpiresAtUtc = &wire
	}
	if !plan.Variations.IsNull() && !plan.Variations.IsUnknown() {
		var pv []variationModel
		resp.Diagnostics.Append(plan.Variations.ElementsAs(ctx, &pv, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, v := range pv {
			creq.InitialVariations = append(creq.InitialVariations, client.VariationInput{
				Key: v.Key.ValueString(), Name: v.Name.ValueString(), Value: v.Value.ValueString(),
				Description: v.Description.ValueStringPointer(),
			})
		}
	}

	f, err := r.client.CreateFlag(ctx, project, creq)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_feature_flag %q failed", plan.Key.ValueString()), withExpiryHint(err))
		return
	}
	if plan.Archived.ValueBool() {
		if err := r.client.ArchiveFlag(ctx, project, f.Key); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Archiving featureflip_feature_flag %q after create failed", plan.Key.ValueString()), err.Error())
			return
		}
		f.IsArchived = true
	}
	m := flagToModel(ctx, project, f, plan.Variations, plan.Tags, plan.ExpiresAt, &resp.Diagnostics)
	if plan.Description.IsNull() && f.Description != nil && *f.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *featureFlagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state featureFlagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.client.GetFlag(ctx, state.Project.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_feature_flag %q failed", state.ID.ValueString()), err.Error())
		return
	}
	m := flagToModel(ctx, state.Project.ValueString(), f, state.Variations, state.Tags, state.ExpiresAt, &resp.Diagnostics)
	if state.Description.IsNull() && f.Description != nil && *f.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *featureFlagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state featureFlagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, key := plan.Project.ValueString(), plan.Key.ValueString()

	// Unarchive first so subsequent edits are accepted upstream.
	if state.Archived.ValueBool() && !plan.Archived.ValueBool() {
		if err := r.client.RestoreFlag(ctx, project, key); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Restoring featureflip_feature_flag %q failed", key), err.Error())
			return
		}
	}

	err := r.client.UpdateFlag(ctx, project, key, client.UpdateFlagRequest{
		Name:              plan.Name.ValueString(),
		Description:       plan.Description.ValueStringPointer(),
		Tags:              r.tagsFromSet(ctx, plan.Tags, &resp.Diagnostics),
		ClientSideVisible: plan.ClientSideVisible.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_feature_flag %q failed", key), err.Error())
		return
	}

	// Expiry has its own endpoints; the flag PUT above never touches it. Only
	// call them on a real change, so an expiry that has since passed never
	// trips EXPIRY_IN_PAST on an unrelated update.
	if !plan.ExpiresAt.IsUnknown() && expiryChanged(plan.ExpiresAt, state.ExpiresAt) {
		if plan.ExpiresAt.IsNull() {
			err = r.client.ClearFlagExpiry(ctx, project, key)
		} else {
			var wire string
			if wire, err = expiryWireValue(plan.ExpiresAt); err == nil {
				err = r.client.SetFlagExpiry(ctx, project, key, wire)
			}
		}
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Updating expires_at on featureflip_feature_flag %q failed", key), withExpiryHint(err))
			return
		}
	}

	if !plan.Variations.IsNull() && !plan.Variations.IsUnknown() {
		if ok := r.reconcileVariations(ctx, project, key, plan.Variations, &resp.Diagnostics); !ok {
			return
		}
	}

	// Archive last so all edits above land on an active flag.
	if !state.Archived.ValueBool() && plan.Archived.ValueBool() {
		if err := r.client.ArchiveFlag(ctx, project, key); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Archiving featureflip_feature_flag %q failed", key), err.Error())
			return
		}
	}

	f, err := r.client.GetFlag(ctx, project, key)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Re-reading featureflip_feature_flag %q after update failed", key), err.Error())
		return
	}
	m := flagToModel(ctx, project, f, plan.Variations, plan.Tags, plan.ExpiresAt, &resp.Diagnostics)
	if plan.Description.IsNull() && f.Description != nil && *f.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// withExpiryHint appends guidance to the API's expiry rejections.
func withExpiryHint(err error) string {
	msg := err.Error()
	switch {
	case client.HasCode(err, "EXPIRY_IN_PAST"):
		msg += "\n\nexpires_at must be in the future when it is set or changed. Pick a later date, or remove expires_at to clear it."
	case client.HasCode(err, "FEATURE_NOT_ENABLED"):
		msg += "\n\nFlag expiration dates are not enabled for this organization yet. Remove expires_at until they are."
	}
	return msg
}

// reconcileVariations converges server variations to the planned list, keyed
// by variation key: extra keys are deleted, missing keys added, matching keys
// updated (unconditional PUT — idempotent upstream).
func (r *featureFlagResource) reconcileVariations(ctx context.Context, project, flag string, planned types.List, diags *diag.Diagnostics) bool {
	var pv []variationModel
	diags.Append(planned.ElementsAs(ctx, &pv, false)...)
	if diags.HasError() {
		return false
	}
	f, err := r.client.GetFlag(ctx, project, flag)
	if err != nil {
		diags.AddError("Reading flag variations for reconciliation failed", err.Error())
		return false
	}
	serverByKey := make(map[string]client.Variation, len(f.Variations))
	for _, v := range f.Variations {
		serverByKey[v.Key] = v
	}
	for _, p := range pv {
		k := p.Key.ValueString()
		if sv, exists := serverByKey[k]; exists {
			err = r.client.UpdateVariation(ctx, project, flag, sv.ID, client.UpdateVariationRequest{
				Name: p.Name.ValueString(), Value: p.Value.ValueString(), Description: p.Description.ValueStringPointer(),
			})
			delete(serverByKey, k)
		} else {
			_, err = r.client.AddVariation(ctx, project, flag, client.VariationInput{
				Key: k, Name: p.Name.ValueString(), Value: p.Value.ValueString(), Description: p.Description.ValueStringPointer(),
			})
		}
		if err != nil {
			diags.AddError(fmt.Sprintf("Reconciling variation %q failed", k), err.Error())
			return false
		}
	}
	for k, sv := range serverByKey {
		if err := r.client.DeleteVariation(ctx, project, flag, sv.ID); err != nil {
			msg := err.Error()
			if client.HasCode(err, "VARIATION_HAS_DEPENDENTS") {
				msg += "\n\nThe variation is referenced by targeting rules or used as a default/prerequisite variation. Update those references first."
			}
			diags.AddError(fmt.Sprintf("Deleting variation %q failed", k), msg)
			return false
		}
	}
	return true
}

func (r *featureFlagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state featureFlagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteFlag(ctx, state.Project.ValueString(), state.Key.ValueString())
	if err != nil && !client.IsNotFound(err) {
		msg := err.Error()
		if client.HasCode(err, "FLAG_HAS_DEPENDENTS") {
			msg += "\n\nOther flags list this flag as a prerequisite. Remove those prerequisites first, or set archived = true instead of destroying."
		}
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_feature_flag %q failed", state.Key.ValueString()), msg)
	}
}

func (r *featureFlagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2, "<project>/<flag-key>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
