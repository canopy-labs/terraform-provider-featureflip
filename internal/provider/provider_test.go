package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"featureflip": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range []string{"FEATUREFLIP_TOKEN", "FEATUREFLIP_ORGANIZATION", "FEATUREFLIP_BASE_URL"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests — run scripts/local-api.sh, then `make testacc`", v)
		}
	}
}
