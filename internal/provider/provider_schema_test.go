package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderSchema(t *testing.T) {
	p := New("test")()

	var mdResp fwprovider.MetadataResponse
	p.Metadata(context.Background(), fwprovider.MetadataRequest{}, &mdResp)
	if mdResp.TypeName != "featureflip" {
		t.Fatalf("expected type name featureflip, got %q", mdResp.TypeName)
	}

	var schemaResp fwprovider.SchemaResponse
	p.Schema(context.Background(), fwprovider.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", schemaResp.Diagnostics)
	}
	for _, name := range []string{"organization", "token", "base_url"} {
		if _, ok := schemaResp.Schema.Attributes[name]; !ok {
			t.Errorf("missing provider attribute %q", name)
		}
	}
	if !schemaResp.Schema.Attributes["token"].IsSensitive() {
		t.Error("token attribute must be sensitive")
	}
}

// TestResourceAndDataSourceSchemasAreValid runs the framework's own
// implementation checks (reserved names like "provider", default-without-
// computed, …) offline, so a schema Terraform would refuse to load fails
// `make test` instead of only the acceptance suite.
func TestResourceAndDataSourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	for _, newResource := range p.Resources(ctx) {
		r := newResource()
		var md resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "featureflip"}, &md)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		resp.Diagnostics.Append(resp.Schema.ValidateImplementation(ctx)...)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", md.TypeName, resp.Diagnostics)
		}
	}

	for _, newDataSource := range p.DataSources(ctx) {
		d := newDataSource()
		var md datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "featureflip"}, &md)
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)
		resp.Diagnostics.Append(resp.Schema.ValidateImplementation(ctx)...)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", md.TypeName, resp.Diagnostics)
		}
	}
}
