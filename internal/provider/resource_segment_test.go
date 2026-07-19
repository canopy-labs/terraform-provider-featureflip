package provider

import (
	"fmt"
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

// TestAccSegment_slashKeyCreate proves the upstream API accepts a
// slash-bearing segment key at create (segment keys have no upstream format
// constraint, unlike project/flag keys) and that the provider round-trips it
// in state. It does NOT exercise ImportState/splitImportIDTail: direct API
// probing (POST + GET against the local stack) confirmed the server's
// GET-by-key route 404s for any key containing "/", whether the slash is
// sent literally or percent-encoded (%2F) — a server-side route-matching
// gap, not a defect in this provider's request construction (segments.go
// already URL-escapes each path segment individually; see TestOrgPathEscapes
// / TestDoPreservesEscapedPathSegments in internal/client). Terraform's Read
// (and therefore Import) goes through that same GET, so a live import of a
// slash-containing key can't round-trip until upstream fixes the lookup
// route; the same limitation blocks environment and flag_environment imports
// with slashed environment keys. splitImportIDTail's parsing is covered at
// the unit level by TestSplitImportIDTail — see TODO.md for the upstream
// follow-up.
func TestAccSegment_slashKeyCreate(t *testing.T) {
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
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_segment.test", "key", "eu/west"),
					resource.TestCheckResourceAttrSet("featureflip_segment.test", "id"),
				),
			},
		},
	})
}
