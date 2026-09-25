package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type webhookResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*webhookResource)(nil)
	_ resource.ResourceWithConfigure   = (*webhookResource)(nil)
	_ resource.ResourceWithImportState = (*webhookResource)(nil)
)

func newWebhookResource() resource.Resource { return &webhookResource{} }

type webhookModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	URL            types.String `tfsdk:"url"`
	Type           types.String `tfsdk:"type"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	EventTypes     types.Set    `tfsdk:"event_types"`
	ProjectIDs     types.Set    `tfsdk:"project_ids"`
	EnvironmentIDs types.Set    `tfsdk:"environment_ids"`
	Secret         types.String `tfsdk:"secret"`
	SecretID       types.String `tfsdk:"secret_id"`
	AutoDisabledAt types.String `tfsdk:"auto_disabled_at"`
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An outbound webhook subscription: where Featureflip sends configuration-change events for the " +
			"provider's organization. Requires an Admin token.\n\n" +
			"The first signing secret is only available at creation and is stored in Terraform state — protect " +
			"your state. To rotate it, add a `featureflip_webhook_secret`. Import with the webhook id; imported " +
			"webhooks cannot recover the secret.\n\n" +
			"Featureflip disables a subscription whose deliveries keep failing (`auto_disabled_at` is set). " +
			"Because `enabled` defaults to `true`, the next plan shows `enabled` changing back to `true`, and " +
			"applying it re-enables the subscription and resets its failure count. Fix the receiver first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 200)},
			},
			"url": schema.StringAttribute{
				Required: true,
				Description: "Receiver URL. Must be https and must not point at a private, loopback, link-local or " +
					"reserved address. If it embeds a credential, pass it through a sensitive variable.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 2048)},
			},
			"type": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("GenericHttp"),
				Description:   "Receiver type (the API's `provider` field, renamed because Terraform reserves that name). Only `GenericHttp` is available. Immutable.",
				Validators:    []validator.String{stringvalidator.OneOf("GenericHttp")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"event_types": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Event types to send, e.g. `flag.toggled`. Omit (or leave empty) to receive every event type. " +
					"The `featureflip_webhook_event_types` data source lists the valid values.",
				Validators: []validator.Set{setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
			},
			"project_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Project ids (`featureflip_project.<name>.id`, not keys) to send events for. Omit to cover every project.",
				Validators:  []validator.Set{setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
			},
			"environment_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Environment ids (`featureflip_environment.<name>.id`, not keys) to send events for. Omit to cover every environment.",
				Validators:  []validator.Set{setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The signing secret created with the subscription (creation-time only; null for imported webhooks).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_id": schema.StringAttribute{
				Computed: true,
				Description: "Id of the signing secret created with the subscription. Use it to adopt that secret " +
					"into a `featureflip_webhook_secret` when rotating it out.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"auto_disabled_at": schema.StringAttribute{
				Computed:    true,
				Description: "When Featureflip disabled the subscription after repeated delivery failures; null otherwise.",
			},
		},
	}
}

// setToSlice converts an optional string set to a wire slice. It always
// returns a non-nil slice: upstream reads an empty list as "all", and an
// omitted attribute must mean exactly that.
func setToSlice(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

// sliceToSet mirrors the feature-flag tags pattern: an empty upstream list
// stays null when the prior value was null, so omitting the attribute never
// shows drift against the API's [].
func sliceToSet(ctx context.Context, prior types.Set, vals []string, diags *diag.Diagnostics) types.Set {
	if prior.IsNull() && len(vals) == 0 {
		return types.SetNull(types.StringType)
	}
	s, d := types.SetValueFrom(ctx, types.StringType, vals)
	diags.Append(d...)
	return s
}

// keepUntrimmed keeps the configured value when upstream only trimmed its
// surrounding whitespace, so padding in config is neither drift nor an
// inconsistent-result error.
func keepUntrimmed(prior types.String, got string) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && strings.TrimSpace(prior.ValueString()) == got {
		return prior
	}
	return types.StringValue(got)
}

// applyWebhook copies the API's view of the subscription onto m. prior
// supplies null-vs-empty intent for the sets; secret fields are untouched
// because the API never returns the plaintext.
func applyWebhook(ctx context.Context, m *webhookModel, prior webhookModel, w *client.Webhook, diags *diag.Diagnostics) {
	m.ID = types.StringValue(w.ID)
	m.Name = keepUntrimmed(prior.Name, w.Name)
	m.URL = keepUntrimmed(prior.URL, w.URL)
	m.Type = types.StringValue(w.Provider)
	m.Enabled = types.BoolValue(w.IsEnabled)
	m.EventTypes = sliceToSet(ctx, prior.EventTypes, w.EventTypes, diags)
	m.ProjectIDs = sliceToSet(ctx, prior.ProjectIDs, w.ProjectIDs, diags)
	m.EnvironmentIDs = sliceToSet(ctx, prior.EnvironmentIDs, w.EnvironmentIDs, diags)
	m.AutoDisabledAt = types.StringPointerValue(w.AutoDisabledAt)
}

func (r *webhookResource) updateRequest(ctx context.Context, plan webhookModel, diags *diag.Diagnostics) client.UpdateWebhookRequest {
	return client.UpdateWebhookRequest{
		Name:           plan.Name.ValueString(),
		URL:            plan.URL.ValueString(),
		EventTypes:     setToSlice(ctx, plan.EventTypes, diags),
		ProjectIDs:     setToSlice(ctx, plan.ProjectIDs, diags),
		EnvironmentIDs: setToSlice(ctx, plan.EnvironmentIDs, diags),
		IsEnabled:      plan.Enabled.ValueBool(),
	}
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	created, err := r.client.CreateWebhook(ctx, client.CreateWebhookRequest{
		Name:           name,
		URL:            plan.URL.ValueString(),
		Provider:       plan.Type.ValueString(),
		EventTypes:     setToSlice(ctx, plan.EventTypes, &resp.Diagnostics),
		ProjectIDs:     setToSlice(ctx, plan.ProjectIDs, &resp.Diagnostics),
		EnvironmentIDs: setToSlice(ctx, plan.EnvironmentIDs, &resp.Diagnostics),
	})
	if err != nil {
		detail := err.Error()
		if client.IsNotFound(err) {
			detail += "\n\nThe webhooks API answered 404. Outbound webhooks may not be enabled for this Featureflip deployment."
		}
		resp.Diagnostics.AddError(fmt.Sprintf("Creating featureflip_webhook %q failed", name), detail)
		return
	}

	// Persist what exists before any follow-up call can fail: the secret is
	// shown only in this response, so losing it to a later error would orphan
	// a live subscription whose secret nobody can read. Record the
	// subscription as it is right now — enabled, whatever the plan says — so a
	// failed disable below can't leave state claiming a live subscription is off.
	early := plan
	early.ID = types.StringValue(created.ID)
	early.Secret = types.StringValue(created.Secret)
	early.SecretID = types.StringNull()
	early.AutoDisabledAt = types.StringNull()
	early.Enabled = types.BoolValue(true)
	resp.Diagnostics.Append(resp.State.Set(ctx, early)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = early.ID
	plan.Secret = early.Secret
	plan.SecretID = early.SecretID

	// The create request has no enabled flag (subscriptions start enabled),
	// so a disabled subscription takes a follow-up full-replace PUT.
	if !plan.Enabled.ValueBool() {
		if err := r.client.UpdateWebhook(ctx, created.ID, r.updateRequest(ctx, plan, &resp.Diagnostics)); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Disabling featureflip_webhook %q after creation failed", name), err.Error())
			return
		}
	}

	w, err := r.client.GetWebhook(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_webhook %q after creation failed", name), err.Error())
		return
	}
	applyWebhook(ctx, &plan, plan, w, &resp.Diagnostics)
	// A new subscription has exactly one secret: the one this response returned.
	if len(w.Secrets) == 1 {
		plan.SecretID = types.StringValue(w.Secrets[0].ID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.client.GetWebhook(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_webhook %q failed", state.Name.ValueString()), err.Error())
		return
	}
	prior := state
	applyWebhook(ctx, &state, prior, w, &resp.Diagnostics)
	// An imported webhook has no secret_id yet. Fill it only when there is a
	// single secret to choose from; with several, which one was the creation
	// secret is not knowable from the API.
	if state.SecretID.IsNull() && len(w.Secrets) == 1 {
		state.SecretID = types.StringValue(w.Secrets[0].ID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	body := r.updateRequest(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateWebhook(ctx, id, body); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Updating featureflip_webhook %q failed", plan.Name.ValueString()), err.Error())
		return
	}
	w, err := r.client.GetWebhook(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading featureflip_webhook %q after update failed", plan.Name.ValueString()), err.Error())
		return
	}
	plan.ID = state.ID
	plan.Secret = state.Secret
	plan.SecretID = state.SecretID
	applyWebhook(ctx, &plan, plan, w, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteWebhook(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Deleting featureflip_webhook %q failed", state.Name.ValueString()), err.Error())
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("secret"), types.StringNull())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("secret_id"), types.StringNull())...)
}
