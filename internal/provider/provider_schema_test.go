package provider

import (
	"context"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
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
