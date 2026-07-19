package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type environmentResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*environmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*environmentResource)(nil)
	_ resource.ResourceWithImportState = (*environmentResource)(nil)
)

func newEnvironmentResource() resource.Resource { return &environmentResource{} }

type environmentModel struct {
	ID        types.String `tfsdk:"id"`
	Project   types.String `tfsdk:"project"`
	Key       types.String `tfsdk:"key"`
	Name      types.String `tfsdk:"name"`
	Color     types.String `tfsdk:"color"`
	SortOrder types.Int64  `tfsdk:"sort_order"`
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An environment within a FeatureFlip project. Import with `<project>/<key>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project": schema.StringAttribute{
				Required:      true,
				Description:   "Project key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"key": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name":  schema.StringAttribute{Required: true},
			"color": schema.StringAttribute{Optional: true, Description: "Display color, e.g. #ff5500."},
			"sort_order": schema.Int64Attribute{
				Optional:   true,
				Computed:   true,
				Default:    int64default.StaticInt64(0),
				Validators: []validator.Int64{int64validator.Between(-2147483648, 2147483647)},
			},
		},
	}
}

func environmentToModel(project string, e *client.Environment) environmentModel {
	return environmentModel{
		ID:        types.StringValue(e.ID),
		Project:   types.StringValue(project),
		Key:       types.StringValue(e.Key),
		Name:      types.StringValue(e.Name),
		Color:     types.StringPointerValue(e.Color),
		SortOrder: types.Int64Value(e.SortOrder),
	}
}

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.CreateEnvironment(ctx, plan.Project.ValueString(), client.CreateEnvironmentRequest{
		Key:       plan.Key.ValueString(),
		Name:      plan.Name.ValueString(),
		Color:     plan.Color.ValueStringPointer(),
		SortOrder: plan.SortOrder.ValueInt64(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_environment %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, environmentToModel(plan.Project.ValueString(), e))...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.GetEnvironment(ctx, state.Project.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_environment %q failed", state.Key.ValueString()), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, environmentToModel(state.Project.ValueString(), e))...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UpdateEnvironment(ctx, plan.Project.ValueString(), plan.Key.ValueString(), client.UpdateEnvironmentRequest{
		Name:      plan.Name.ValueString(),
		Color:     plan.Color.ValueStringPointer(),
		SortOrder: plan.SortOrder.ValueInt64(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_environment %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	e, err := r.client.GetEnvironment(ctx, plan.Project.ValueString(), plan.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Re-reading featureflip_environment %q after update failed", plan.Key.ValueString()), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, environmentToModel(plan.Project.ValueString(), e))...)
}

func (r *environmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteEnvironment(ctx, state.Project.ValueString(), state.Key.ValueString())
	if err != nil && !client.IsNotFound(err) {
		if client.HasCode(err, "LAST_ENVIRONMENT") {
			resp.Diagnostics.AddError("Cannot delete the last environment",
				"A FeatureFlip project must keep at least one environment. Create another environment before destroying this one.")
			return
		}
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_environment %q failed", state.Key.ValueString()), err.Error())
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportIDTail(req.ID, 2, "<project>/<environment-key>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
