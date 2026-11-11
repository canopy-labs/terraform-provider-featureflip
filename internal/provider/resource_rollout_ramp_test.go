package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// description is the ramped rule's; otherDescription belongs to a second rule.
func testAccRolloutRampConfig(proj, abortPercent, description, otherDescription string) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Ramp Acc"
}

resource "featureflip_environment" "env" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}

resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "new-checkout"
  name    = "New Checkout"
  type    = "Boolean"
}

resource "featureflip_flag_environment" "test" {
  project     = featureflip_project.test.key
  flag        = featureflip_feature_flag.test.key
  environment = featureflip_environment.env.key
  enabled     = true

  rules = [{
    description        = %q
    variation          = "true"
    rollout_percentage = 10
    condition_groups = [{
      operator   = "And"
      conditions = [{ attribute = "plan", operator = "Equals", values = ["beta"] }]
    }]
  }, {
    description = %q
    variation   = "true"
    condition_groups = [{
      operator   = "And"
      conditions = [{ attribute = "plan", operator = "Equals", values = ["staff"] }]
    }]
  }]

  # The ramps below move these; without this the next apply would undo them.
  lifecycle {
    ignore_changes = [rules[0].rollout_percentage, strategy, default_variation]
  }
}

resource "featureflip_rollout_ramp" "rule" {
  project       = featureflip_project.test.key
  flag          = featureflip_feature_flag.test.key
  environment   = featureflip_environment.env.key
  rule_id       = featureflip_flag_environment.test.rules[0].id
  abort_percent = %s

  steps = [
    { percent = 10, hold_minutes = 60 },
    { percent = 50, hold_minutes = 60 },
    { percent = 100, hold_minutes = 0 },
  ]
}

resource "featureflip_rollout_ramp" "default" {
  project      = featureflip_project.test.key
  flag         = featureflip_feature_flag.test.key
  environment  = featureflip_environment.env.key
  default_rule = true
  variation    = "true"

  steps = [
    { percent = 5, hold_minutes = 30 },
    { percent = 100, hold_minutes = 0 },
  ]

  depends_on = [featureflip_flag_environment.test]
}`, proj, description, otherDescription, abortPercent)
}

func TestAccRolloutRamp_lifecycle(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	const rule, def = "featureflip_rollout_ramp.rule", "featureflip_rollout_ramp.default"
	var ruleRampID string
	importID := func(addr string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			return fmt.Sprintf("%s/new-checkout/acctest/%s", proj, s.RootModule().Resources[addr].Primary.ID), nil
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRolloutRampConfig(proj, "null", "beta", "staff"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rule, "status", "Running"),
					resource.TestCheckResourceAttr(rule, "current_step_index", "0"),
					resource.TestCheckResourceAttr(rule, "default_rule", "false"),
					resource.TestCheckResourceAttrSet(rule, "next_step_at"),
					resource.TestCheckResourceAttrPair(rule, "rule_id", "featureflip_flag_environment.test", "rules.0.id"),
					captureAttr(rule, "id", &ruleRampID),
					resource.TestCheckResourceAttr(def, "status", "Running"),
					resource.TestCheckResourceAttr(def, "default_rule", "true"),
					resource.TestCheckResourceAttrSet(def, "target_variation_id"),
				),
			},
			{
				// The ramp's own progress is not drift, and abort_percent changes in place.
				Config: testAccRolloutRampConfig(proj, "0", "beta", "staff"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAttrIs(rule, "id", &ruleRampID),
					resource.TestCheckResourceAttr(rule, "abort_percent", "0"),
					resource.TestCheckResourceAttr(rule, "status", "Running"),
				),
			},
			{
				// Editing another rule plans every rule id unknown. That must not replace
				// the ramp, and the ramped rule is not rewritten, so the ramp keeps running.
				Config: testAccRolloutRampConfig(proj, "0", "beta", "staff only"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAttrIs(rule, "id", &ruleRampID),
					resource.TestCheckResourceAttrPair(rule, "rule_id", "featureflip_flag_environment.test", "rules.0.id"),
					resource.TestCheckResourceAttr(rule, "status", "Running"),
					resource.TestCheckResourceAttr(def, "status", "Running"),
				),
			},
			{
				// Editing the ramped rule itself pauses the ramp, as a hand edit does. The ramp
				// is not replaced, but its state still holds the status read during this
				// plan's refresh: the pause is a side effect of another resource's write.
				Config: testAccRolloutRampConfig(proj, "0", "beta users", "staff only"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAttrIs(rule, "id", &ruleRampID),
					resource.TestCheckResourceAttrPair(rule, "rule_id", "featureflip_flag_environment.test", "rules.0.id"),
				),
			},
			{
				// The next refresh reads the pause.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAttrIs(rule, "id", &ruleRampID),
					resource.TestCheckResourceAttr(rule, "status", "Paused"),
					resource.TestCheckResourceAttr(rule, "paused_reason", "RuleEditedManually"),
				),
			},
			{
				ResourceName:            rule,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateIdFunc:       importID(rule),
				ImportStateVerifyIgnore: []string{"abort_percent"},
			},
			{
				ResourceName:      def,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: importID(def),
				// The API does not report bucket_by; it was never set here anyway.
				ImportStateVerifyIgnore: []string{"bucket_by"},
			},
		},
	})
}
