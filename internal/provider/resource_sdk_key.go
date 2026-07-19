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

type sdkKeyResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*sdkKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*sdkKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sdkKeyResource)(nil)
)

func newSDKKeyResource() resource.Resource { return &sdkKeyResource{} }

type sdkKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Project     types.String `tfsdk:"project"`
	Environment types.String `tfsdk:"environment"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Type        types.String `tfsdk:"type"`
	Key         types.String `tfsdk:"key"`
	LastFour    types.String `tfsdk:"last_four"`
}

func (r *sdkKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sdk_key"
}

func (r *sdkKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An SDK key for an environment. The plaintext key is only available at creation and is stored " +
			"in Terraform state — protect your state. Destroying this resource REVOKES the key (revocation is " +
			"irreversible). Import with `<project>/<environment>/<key-id>`; imported keys cannot recover the plaintext.",
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
			"environment": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "ClientSide or ServerSide. Immutable.",
				Validators:    []validator.String{stringvalidator.OneOf("ClientSide", "ServerSide")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Plaintext SDK key (creation-time only; empty for imported keys).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_four": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sdkKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sdkKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateSDKKey(ctx, plan.Project.ValueString(), plan.Environment.ValueString(), client.CreateSDKKeyRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Type:        plan.Type.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_sdk_key %q failed", plan.Name.ValueString()), err.Error())
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.Key = types.StringValue(created.Key)
	plan.LastFour = types.StringValue(created.LastFourCharacters)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sdkKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sdkKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := r.client.GetSDKKey(ctx, state.Project.ValueString(), state.Environment.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_sdk_key %q failed", state.Name.ValueString()), err.Error())
		return
	}
	if !k.IsActive {
		// Revocation is irreversible — treat a revoked key as deleted so the
		// next apply recreates it.
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(k.Name)
	// Don't manufacture drift between null and empty description.
	if !(state.Description.IsNull() && k.Description != nil && *k.Description == "") {
		state.Description = types.StringPointerValue(k.Description)
	}
	state.Type = types.StringValue(k.Type)
	state.LastFour = types.StringValue(k.LastFourCharacters)
	// state.Key deliberately untouched — the plaintext never comes back from the API.
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sdkKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sdkKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deliberately no re-fetch after this PUT: UpdateSDKKey returns no response
	// body, so there is nothing server-side to reconcile back into plan.
	err := r.client.UpdateSDKKey(ctx, plan.Project.ValueString(), plan.Environment.ValueString(), state.ID.ValueString(), client.UpdateSDKKeyRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_sdk_key %q failed", plan.Name.ValueString()), err.Error())
		return
	}
	plan.ID = state.ID
	plan.Key = state.Key
	plan.LastFour = state.LastFour
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sdkKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sdkKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RevokeSDKKey(ctx, state.Project.ValueString(), state.Environment.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Revoking featureflip_sdk_key %q failed", state.Name.ValueString()), err.Error())
	}
}

func (r *sdkKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	project, environment, sdkKeyID, err := splitSDKKeyImportID(req.ID, "<project>/<environment>/<sdk-key-id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), project)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment"), environment)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), sdkKeyID)...)
}
