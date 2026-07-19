package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSDKKey_basic(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	base := fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "SDK Acc"
}

resource "featureflip_environment" "prod" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}`, proj)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + `
resource "featureflip_sdk_key" "test" {
  project     = featureflip_project.test.key
  environment = featureflip_environment.prod.key
  name        = "backend"
  type        = "ServerSide"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("featureflip_sdk_key.test", "key"),
					resource.TestCheckResourceAttrSet("featureflip_sdk_key.test", "last_four"),
					resource.TestCheckResourceAttrSet("featureflip_sdk_key.test", "id"),
				),
			},
			{
				Config: base + `
resource "featureflip_sdk_key" "test" {
  project     = featureflip_project.test.key
  environment = featureflip_environment.prod.key
  name        = "backend-renamed"
  type        = "ServerSide"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_sdk_key.test", "name", "backend-renamed"),
					// plaintext must survive updates untouched
					resource.TestCheckResourceAttrSet("featureflip_sdk_key.test", "key"),
				),
			},
		},
	})
}
