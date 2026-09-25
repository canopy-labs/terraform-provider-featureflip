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

type webhookSecretResource struct{ resourceWithClient }

var (
	_ resource.Resource                = (*webhookSecretResource)(nil)
	_ resource.ResourceWithConfigure   = (*webhookSecretResource)(nil)
	_ resource.ResourceWithImportState = (*webhookSecretResource)(nil)
)

func newWebhookSecretResource() resource.Resource { return &webhookSecretResource{} }

type webhookSecretModel struct {
	ID        types.String `tfsdk:"id"`
	WebhookID types.String `tfsdk:"webhook_id"`
	Secret    types.String `tfsdk:"secret"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *webhookSecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_secret"
}

func (r *webhookSecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An additional signing secret on a webhook subscription, for rotating secrets without dropping " +
			"deliveries. Every active secret signs every delivery, so receivers can accept the new secret before the " +
			"old one is retired. Destroying this resource RETIRES the secret (irreversible).\n\n" +
			"The plaintext is only available at creation and is stored in Terraform state — protect your state. " +
			"Import with `<webhook-id>/<secret-id>`; imported secrets cannot recover the plaintext. Importing the " +
			"webhook's `secret_id` is how you bring its creation secret under management to retire it.\n\n" +
			"A subscription must always keep one active secret. If destroying this resource would retire the last " +
			"one, Featureflip refuses; the provider then warns, leaves the secret active and drops it from state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"webhook_id": schema.StringAttribute{
				Required:      true,
				Description:   "Id of the `featureflip_webhook` this secret signs for.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Plaintext signing secret (creation-time only; null for imported secrets).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// findActiveSecret returns the secret with the given id when it exists and has
// not been retired.
func findActiveSecret(w *client.Webhook, id string) (client.WebhookSecret, bool) {
	for _, s := range w.Secrets {
		if s.ID == id {
			return s, s.RetiredAt == nil
		}
	}
	return client.WebhookSecret{}, false
}

func (r *webhookSecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhookID := plan.WebhookID.ValueString()
	created, err := r.client.AddWebhookSecret(ctx, webhookID)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Adding a signing secret to webhook %q failed", webhookID), err.Error())
		return
	}
	plan.ID = types.StringValue(created.SecretID)
	plan.Secret = types.StringValue(created.Secret)
	plan.CreatedAt = types.StringNull()
	// Persist before the follow-up read: the plaintext exists only in the
	// response above.
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	w, err := r.client.GetWebhook(ctx, webhookID)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading webhook %q after adding a signing secret failed", webhookID), err.Error())
		return
	}
	if s, ok := findActiveSecret(w, created.SecretID); ok {
		plan.CreatedAt = types.StringValue(s.CreatedAt)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookSecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.client.GetWebhook(ctx, state.WebhookID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading webhook %q failed", state.WebhookID.ValueString()), err.Error())
		return
	}
	s, ok := findActiveSecret(w, state.ID.ValueString())
	if !ok {
		// Retirement is irreversible — treat a retired (or vanished) secret as
		// deleted so the next apply adds a fresh one.
		resp.State.RemoveResource(ctx)
		return
	}
	state.CreatedAt = types.StringValue(s.CreatedAt)
	// state.Secret deliberately untouched — the plaintext never comes back from the API.
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never reached: webhook_id is the only configurable attribute and
// it forces replacement.
func (r *webhookSecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan webhookSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookSecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhookID := state.WebhookID.ValueString()
	err := r.client.RetireWebhookSecret(ctx, webhookID, state.ID.ValueString())
	switch {
	case err == nil, client.IsNotFound(err):
	case client.IsLastActiveSecret(err):
		// Usually the webhook itself is being destroyed in the same run and
		// takes the secret with it. Failing here would block that destroy.
		resp.Diagnostics.AddWarning(
			fmt.Sprintf("Signing secret %q left active on webhook %q", state.ID.ValueString(), webhookID),
			"Featureflip refuses to retire a subscription's last active signing secret, so this secret is still "+
				"active and signing deliveries. It has been removed from Terraform state. Add another "+
				"featureflip_webhook_secret before retiring this one, or delete the webhook.")
	default:
		resp.Diagnostics.AddError(fmt.Sprintf("Retiring signing secret %q on webhook %q failed", state.ID.ValueString(), webhookID), err.Error())
	}
}

func (r *webhookSecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Both segments are GUIDs, so neither can contain "/".
	parts, err := splitImportID(req.ID, 2, "<webhook-id>/<secret-id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("webhook_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("secret"), types.StringNull())...)
}
