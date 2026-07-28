package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type projectResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)

func newProjectResource() resource.Resource { return &projectResource{} }

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Featureflip project. Import with `terraform import featureflip_project.example <project-key>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				Required:      true,
				Description:   "Immutable project key (changing it replaces the project).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
		},
	}
}

func projectToModel(p *client.Project) projectModel {
	return projectModel{
		ID:          types.StringValue(p.ID),
		Key:         types.StringValue(p.Key),
		Name:        types.StringValue(p.Name),
		Description: types.StringPointerValue(p.Description),
	}
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.CreateProject(ctx, client.CreateProjectRequest{
		Key:         plan.Key.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_project %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	m := projectToModel(p)
	// Don't manufacture drift between null and empty description.
	if plan.Description.IsNull() && p.Description != nil && *p.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.GetProject(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_project %q failed", state.ID.ValueString()), err.Error())
		return
	}
	m := projectToModel(p)
	// Don't manufacture drift between null and empty description.
	if state.Description.IsNull() && p.Description != nil && *p.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UpdateProject(ctx, plan.Key.ValueString(), client.UpdateProjectRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_project %q failed", plan.Key.ValueString()), err.Error())
		return
	}
	p, err := r.client.GetProject(ctx, plan.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Re-reading featureflip_project %q after update failed", plan.Key.ValueString()), err.Error())
		return
	}
	m := projectToModel(p)
	if plan.Description.IsNull() && p.Description != nil && *p.Description == "" {
		m.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProject(ctx, state.Key.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_project %q failed", state.Key.ValueString()), err.Error())
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Accepts a project key or UUID — the API dual-addresses both.
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
