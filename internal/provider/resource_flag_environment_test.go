package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccFlagEnvBase(proj string) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "FlagEnv Acc"
}

resource "featureflip_environment" "prod" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}

resource "featureflip_segment" "beta" {
  project = featureflip_project.test.key
  key     = "beta"
  name    = "Beta"
  conditions = [
    { attribute = "email", operator = "EndsWith", values = ["@acme.io"] },
  ]
}

resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "rollout-test"
  name    = "Rollout Test"
  type    = "Boolean"
}`, proj)
}

func TestAccFlagEnvironment_rulesLifecycle(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFlagEnvBase(proj) + `
resource "featureflip_flag_environment" "prod" {
  project           = featureflip_project.test.key
  flag              = featureflip_feature_flag.test.key
  environment       = featureflip_environment.prod.key
  enabled           = true
  default_variation = "false"
  prerequisites     = []
  rules = [
    {
      description = "beta users get true"
      variation   = "true"
      segment     = featureflip_segment.beta.key
    },
    {
      description = "germany 50% rollout"
      variation   = "true"
      rollout_percentage = 50
      condition_groups = [
        {
          operator = "And"
          conditions = [
            { attribute = "country", operator = "Equals", values = ["DE"] },
            { attribute = "plan", operator = "Equals", values = ["premium"] },
          ]
        },
        {
          operator = "Or"
          conditions = [
            { attribute = "beta", operator = "Equals", values = ["true"] },
          ]
        },
      ]
    },
    {
      variation          = "false"
      rollout_percentage = 10
    },
  ]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "enabled", "true"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.#", "3"),
					resource.TestCheckResourceAttrSet("featureflip_flag_environment.prod", "rules.0.id"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.1.rollout_percentage", "50"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.1.condition_groups.#", "2"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.1.condition_groups.0.conditions.#", "2"),
					resource.TestCheckNoResourceAttr("featureflip_flag_environment.prod", "rules.2.description"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.2.rollout_percentage", "10"),
				),
			},
			{
				// Reorder (swap) + drop segment rule's description + remove rollout rule → reconcile
				Config: testAccFlagEnvBase(proj) + `
resource "featureflip_flag_environment" "prod" {
  project           = featureflip_project.test.key
  flag              = featureflip_feature_flag.test.key
  environment       = featureflip_environment.prod.key
  enabled           = false
  default_variation = "false"
  rules = [
    {
      description = "germany 50% rollout"
      variation   = "true"
      rollout_percentage = 50
      condition_groups = [
        {
          operator = "And"
          conditions = [
            { attribute = "country", operator = "Equals", values = ["DE"] },
          ]
        },
      ]
    },
  ]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "enabled", "false"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.#", "1"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.prod", "rules.0.description", "germany 50% rollout"),
				),
			},
			{
				ResourceName:            "featureflip_flag_environment.prod",
				ImportState:             true,
				ImportStateId:           proj + "/rollout-test/acctest",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"rules", "prerequisites"},
			},
		},
	})
}
