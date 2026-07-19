package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDataSources_basic(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "DS Acc"
}

resource "featureflip_environment" "prod" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}

resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "ds-flag"
  name    = "DS Flag"
  type    = "Boolean"
}

resource "featureflip_segment" "test" {
  project = featureflip_project.test.key
  key     = "ds-segment"
  name    = "DS Segment"
  conditions = [
    { attribute = "plan", operator = "Equals", values = ["pro"] },
  ]
}

data "featureflip_organization" "current" {}

data "featureflip_project" "test" {
  key = featureflip_project.test.key
}

data "featureflip_environment" "prod" {
  project = featureflip_project.test.key
  key     = featureflip_environment.prod.key
}

data "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = featureflip_feature_flag.test.key
}

data "featureflip_segment" "test" {
  project = featureflip_project.test.key
  key     = featureflip_segment.test.key
}`, proj),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.featureflip_organization.current", "id"),
					resource.TestCheckResourceAttrSet("data.featureflip_organization.current", "plan"),
					resource.TestCheckResourceAttr("data.featureflip_project.test", "name", "DS Acc"),
					resource.TestCheckResourceAttr("data.featureflip_environment.prod", "name", "Acc Test Env"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "type", "Boolean"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "variations.#", "2"),
					resource.TestCheckResourceAttr("data.featureflip_segment.test", "conditions.0.attribute", "plan"),
				),
			},
		},
	})
}
