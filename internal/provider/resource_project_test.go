package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProject_basic(t *testing.T) {
	key := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key         = %[1]q
  name        = "Acc Test"
  description = "created by acceptance tests"
}`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_project.test", "key", key),
					resource.TestCheckResourceAttr("featureflip_project.test", "description", "created by acceptance tests"),
					resource.TestCheckResourceAttrSet("featureflip_project.test", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Acc Test Renamed"
}`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_project.test", "name", "Acc Test Renamed"),
					resource.TestCheckNoResourceAttr("featureflip_project.test", "description"),
				),
			},
			{
				ResourceName:      "featureflip_project.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccProject_noDescription(t *testing.T) {
	key := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Acc Test No Description"
}`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_project.test", "key", key),
					resource.TestCheckNoResourceAttr("featureflip_project.test", "description"),
					resource.TestCheckResourceAttrSet("featureflip_project.test", "id"),
				),
			},
		},
	})
}

func TestAccProject_defaultFlagLifetime(t *testing.T) {
	key := acctest.RandomWithPrefix("tf-acc-proj")
	cfg := func(extra string) string {
		return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Lifetime Acc"
  %s
}
data "featureflip_project" "test" {
  key = featureflip_project.test.key
}`, key, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// No default: no lifetime call is made, and the attribute stays null.
				Config: cfg(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("featureflip_project.test", "default_flag_lifetime_days"),
					resource.TestCheckNoResourceAttr("data.featureflip_project.test", "default_flag_lifetime_days"),
				),
			},
			{
				Config: cfg("default_flag_lifetime_days = 90"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_project.test", "default_flag_lifetime_days", "90"),
					resource.TestCheckResourceAttr("data.featureflip_project.test", "default_flag_lifetime_days", "90"),
				),
			},
			{
				ResourceName:      "featureflip_project.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
			},
			{
				Config: cfg("default_flag_lifetime_days = 30"),
				Check:  resource.TestCheckResourceAttr("featureflip_project.test", "default_flag_lifetime_days", "30"),
			},
			{
				Config:      cfg("default_flag_lifetime_days = 0"),
				ExpectError: regexp.MustCompile(`between 1 and 3650`),
			},
			{
				// Removing the attribute removes the default ({"days": null}).
				Config: cfg(""),
				Check:  resource.TestCheckNoResourceAttr("featureflip_project.test", "default_flag_lifetime_days"),
			},
		},
	})
}

func TestAccProject_createdWithDefaultFlagLifetime(t *testing.T) {
	key := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key                        = %q
  name                       = "Lifetime On Create"
  default_flag_lifetime_days = 60
}`, key),
			Check: resource.TestCheckResourceAttr("featureflip_project.test", "default_flag_lifetime_days", "60"),
		}},
	})
}
