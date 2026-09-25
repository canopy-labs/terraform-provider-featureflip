package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

type featureflipProvider struct {
	version string
}

var _ provider.Provider = (*featureflipProvider)(nil)

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &featureflipProvider{version: version}
	}
}

type providerModel struct {
	Organization types.String `tfsdk:"organization"`
	Token        types.String `tfsdk:"token"`
	BaseURL      types.String `tfsdk:"base_url"`
}

func (p *featureflipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "featureflip"
	resp.Version = p.version
}

func (p *featureflipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Featureflip projects, environments, feature flags, targeting, segments, and SDK keys.",
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{
				Optional:    true,
				Description: "Organization slug. May also be set via the FEATUREFLIP_ORGANIZATION environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API token (ffs_… service token recommended, or ffp_… personal access token). May also be set via FEATUREFLIP_TOKEN. Tokens are created in the Featureflip dashboard.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "API base URL. Defaults to https://api.featureflip.io. May also be set via FEATUREFLIP_BASE_URL.",
			},
		},
	}
}

func (p *featureflipProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for name, v := range map[string]types.String{"organization": cfg.Organization, "token": cfg.Token, "base_url": cfg.BaseURL} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider configuration value",
				fmt.Sprintf("%s depends on a value known only after apply. Set it statically or via its environment variable.", name))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	org := stringOr(cfg.Organization, os.Getenv("FEATUREFLIP_ORGANIZATION"))
	token := stringOr(cfg.Token, os.Getenv("FEATUREFLIP_TOKEN"))
	baseURL := stringOr(cfg.BaseURL, os.Getenv("FEATUREFLIP_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.featureflip.io"
	}

	if org == "" {
		resp.Diagnostics.AddAttributeError(path.Root("organization"), "Missing organization",
			"Set the organization attribute or FEATUREFLIP_ORGANIZATION to your organization slug.")
	}
	switch {
	case token == "":
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set the token attribute or FEATUREFLIP_TOKEN. Create a service token in the Featureflip dashboard (Organization Settings → Service Tokens).")
	case !tokenFormatOK(token):
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Invalid API token",
			"Featureflip tokens start with ffs_ (service token) or ffp_ (personal access token).")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := client.New(baseURL, token, org, p.version)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("base_url"), "Invalid base_url", err.Error())
		return
	}
	if _, err := c.GetOrganization(ctx, org); err != nil {
		resp.Diagnostics.AddError("Featureflip authentication failed",
			fmt.Sprintf("GET /orgs/%s against %s failed: %s\n\nVerify the token is valid (not revoked or expired), that the organization slug is correct, and that the token belongs to this organization.", org, baseURL, err))
		return
	}

	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *featureflipProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newProjectResource,
		newEnvironmentResource,
		newSegmentResource,
		newFeatureFlagResource,
		newFlagEnvironmentResource,
		newSDKKeyResource,
		newWebhookResource,
		newWebhookSecretResource,
	}
}

func (p *featureflipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newOrganizationDataSource,
		newProjectDataSource,
		newEnvironmentDataSource,
		newFeatureFlagDataSource,
		newSegmentDataSource,
		newWebhookEventTypesDataSource,
	}
}
