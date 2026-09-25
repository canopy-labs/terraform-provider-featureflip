package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ---------- featureflip_webhook_event_types ----------

type webhookEventTypesDataSource struct{ datasourceWithClient }

func newWebhookEventTypesDataSource() datasource.DataSource { return &webhookEventTypesDataSource{} }

type webhookEventTypesDSModel struct {
	EventTypes types.List `tfsdk:"event_types"`
}

func (d *webhookEventTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_event_types"
}

func (d *webhookEventTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The event types a `featureflip_webhook` can filter on. Requires an Admin token.",
		Attributes: map[string]schema.Attribute{
			"event_types": schema.ListAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *webhookEventTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	eventTypes, err := d.client.ListWebhookEventTypes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_webhook_event_types failed", err.Error())
		return
	}
	if eventTypes == nil {
		eventTypes = []string{}
	}
	l, diags := types.ListValueFrom(ctx, types.StringType, eventTypes)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, webhookEventTypesDSModel{EventTypes: l})...)
}
