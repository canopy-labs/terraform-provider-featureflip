package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type segmentResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*segmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*segmentResource)(nil)
	_ resource.ResourceWithImportState = (*segmentResource)(nil)
)

func newSegmentResource() resource.Resource { return &segmentResource{} }

type segmentModel struct {
	ID          types.String `tfsdk:"id"`
	Project     types.String `tfsdk:"project"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Conditions  types.List   `tfsdk:"conditions"`
}

func (r *segmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (r *segmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A reusable user segment. Import with `<project>/<segment-key>`.",
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
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"conditions":  conditionsSchemaAttribute(false),
		},
	}
}

func (r *segmentResource) segmentToModel(ctx context.Context, project string, s *client.Segment, keepNullConditions bool, diags *diag.Diagnostics) segmentModel {
	m := segmentModel{
		ID:          types.StringValue(s.ID),
		Project:     types.StringValue(project),
		Key:         types.StringValue(s.Key),
		Name:        types.StringValue(s.Name),
		Description: types.StringPointerValue(s.Description),
	}
	if keepNullConditions && len(s.Conditions) == 0 {
		m.Conditions = types.ListNull(types.ObjectType{AttrTypes: conditionAttrTypes()})
	} else {
		m.Conditions = conditionsToList(ctx, s.Conditions, diags)
	}
	return m
}

func (r *segmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan segmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	conds, ok := conditionsFromList(ctx, plan.Conditions, &resp.Diagnostics)
	if !ok {
		return
	}
	s, err := r.client.CreateSegment(ctx, plan.Project.ValueString(), client.CreateSegmentRequest{
		Key:         plan.Key.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Conditions:  conds,
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_segment %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	// The API does not preserve submission order for a segment's condition
	// list (see reorderConditionsLike) — realign to the plan's order so the
	// ordered `conditions` List attribute doesn't trip Terraform's
	// post-apply consistency check.
	s.Conditions = reorderConditionsLike(conds, s.Conditions)
	m := r.segmentToModel(ctx, plan.Project.ValueString(), s, plan.Conditions.IsNull(), &resp.Diagnostics)
	// Don't manufacture drift between null and empty description.
	if plan.Description.IsNull() && s.Description != nil && *s.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *segmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state segmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	priorConds, ok := conditionsFromList(ctx, state.Conditions, &resp.Diagnostics)
	if !ok {
		return
	}
	s, err := r.client.GetSegment(ctx, state.Project.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_segment %q failed", state.ID.ValueString()), err.Error())
		return
	}
	// Realign to the prior state's order — see reorderConditionsLike.
	s.Conditions = reorderConditionsLike(priorConds, s.Conditions)
	m := r.segmentToModel(ctx, state.Project.ValueString(), s, state.Conditions.IsNull(), &resp.Diagnostics)
	if state.Description.IsNull() && s.Description != nil && *s.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *segmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan segmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	conds, ok := conditionsFromList(ctx, plan.Conditions, &resp.Diagnostics)
	if !ok {
		return
	}
	err := r.client.UpdateSegment(ctx, plan.Project.ValueString(), plan.Key.ValueString(), client.UpdateSegmentRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Conditions:  conds,
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_segment %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	s, err := r.client.GetSegment(ctx, plan.Project.ValueString(), plan.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Re-reading featureflip_segment %q after update failed", plan.Key.ValueString()), err.Error())
		return
	}
	// Realign to the plan's order — see reorderConditionsLike.
	s.Conditions = reorderConditionsLike(conds, s.Conditions)
	m := r.segmentToModel(ctx, plan.Project.ValueString(), s, plan.Conditions.IsNull(), &resp.Diagnostics)
	if plan.Description.IsNull() && s.Description != nil && *s.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *segmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state segmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteSegment(ctx, state.Project.ValueString(), state.Key.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_segment %q failed", state.Key.ValueString()),
			err.Error()+"\n\nIf the segment is referenced by targeting rules, remove those rules first.")
	}
}

func (r *segmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportIDTail(req.ID, 2, "<project>/<segment-key>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
