package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccFlagTriggerConfig(proj, name, enabled, keepers string) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Trigger Acc"
}

resource "featureflip_environment" "prod" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}

resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "kill-switch"
  name    = "Kill Switch"
  type    = "Boolean"
}

resource "featureflip_flag_trigger" "test" {
  project     = featureflip_project.test.key
  flag        = featureflip_feature_flag.test.key
  environment = featureflip_environment.prod.key
  name        = %q
  action      = "Disable"
  enabled     = %s
  %s
}`, proj, name, enabled, keepers)
}

// captureAttr stores a resource attribute so a later step can compare against it.
func captureAttr(addr, attr string, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not in state", addr)
		}
		*into = rs.Primary.Attributes[attr]
		return nil
	}
}

func checkAttrNot(addr, attr string, old *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if got := s.RootModule().Resources[addr].Primary.Attributes[attr]; got == *old {
			return fmt.Errorf("%s.%s is still %q", addr, attr, got)
		}
		return nil
	}
}

func checkAttrIs(addr, attr string, want *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if got := s.RootModule().Resources[addr].Primary.Attributes[attr]; got != *want {
			return fmt.Errorf("%s.%s = %q, want %q", addr, attr, got, *want)
		}
		return nil
	}
}

// firesWith POSTs to a trigger URL and checks the status code.
func firesWith(url *string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		resp, err := http.Post(*url, "application/json", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			return fmt.Errorf("POST trigger URL: status %d, want %d", resp.StatusCode, want)
		}
		return nil
	}
}

func TestAccFlagTrigger_lifecycle(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	const addr = "featureflip_flag_trigger.test"
	var firstURL, firstSecret, id, rotatedURL string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFlagTriggerConfig(proj, "grafana", "true", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "url"),
					resource.TestCheckResourceAttrSet(addr, "secret"),
					resource.TestCheckResourceAttrSet(addr, "endpoint"),
					resource.TestCheckResourceAttrSet(addr, "secret_last_four"),
					resource.TestCheckResourceAttr(addr, "enabled", "true"),
					captureAttr(addr, "url", &firstURL),
					captureAttr(addr, "secret", &firstSecret),
					captureAttr(addr, "id", &id),
					// The flag is already off, so this is an "unchanged" firing, but a 200 proves the URL works.
					firesWith(&firstURL, http.StatusOK),
				),
			},
			{
				// Rename and disable: no rotation, the secret survives, and the URL stops working.
				Config: testAccFlagTriggerConfig(proj, "grafana-renamed", "false", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", "grafana-renamed"),
					resource.TestCheckResourceAttr(addr, "enabled", "false"),
					checkAttrIs(addr, "secret", &firstSecret),
					firesWith(&firstURL, http.StatusNotFound),
				),
			},
			{
				// Re-enable and rotate in one apply: same trigger, new URL, old URL dead.
				Config: testAccFlagTriggerConfig(proj, "grafana-renamed", "true", `rotation_keepers = { rotated = "1" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAttrIs(addr, "id", &id),
					checkAttrNot(addr, "secret", &firstSecret),
					checkAttrNot(addr, "url", &firstURL),
					firesWith(&firstURL, http.StatusNotFound),
					captureAttr(addr, "url", &rotatedURL),
					firesWith(&rotatedURL, http.StatusOK),
				),
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources[addr]
					return fmt.Sprintf("%s/kill-switch/acctest/%s", proj, rs.Primary.ID), nil
				},
				// The URL, endpoint and secret come back only from create and rotate; keepers are config-only.
				ImportStateVerifyIgnore: []string{"url", "endpoint", "secret", "rotation_keepers"},
			},
		},
	})
}
