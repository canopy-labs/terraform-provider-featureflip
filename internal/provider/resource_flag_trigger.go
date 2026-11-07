package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

type flagTriggerResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*flagTriggerResource)(nil)
	_ resource.ResourceWithConfigure   = (*flagTriggerResource)(nil)
	_ resource.ResourceWithImportState = (*flagTriggerResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*flagTriggerResource)(nil)
)

func newFlagTriggerResource() resource.Resource { return &flagTriggerResource{} }

type flagTriggerModel struct {
	ID              types.String `tfsdk:"id"`
	Project         types.String `tfsdk:"project"`
	Flag            types.String `tfsdk:"flag"`
	Environment     types.String `tfsdk:"environment"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Action          types.String `tfsdk:"action"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	RotationKeepers types.Map    `tfsdk:"rotation_keepers"`
	URL             types.String `tfsdk:"url"`
	Endpoint        types.String `tfsdk:"endpoint"`
	Secret          types.String `tfsdk:"secret"`
	SecretLastFour  types.String `tfsdk:"secret_last_four"`
}

func (r *flagTriggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flag_trigger"
}

func (r *flagTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A flag trigger: a secret URL that turns one flag on or off in one environment when a monitoring " +
			"tool (a Grafana alert, a Datadog monitor) POSTs to it. Calling it when the flag is already in the target " +
			"state changes nothing. Needs the Team plan or above.\n\n" +
			"The URL and secret are only available when the trigger is created or rotated, and are stored in " +
			"Terraform state — protect your state. Change `rotation_keepers` to rotate them in place: the old URL " +
			"stops working at once. Destroying this resource deletes the trigger and its URL stops working.\n\n" +
			"Import with `<project>/<flag>/<environment>/<trigger-id>`; imported triggers cannot recover the URL " +
			"until they are rotated.",
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
				Description:   "Key of the flag the trigger turns on or off.",
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
			"action": schema.StringAttribute{
				Required:      true,
				Description:   "Disable turns the flag off (a kill switch); Enable turns it on. Immutable.",
				Validators:    []validator.String{stringvalidator.OneOf("Enable", "Disable")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether calling the URL does anything. false makes it answer 404 until re-enabled. Defaults to true.",
			},
			"rotation_keepers": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Arbitrary values that, when set or changed, rotate the trigger's URL and secret in place, e.g. " +
					"`{ rotated = \"2026-11-06\" }`. The trigger keeps its id and history. Setting them on an imported " +
					"trigger is how it gets a URL into state. Removing them does not rotate.",
			},
			"url": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The URL a monitoring tool POSTs to. Contains the secret. Null for imported triggers until rotated.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint": schema.StringAttribute{
				Computed: true,
				Description: "The URL without the secret, for tools that can send a header: POST to it with " +
					"`Authorization: Bearer <secret>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The trigger's secret, for the `Authorization: Bearer` form. Null for imported triggers until rotated.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_last_four": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ModifyPlan marks the URL and secret unknown when rotation_keepers changes:
// UseStateForUnknown would otherwise plan the old values, and the apply would
// fail with "provider produced inconsistent result".
func (r *flagTriggerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state flagTriggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !rotationRequested(plan, state) {
		return
	}
	plan.URL = types.StringUnknown()
	plan.Secret = types.StringUnknown()
	plan.SecretLastFour = types.StringUnknown()
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

// rotationRequested reports whether rotation_keepers differs between plan and
// state. Removing the keepers altogether doesn't rotate: there is nothing new
// to rotate for, and it would surprise someone tidying up their config.
func rotationRequested(plan, state flagTriggerModel) bool {
	if plan.RotationKeepers.IsNull() || plan.RotationKeepers.IsUnknown() {
		return plan.RotationKeepers.IsUnknown()
	}
	return !plan.RotationKeepers.Equal(state.RotationKeepers)
}

func (r *flagTriggerResource) applyTrigger(m *flagTriggerModel, t *client.FlagTrigger) {
	m.ID = types.StringValue(t.ID)
	m.Name = types.StringValue(t.Name)
	// Don't manufacture drift between null and empty description.
	if !(m.Description.IsNull() && t.Description != nil && *t.Description == "") {
		m.Description = types.StringPointerValue(t.Description)
	}
	m.Action = types.StringValue(t.Action)
	m.Enabled = types.BoolValue(t.Enabled)
	m.SecretLastFour = types.StringValue(t.SecretLastFour)
}

func (r *flagTriggerResource) applySecret(m *flagTriggerModel, created *client.FlagTriggerWithURL) {
	r.applyTrigger(m, &created.Trigger)
	m.URL = types.StringValue(created.URL)
	m.Endpoint = types.StringValue(created.Endpoint)
	m.Secret = types.StringValue(created.Secret)
}

func (r *flagTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan flagTriggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env := plan.Project.ValueString(), plan.Flag.ValueString(), plan.Environment.ValueString()
	created, err := r.client.CreateFlagTrigger(ctx, project, flag, env, client.CreateFlagTriggerRequest{
		Name:        plan.Name.ValueString(),
		Action:      plan.Action.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_flag_trigger %q on flag %q failed", plan.Name.ValueString(), flag), err.Error())
		return
	}
	wantEnabled := plan.Enabled.ValueBool()
	r.applySecret(&plan, created)
	// Persist before any follow-up write: the URL exists only in the response above.
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() || created.Trigger.Enabled == wantEnabled {
		return
	}

	// Triggers are created enabled; enabled = false needs a second write.
	updated, err := r.client.UpdateFlagTrigger(ctx, project, flag, env, created.Trigger.ID, client.UpdateFlagTriggerRequest{
		Name:        created.Trigger.Name,
		Description: created.Trigger.Description,
		Enabled:     wantEnabled,
	})
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Disabling featureflip_flag_trigger %q after creating it failed", plan.Name.ValueString()), err.Error())
		return
	}
	r.applyTrigger(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flagTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state flagTriggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.GetFlagTrigger(ctx, state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_flag_trigger %q failed", state.Name.ValueString()), err.Error())
		return
	}
	r.applyTrigger(&state, t)
	// state.URL and state.Secret deliberately untouched — they never come back from the API.
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *flagTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state flagTriggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, flag, env, id := plan.Project.ValueString(), plan.Flag.ValueString(), plan.Environment.ValueString(), state.ID.ValueString()
	rotate := rotationRequested(plan, state)

	plan.URL, plan.Endpoint, plan.Secret, plan.SecretLastFour = state.URL, state.Endpoint, state.Secret, state.SecretLastFour
	// A keepers-only change skips the PUT, which would write an audit row for nothing.
	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) || !plan.Enabled.Equal(state.Enabled) {
		updated, err := r.client.UpdateFlagTrigger(ctx, project, flag, env, id, client.UpdateFlagTriggerRequest{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueStringPointer(),
			Enabled:     plan.Enabled.ValueBool(),
		})
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_flag_trigger %q failed", plan.Name.ValueString()), err.Error())
			return
		}
		r.applyTrigger(&plan, updated)
	}
	if !rotate {
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		return
	}

	rotated, err := r.client.RotateFlagTrigger(ctx, project, flag, env, id)
	if err != nil {
		// Keep the old keepers in state so the next apply tries the rotation again.
		plan.RotationKeepers = state.RotationKeepers
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		resp.Diagnostics.AddError(fmt.Sprintf("Rotating featureflip_flag_trigger %q failed", plan.Name.ValueString()), err.Error())
		return
	}
	r.applySecret(&plan, rotated)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flagTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state flagTriggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteFlagTrigger(ctx, state.Project.ValueString(), state.Flag.ValueString(), state.Environment.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_flag_trigger %q failed", state.Name.ValueString()), err.Error())
	}
}

func (r *flagTriggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	project, flag, environment, triggerID, err := splitFlagTriggerImportID(req.ID, "<project>/<flag>/<environment>/<trigger-id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), project)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("flag"), flag)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment"), environment)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), triggerID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), types.StringNull())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("secret"), types.StringNull())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("endpoint"), types.StringNull())...)
}

// splitFlagTriggerImportID parses "<project>/<flag>/<environment>/<trigger-id>".
// project and flag keys are regex-constrained upstream to exclude "/", and the
// trigger id is a GUID, but the environment key may itself contain "/": it is
// whatever lies between the second "/" and the last one.
func splitFlagTriggerImportID(id, format string) (project, flag, environment, triggerID string, err error) {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) != 3 {
		return "", "", "", "", fmt.Errorf("unexpected import ID %q: expected %s", id, format)
	}
	last := strings.LastIndex(parts[2], "/")
	if last == -1 {
		return "", "", "", "", fmt.Errorf("unexpected import ID %q: expected %s", id, format)
	}
	project, flag, environment, triggerID = parts[0], parts[1], parts[2][:last], parts[2][last+1:]
	if project == "" || flag == "" || environment == "" || triggerID == "" {
		return "", "", "", "", fmt.Errorf("unexpected import ID %q: empty segment, expected %s", id, format)
	}
	return project, flag, environment, triggerID, nil
}
