package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSegment_basic(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Segment Acc"
}

resource "featureflip_segment" "test" {
  project = featureflip_project.test.key
  key     = "beta-users"
  name    = "Beta Users"
  conditions = [
    {
      attribute = "email"
      operator  = "EndsWith"
      values    = ["@acme.io"]
    },
    {
      attribute = "country"
      operator  = "In"
      values    = ["DE", "FR"]
      negate    = true
    },
  ]
}`, proj),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_segment.test", "conditions.#", "2"),
					resource.TestCheckResourceAttr("featureflip_segment.test", "conditions.1.negate", "true"),
					resource.TestCheckResourceAttrSet("featureflip_segment.test", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Segment Acc"
}

resource "featureflip_segment" "test" {
  project = featureflip_project.test.key
  key     = "beta-users"
  name    = "Beta Users v2"
  conditions = [
    {
      attribute = "email"
      operator  = "EndsWith"
      values    = ["@acme.io", "@acme.dev"]
    },
  ]
}`, proj),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_segment.test", "conditions.#", "1"),
					resource.TestCheckResourceAttr("featureflip_segment.test", "conditions.0.values.#", "2"),
				),
			},
			{
				ResourceName:      "featureflip_segment.test",
				ImportState:       true,
				ImportStateId:     proj + "/beta-users",
				ImportStateVerify: true,
				// Imported state has null conditions when server returns none-vs-list mismatch is impossible here,
				// but negate defaults materialize — verify tolerates them since Read canonicalizes both sides.
			},
		},
	})
}

// TestAccSegment_slashKeyRejected pins the upstream segment-key format: since
// feature-flagger#1795 the API refuses a key with "/" (or any character
// outside lowercase letters, digits, hyphens and underscores) at create, so
// create and get-by-key agree. It used to accept one that it could not then
// read back.
func TestAccSegment_slashKeyRejected(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %[1]q
  name = "Segment Slash Key Acc"
}

resource "featureflip_segment" "test" {
  project = featureflip_project.test.key
  key     = "eu/west"
  name    = "EU West"
}`, proj),
				ExpectError: regexp.MustCompile(`Segment key must be lowercase alphanumeric with hyphens or\s+underscores`),
			},
		},
	})
}
