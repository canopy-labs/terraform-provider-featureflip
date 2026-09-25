package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ---------- featureflip_organization ----------

type organizationDataSource struct{ datasourceWithClient }

func newOrganizationDataSource() datasource.DataSource { return &organizationDataSource{} }

type organizationDSModel struct {
	ID       types.String `tfsdk:"id"`
	Slug     types.String `tfsdk:"slug"`
	Name     types.String `tfsdk:"name"`
	Plan     types.String `tfsdk:"plan"`
	IsActive types.Bool   `tfsdk:"is_active"`
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an organization. Defaults to the provider's organization when slug is omitted.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true},
			"slug":      schema.StringAttribute{Optional: true, Computed: true},
			"name":      schema.StringAttribute{Computed: true},
			"plan":      schema.StringAttribute{Computed: true},
			"is_active": schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg organizationDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	slug := d.client.Org
	if !cfg.Slug.IsNull() {
		slug = cfg.Slug.ValueString()
	}
	org, err := d.client.GetOrganization(ctx, slug)
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_organization failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationDSModel{
		ID:       types.StringValue(org.ID),
		Slug:     types.StringValue(org.Slug),
		Name:     types.StringValue(org.Name),
		Plan:     types.StringValue(org.Plan),
		IsActive: types.BoolValue(org.IsActive),
	})...)
}

// ---------- featureflip_project ----------

type projectDataSource struct{ datasourceWithClient }

func newProjectDataSource() datasource.DataSource { return &projectDataSource{} }

type projectDSModel struct {
	ID          types.String `tfsdk:"id"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a project by key.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"key":         schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name":        schema.StringAttribute{Computed: true},
			"description": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg projectDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := d.client.GetProject(ctx, cfg.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_project failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectDSModel{
		ID:          types.StringValue(p.ID),
		Key:         types.StringValue(p.Key),
		Name:        types.StringValue(p.Name),
		Description: types.StringPointerValue(p.Description),
	})...)
}

// ---------- featureflip_environment ----------

type environmentDataSource struct{ datasourceWithClient }

func newEnvironmentDataSource() datasource.DataSource { return &environmentDataSource{} }

type environmentDSModel struct {
	ID        types.String `tfsdk:"id"`
	Project   types.String `tfsdk:"project"`
	Key       types.String `tfsdk:"key"`
	Name      types.String `tfsdk:"name"`
	Color     types.String `tfsdk:"color"`
	SortOrder types.Int64  `tfsdk:"sort_order"`
}

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an environment by project and key.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true},
			"project":    schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"key":        schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name":       schema.StringAttribute{Computed: true},
			"color":      schema.StringAttribute{Computed: true},
			"sort_order": schema.Int64Attribute{Computed: true},
		},
	}
}

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg environmentDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := d.client.GetEnvironment(ctx, cfg.Project.ValueString(), cfg.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_environment failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, environmentDSModel{
		ID:        types.StringValue(e.ID),
		Project:   cfg.Project,
		Key:       types.StringValue(e.Key),
		Name:      types.StringValue(e.Name),
		Color:     types.StringPointerValue(e.Color),
		SortOrder: types.Int64Value(e.SortOrder),
	})...)
}

// ---------- featureflip_feature_flag ----------

type featureFlagDataSource struct{ datasourceWithClient }

func newFeatureFlagDataSource() datasource.DataSource { return &featureFlagDataSource{} }

type featureFlagDSModel struct {
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

func (d *featureFlagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_feature_flag"
}

func (d *featureFlagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a feature flag by project and key.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true},
			"project":             schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"key":                 schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name":                schema.StringAttribute{Computed: true},
			"type":                schema.StringAttribute{Computed: true},
			"description":         schema.StringAttribute{Computed: true},
			"tags":                schema.SetAttribute{Computed: true, ElementType: types.StringType},
			"client_side_visible": schema.BoolAttribute{Computed: true},
			"archived":            schema.BoolAttribute{Computed: true},
			"expires_at": schema.StringAttribute{
				Computed:    true,
				Description: "Advisory expiry date (RFC 3339, UTC), or null when none is set.",
			},
			"variations": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true},
						"key":         schema.StringAttribute{Computed: true},
						"name":        schema.StringAttribute{Computed: true},
						"value":       schema.StringAttribute{Computed: true},
						"description": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *featureFlagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg featureFlagDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := d.client.GetFlag(ctx, cfg.Project.ValueString(), cfg.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_feature_flag failed", err.Error())
		return
	}
	m := flagToModel(ctx, cfg.Project.ValueString(), f, types.ListNull(types.ObjectType{AttrTypes: variationAttrTypes()}), types.SetNull(types.StringType), types.StringNull(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// flagToModel returns null tags for empty; data sources always materialize.
	if m.Tags.IsNull() {
		tags, diag := types.SetValueFrom(ctx, types.StringType, []string{})
		resp.Diagnostics.Append(diag...)
		m.Tags = tags
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, featureFlagDSModel{
		ID: m.ID, Project: m.Project, Key: m.Key, Name: m.Name, Type: m.Type,
		Description: m.Description, Tags: m.Tags, ClientSideVisible: m.ClientSideVisible,
		Archived: m.Archived, Variations: m.Variations, ExpiresAt: m.ExpiresAt,
	})...)
}

// ---------- featureflip_segment ----------

type segmentDataSource struct{ datasourceWithClient }

func newSegmentDataSource() datasource.DataSource { return &segmentDataSource{} }

type segmentDSModel struct {
	ID          types.String `tfsdk:"id"`
	Project     types.String `tfsdk:"project"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Conditions  types.List   `tfsdk:"conditions"`
}

func (d *segmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (d *segmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a segment by project and key.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"project":     schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"key":         schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name":        schema.StringAttribute{Computed: true},
			"description": schema.StringAttribute{Computed: true},
			"conditions": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"attribute": schema.StringAttribute{Computed: true},
						"operator":  schema.StringAttribute{Computed: true},
						"values":    schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"negate":    schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *segmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg segmentDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := d.client.GetSegment(ctx, cfg.Project.ValueString(), cfg.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading featureflip_segment failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, segmentDSModel{
		ID:          types.StringValue(s.ID),
		Project:     cfg.Project,
		Key:         types.StringValue(s.Key),
		Name:        types.StringValue(s.Name),
		Description: types.StringPointerValue(s.Description),
		Conditions:  conditionsToList(ctx, s.Conditions, &resp.Diagnostics),
	})...)
}
