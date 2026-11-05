package provider

import (
	"fmt"
	"os"
	"regexp"
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

func testAccCriticalEnvConfig(proj, reasonAttrs string, enabled bool) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Critical Env Acc"
}

resource "featureflip_environment" "test" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Critical"
  %[2]s
}

resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "critical-test"
  name    = "Critical Test"
  type    = "Boolean"
}

resource "featureflip_flag_environment" "test" {
  project           = featureflip_project.test.key
  flag              = featureflip_feature_flag.test.key
  environment       = featureflip_environment.test.key
  enabled           = %[3]t
  default_variation = "false"
}

data "featureflip_environment" "production" {
  project    = featureflip_project.test.key
  key        = "production"
  depends_on = [featureflip_environment.test]
}`, proj, reasonAttrs, enabled)
}

// A critical environment refuses writes without a change reason (422
// change_reason_required), accepts them with one, and can be loosened again
// with one. The provider reads its reason from FEATUREFLIP_CHANGE_REASON here,
// which seed.sh sets for the whole suite; the second step unsets it.
func TestAccEnvironment_criticalSettings(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	reason := os.Getenv("FEATUREFLIP_CHANGE_REASON")
	if reason == "" {
		reason = "terraform acceptance tests"
	}
	t.Setenv("FEATUREFLIP_CHANGE_REASON", reason) // restored after the test

	const critical = "require_change_reason = true\n  require_confirmation = true"
	const loosened = "require_change_reason = false\n  require_confirmation = false"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCriticalEnvConfig(proj, critical, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_environment.test", "require_change_reason", "true"),
					resource.TestCheckResourceAttr("featureflip_environment.test", "require_confirmation", "true"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.test", "enabled", "true"),
					// A new project on a Business plan starts with a critical production environment.
					resource.TestCheckResourceAttr("data.featureflip_environment.production", "require_change_reason", "true"),
				),
			},
			{
				PreConfig:   func() { os.Unsetenv("FEATUREFLIP_CHANGE_REASON") },
				Config:      testAccCriticalEnvConfig(proj, critical, false),
				ExpectError: regexp.MustCompile(`(?s)change_reason_required.*FEATUREFLIP_CHANGE_REASON`),
			},
			{
				PreConfig: func() { os.Setenv("FEATUREFLIP_CHANGE_REASON", reason) },
				Config:    testAccCriticalEnvConfig(proj, loosened, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_environment.test", "require_change_reason", "false"),
					resource.TestCheckResourceAttr("featureflip_environment.test", "require_confirmation", "false"),
					resource.TestCheckResourceAttr("featureflip_flag_environment.test", "enabled", "false"),
				),
			},
			{
				// Omitting the settings leaves them alone: no diff against the loosened values.
				Config:   testAccCriticalEnvConfig(proj, "", false),
				PlanOnly: true,
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
