package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

func webhookAccBase(proj string) string {
	return fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Webhook Acc"
}

resource "featureflip_environment" "acc" {
  project = featureflip_project.test.key
  key     = "acctest"
  name    = "Acc Test Env"
}
`, proj)
}

func TestAccWebhook_basic(t *testing.T) {
	base := webhookAccBase(acctest.RandomWithPrefix("tf-acc-proj"))
	const res = "featureflip_webhook.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + `
resource "featureflip_webhook" "test" {
  name            = "audit"
  url             = "https://example.com/featureflip"
  event_types     = ["flag.toggled", "flag.updated"]
  project_ids     = [featureflip_project.test.id]
  environment_ids = [featureflip_environment.acc.id]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "id"),
					resource.TestCheckResourceAttrSet(res, "secret"),
					resource.TestCheckResourceAttrSet(res, "secret_id"),
					resource.TestCheckResourceAttr(res, "type", "GenericHttp"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "event_types.#", "2"),
					resource.TestCheckResourceAttrPair(res, "project_ids.0", "featureflip_project.test", "id"),
					resource.TestCheckResourceAttrPair(res, "environment_ids.0", "featureflip_environment.acc", "id"),
					resource.TestCheckNoResourceAttr(res, "auto_disabled_at"),
				),
			},
			{
				// Disable, widen to every event type, and drop the environment filter.
				Config: base + `
resource "featureflip_webhook" "test" {
  name        = "audit-renamed"
  url         = "https://example.com/featureflip/v2"
  enabled     = false
  project_ids = [featureflip_project.test.id]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "name", "audit-renamed"),
					resource.TestCheckResourceAttr(res, "url", "https://example.com/featureflip/v2"),
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckNoResourceAttr(res, "event_types.#"),
					resource.TestCheckNoResourceAttr(res, "environment_ids.#"),
					// the creation secret must survive updates untouched
					resource.TestCheckResourceAttrSet(res, "secret"),
				),
			},
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
			{
				// enabled omitted → back to the default, true.
				Config: base + `
resource "featureflip_webhook" "test" {
  name        = "audit-renamed"
  url         = "https://example.com/featureflip/v2"
  project_ids = [featureflip_project.test.id]
}`,
				Check: resource.TestCheckResourceAttr(res, "enabled", "true"),
			},
		},
	})
}

func TestAccWebhook_createDisabled(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "featureflip_webhook" "test" {
  name    = "starts-disabled"
  url     = "https://example.com/featureflip"
  enabled = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_webhook.test", "enabled", "false"),
					resource.TestCheckResourceAttrSet("featureflip_webhook.test", "secret"),
				),
			},
		},
	})
}

func TestAccWebhook_rejectsInvalidURL(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "featureflip_webhook" "test" {
  name = "metadata"
  url  = "https://169.254.169.254/latest"
}`,
				ExpectError: regexp.MustCompile(`public internet`),
			},
		},
	})
}

// TestAccWebhookSecret_rotation walks the documented rotation: add a second
// secret, adopt the creation secret by import and retire it, then remove the
// last one (which upstream refuses, so the provider must only warn).
func TestAccWebhookSecret_rotation(t *testing.T) {
	webhook := `
resource "featureflip_webhook" "test" {
  name = "rotating"
  url  = "https://example.com/featureflip"
}
`
	next := `
resource "featureflip_webhook_secret" "next" {
  webhook_id = featureflip_webhook.test.id
}
`
	adoptCreation := `
import {
  to = featureflip_webhook_secret.creation
  id = "${featureflip_webhook.test.id}/${featureflip_webhook.test.secret_id}"
}

resource "featureflip_webhook_secret" "creation" {
  webhook_id = featureflip_webhook.test.id
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: webhook + next,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("featureflip_webhook_secret.next", "secret"),
					resource.TestCheckResourceAttrSet("featureflip_webhook_secret.next", "created_at"),
					resource.TestCheckResourceAttrPair("featureflip_webhook_secret.next", "webhook_id", "featureflip_webhook.test", "id"),
					testAccWebhookActiveSecrets("featureflip_webhook.test", 2),
				),
			},
			{
				Config: webhook + next + adoptCreation,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("featureflip_webhook_secret.creation", "id", "featureflip_webhook.test", "secret_id"),
					resource.TestCheckNoResourceAttr("featureflip_webhook_secret.creation", "secret"),
				),
			},
			{
				// Dropping the adopted resource retires the creation secret.
				Config: webhook + next,
				Check:  testAccWebhookActiveSecrets("featureflip_webhook.test", 1),
			},
			{
				// "next" is now the last active secret: upstream refuses to
				// retire it, the provider warns and drops it from state.
				Config: webhook,
				Check:  testAccWebhookActiveSecrets("featureflip_webhook.test", 1),
			},
		},
	})
}

func TestAccWebhookEventTypesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "featureflip_webhook_event_types" "all" {}`,
				Check: resource.TestCheckTypeSetElemAttr(
					"data.featureflip_webhook_event_types.all", "event_types.*", "flag.toggled"),
			},
		},
	})
}

// testAccWebhookActiveSecrets asserts, against the live API, how many
// unretired signing secrets the webhook has.
func testAccWebhookActiveSecrets(name string, want int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not found in state", name)
		}
		c, err := client.New(os.Getenv("FEATUREFLIP_BASE_URL"), os.Getenv("FEATUREFLIP_TOKEN"), os.Getenv("FEATUREFLIP_ORGANIZATION"), "acctest")
		if err != nil {
			return err
		}
		w, err := c.GetWebhook(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		active := 0
		for _, sec := range w.Secrets {
			if sec.RetiredAt == nil {
				active++
			}
		}
		if active != want {
			return fmt.Errorf("%s has %d active signing secrets, want %d", name, active, want)
		}
		return nil
	}
}
