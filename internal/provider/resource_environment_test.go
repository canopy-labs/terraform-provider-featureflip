package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccEnvironment_basic(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Env Acc"
}

resource "featureflip_environment" "test" {
  project    = featureflip_project.test.key
  key        = "acctest"
  name       = "Staging"
  color      = "#00ff00"
  sort_order = 5
}`, proj),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_environment.test", "key", "acctest"),
					resource.TestCheckResourceAttr("featureflip_environment.test", "sort_order", "5"),
					resource.TestCheckResourceAttrSet("featureflip_environment.test", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Env Acc"
}

resource "featureflip_environment" "test" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Staging Renamed"
}`, proj),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_environment.test", "name", "Staging Renamed"),
					resource.TestCheckResourceAttr("featureflip_environment.test", "sort_order", "0"),
				),
			},
			{
				ResourceName:      "featureflip_environment.test",
				ImportState:       true,
				ImportStateId:     proj + "/acctest",
				ImportStateVerify: true,
			},
		},
	})
}
