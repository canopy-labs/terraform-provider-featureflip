package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccFlagProjectConfig(proj string) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Flag Acc"
}`, proj)
}

func TestAccFeatureFlag_boolean(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "new-checkout"
  name    = "New Checkout"
  type    = "Boolean"
  tags    = ["checkout", "q3"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Boolean flags get auto-seeded true/false variations.
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "variations.#", "2"),
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "archived", "false"),
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "tags.#", "2"),
				),
			},
			{
				Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "test" {
  project  = featureflip_project.test.key
  key      = "new-checkout"
  name     = "New Checkout"
  type     = "Boolean"
  archived = true
}`,
				Check: resource.TestCheckResourceAttr("featureflip_feature_flag.test", "archived", "true"),
			},
			{
				ResourceName:      "featureflip_feature_flag.test",
				ImportState:       true,
				ImportStateId:     proj + "/new-checkout",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFeatureFlag_stringVariationsReconcile(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "banner-text"
  name    = "Banner Text"
  type    = "String"
  variations = [
    { key = "control", name = "Control", value = "Welcome!" },
    { key = "variant", name = "Variant", value = "Hi there!" },
  ]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "variations.#", "2"),
					resource.TestCheckResourceAttrSet("featureflip_feature_flag.test", "variations.0.id"),
				),
			},
			{
				// update value of one, drop one, add one → reconcile by key
				Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "banner-text"
  name    = "Banner Text"
  type    = "String"
  variations = [
    { key = "control", name = "Control", value = "Welcome back!" },
    { key = "loud", name = "Loud", value = "HELLO!!!" },
  ]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "variations.#", "2"),
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "variations.0.value", "Welcome back!"),
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "variations.1.key", "loud"),
				),
			},
		},
	})
}

func TestAccFeatureFlag_expiry(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	flag := func(extra string) string {
		return testAccFlagProjectConfig(proj) + fmt.Sprintf(`
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "expiring"
  name    = "Expiring"
  type    = "Boolean"
  %s
}`, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The API answers in UTC; an offset spelling must not read as drift.
				Config: flag(`expires_at = "2099-01-31T02:00:00+02:00"`),
				Check:  resource.TestCheckResourceAttr("featureflip_feature_flag.test", "expires_at", "2099-01-31T02:00:00+02:00"),
			},
			{
				// Moved through PUT .../expiry alongside an ordinary flag update.
				Config: flag(`expires_at = "2099-06-30T00:00:00Z"`) + `
data "featureflip_feature_flag" "test" {
  project = featureflip_feature_flag.test.project
  key     = featureflip_feature_flag.test.key
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "expires_at", "2099-06-30T00:00:00Z"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "expires_at", "2099-06-30T00:00:00Z"),
				),
			},
			{
				ResourceName:      "featureflip_feature_flag.test",
				ImportState:       true,
				ImportStateId:     proj + "/expiring",
				ImportStateVerify: true,
			},
			{
				// Past dates can't be rejected at plan time (plans apply later);
				// the API refuses them on apply and the hint says what to do.
				Config:      flag(`expires_at = "2020-01-01T00:00:00Z"`),
				ExpectError: regexp.MustCompile(`must be in the future when it is set or changed`),
			},
			{
				// Removing the attribute clears the expiry through DELETE .../expiry.
				Config: flag(""),
				Check:  resource.TestCheckNoResourceAttr("featureflip_feature_flag.test", "expires_at"),
			},
		},
	})
}

func TestAccFeatureFlag_owner(t *testing.T) {
	owner := os.Getenv("FEATUREFLIP_ACC_OWNER_EMAIL")
	if owner == "" {
		t.Skip("FEATUREFLIP_ACC_OWNER_EMAIL not set — re-run scripts/local-api.sh")
	}
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	flag := func(extra string) string {
		return testAccFlagProjectConfig(proj) + fmt.Sprintf(`
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "owned"
  name    = "Owned"
  type    = "Boolean"
  %s
}
data "featureflip_feature_flag" "test" {
  project = featureflip_feature_flag.test.project
  key     = featureflip_feature_flag.test.key
}`, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Upper-cased in config; the API answers in its stored case. No diff either way.
				Config: flag(fmt.Sprintf(`owner_email = %q`, strings.ToUpper(owner))),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "owner_email", strings.ToUpper(owner)),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "owner_email", owner),
				),
			},
			{
				ResourceName:            "featureflip_feature_flag.test",
				ImportState:             true,
				ImportStateId:           proj + "/owned",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"owner_email"}, // import has no prior spelling; it reads the stored case
			},
			{
				Config:      flag(`owner_email = "nobody-here@example.com"`),
				ExpectError: regexp.MustCompile(`active member of the organization`),
			},
			{
				// Removing the attribute leaves the flag unowned (DELETE .../owner).
				Config: flag(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("featureflip_feature_flag.test", "owner_email"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.test", "owner_email"),
				),
			},
		},
	})
}

func TestAccFeatureFlag_permanent(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	flag := func(extra string) string {
		return testAccFlagProjectConfig(proj) + fmt.Sprintf(`
resource "featureflip_feature_flag" "test" {
  project = featureflip_project.test.key
  key     = "kill-switch"
  name    = "Kill Switch"
  type    = "Boolean"
  %s
}
data "featureflip_feature_flag" "test" {
  project = featureflip_feature_flag.test.project
  key     = featureflip_feature_flag.test.key
}`, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: flag(`expires_at = "2099-01-31T00:00:00Z"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "permanent", "false"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "expiry_source", "explicit"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "effective_expires_at", "2099-01-31T00:00:00Z"),
				),
			},
			{
				// Rejected at plan time, before any API call.
				Config:      flag(`expires_at = "2099-01-31T00:00:00Z"` + "\n  permanent = true"),
				ExpectError: regexp.MustCompile(`permanent flag can't have an expiry date`),
			},
			{
				// The switch in one apply: the expiry is cleared, then the flag is marked permanent.
				Config: flag(`permanent = true`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "permanent", "true"),
					resource.TestCheckNoResourceAttr("featureflip_feature_flag.test", "expires_at"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.test", "permanent", "true"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.test", "expiry_source"),
				),
			},
			{
				ResourceName:      "featureflip_feature_flag.test",
				ImportState:       true,
				ImportStateId:     proj + "/kill-switch",
				ImportStateVerify: true,
			},
			{
				// And back: permanent is dropped before the expiry is set.
				Config: flag(`expires_at = "2099-06-30T00:00:00Z"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "permanent", "false"),
					resource.TestCheckResourceAttr("featureflip_feature_flag.test", "expires_at", "2099-06-30T00:00:00Z"),
				),
			},
		},
	})
}

func TestAccFeatureFlag_createdPermanent(t *testing.T) {
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "test" {
  project   = featureflip_project.test.key
  key       = "ops-toggle"
  name      = "Ops Toggle"
  type      = "Boolean"
  permanent = true
}`,
			Check: resource.TestCheckResourceAttr("featureflip_feature_flag.test", "permanent", "true"),
		}},
	})
}
