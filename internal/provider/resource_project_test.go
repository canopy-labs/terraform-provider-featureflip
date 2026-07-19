package provider

import (
	"fmt"
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
