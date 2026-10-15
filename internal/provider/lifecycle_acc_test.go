package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

// testAccOwnerEmail is an active member's email, or skips the test.
func testAccOwnerEmail(t *testing.T) string {
	t.Helper()
	owner := os.Getenv("FEATUREFLIP_ACC_OWNER_EMAIL")
	if owner == "" {
		t.Skip("FEATUREFLIP_ACC_OWNER_EMAIL not set — re-run scripts/local-api.sh")
	}
	return owner
}

// testAccUseMemberToken switches the test to a member's personal access
// token, the kind the API makes the default owner of the flags it creates.
// It returns that member's email.
func testAccUseMemberToken(t *testing.T) string {
	t.Helper()
	token := os.Getenv("FEATUREFLIP_ACC_MEMBER_TOKEN")
	if token == "" {
		t.Skip("FEATUREFLIP_ACC_MEMBER_TOKEN not set — re-run scripts/local-api.sh")
	}
	owner := testAccOwnerEmail(t)
	t.Setenv("FEATUREFLIP_TOKEN", token)
	return owner
}

// testAccClient talks to the API outside Terraform, the way a dashboard edit would.
func testAccClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(os.Getenv("FEATUREFLIP_BASE_URL"), os.Getenv("FEATUREFLIP_TOKEN"), os.Getenv("FEATUREFLIP_ORGANIZATION"), "acctest")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testAccCheckOwner checks the flag's owner in the API itself.
func testAccCheckOwner(t *testing.T, project, key, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f, err := testAccClient(t).GetFlag(context.Background(), project, key)
		if err != nil {
			return err
		}
		if f.Owner == nil || !strings.EqualFold(f.Owner.Email, want) {
			return fmt.Errorf("flag %q: owner = %+v, want %s", key, f.Owner, want)
		}
		return nil
	}
}

func mustDo(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// A v0.3.0 configuration has none of the lifecycle attributes. After the
// upgrade it must plan clean, whatever the API or the dashboard set: the
// creator-as-owner default, an owner change, a permanent marker, a project
// default lifetime, and an owner on an archived flag.
func TestAccLifecycle_upgradeFromV030(t *testing.T) {
	member := testAccUseMemberToken(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	config := fmt.Sprintf(`
resource "featureflip_project" "p" {
  key  = %q
  name = "Upgrade Acc"
}
resource "featureflip_feature_flag" "plain" {
  project = featureflip_project.p.key
  key     = "plain"
  name    = "Plain"
  type    = "Boolean"
}
resource "featureflip_feature_flag" "exp" {
  project    = featureflip_project.p.key
  key        = "exp"
  name       = "Exp"
  type       = "Boolean"
  expires_at = "2099-05-01T00:00:00Z"
}
resource "featureflip_feature_flag" "kill_switch" {
  project = featureflip_project.p.key
  key     = "kill-switch"
  name    = "Kill Switch"
  type    = "Boolean"
}
resource "featureflip_feature_flag" "retired" {
  project  = featureflip_project.p.key
  key      = "retired"
  name     = "Retired"
  type     = "Boolean"
  archived = true
}`, proj)
	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		Steps: []resource.TestStep{
			{
				ExternalProviders: map[string]resource.ExternalProvider{
					"featureflip": {Source: "registry.terraform.io/canopy-labs/featureflip", VersionConstraint: "0.3.0"},
				},
				Config: config,
				// The API made the token's user the owner of every flag v0.3.0 created.
				Check: testAccCheckOwner(t, proj, "plain", member),
			},
			{
				// Dashboard edits, then the new provider: no changes.
				PreConfig: func() {
					c, ctx := testAccClient(t), context.Background()
					mustDo(t, "mark kill-switch permanent", c.SetFlagPermanent(ctx, proj, "kill-switch"))
					mustDo(t, "clear exp's owner", c.ClearFlagOwner(ctx, proj, "exp"))
					days := 60
					mustDo(t, "set the project default lifetime", c.SetDefaultFlagLifetime(ctx, proj, &days))
				},
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   config,
				PlanOnly:                 true,
			},
			{
				// An apply with the new provider changes nothing either.
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.plain", "owner_email", member),
					resource.TestCheckResourceAttr("featureflip_feature_flag.retired", "owner_email", member),
					resource.TestCheckResourceAttr("featureflip_feature_flag.exp", "owner_email", ""),
					resource.TestCheckResourceAttr("featureflip_feature_flag.kill_switch", "permanent", "true"),
					resource.TestCheckResourceAttr("featureflip_project.p", "default_flag_lifetime_days", "60"),
					testAccCheckOwner(t, proj, "plain", member),
					testAccCheckOwner(t, proj, "retired", member),
				),
			},
		},
	})
}

// owner_email = "", permanent = false and default_flag_lifetime_days = 0 clear
// their values, and the replan after each apply is clean.
func TestAccLifecycle_explicitClearing(t *testing.T) {
	owner := testAccOwnerEmail(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	cfg := func(project, flag string) string {
		return fmt.Sprintf(`
resource "featureflip_project" "p" {
  key  = %q
  name = "Clearing Acc"
  %s
}
resource "featureflip_feature_flag" "f" {
  project = featureflip_project.p.key
  key     = "f"
  name    = "F"
  type    = "Boolean"
  %s
}
data "featureflip_feature_flag" "f" {
  project = featureflip_feature_flag.f.project
  key     = featureflip_feature_flag.f.key
}
data "featureflip_project" "p" {
  key = featureflip_project.p.key
}`, proj, project, flag)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("default_flag_lifetime_days = 30", fmt.Sprintf("owner_email = %q\n  permanent = true", owner)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.f", "owner_email", owner),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.f", "permanent", "true"),
					resource.TestCheckResourceAttr("data.featureflip_project.p", "default_flag_lifetime_days", "30"),
				),
			},
			{
				Config: cfg("default_flag_lifetime_days = 0", `owner_email = ""`+"\n  permanent = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("featureflip_feature_flag.f", "owner_email", ""),
					resource.TestCheckResourceAttr("featureflip_feature_flag.f", "permanent", "false"),
					resource.TestCheckResourceAttr("featureflip_project.p", "default_flag_lifetime_days", "0"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.f", "owner_email"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.f", "permanent", "false"),
					resource.TestCheckNoResourceAttr("data.featureflip_project.p", "default_flag_lifetime_days"),
				),
			},
			{
				// Set again outside Terraform: the explicit values put them back.
				PreConfig: func() {
					c, ctx := testAccClient(t), context.Background()
					mustDo(t, "set the owner", c.SetFlagOwner(ctx, proj, "f", owner))
					mustDo(t, "mark permanent", c.SetFlagPermanent(ctx, proj, "f"))
					days := 90
					mustDo(t, "set the default lifetime", c.SetDefaultFlagLifetime(ctx, proj, &days))
				},
				Config: cfg("default_flag_lifetime_days = 0", `owner_email = ""`+"\n  permanent = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.f", "owner_email"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.f", "permanent", "false"),
					resource.TestCheckNoResourceAttr("data.featureflip_project.p", "default_flag_lifetime_days"),
				),
			},
		},
	})
}

// With a member's token the API makes the creator the owner. Omitting
// owner_email keeps that owner; owner_email = "" clears it on create.
func TestAccLifecycle_createdByMember(t *testing.T) {
	member := testAccUseMemberToken(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccFlagProjectConfig(proj) + `
resource "featureflip_feature_flag" "defaulted" {
  project = featureflip_project.test.key
  key     = "defaulted"
  name    = "Defaulted"
  type    = "Boolean"
}
resource "featureflip_feature_flag" "unowned" {
  project     = featureflip_project.test.key
  key         = "unowned"
  name        = "Unowned"
  type        = "Boolean"
  owner_email = ""
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("featureflip_feature_flag.defaulted", "owner_email", member),
				resource.TestCheckResourceAttr("featureflip_feature_flag.defaulted", "permanent", "false"),
				resource.TestCheckResourceAttr("featureflip_feature_flag.unowned", "owner_email", ""),
			),
		}},
	})
}

// Archived flags are read-only upstream except for clearing the owner,
// permanent marker and expiry. Those clears must apply (the flag PUT is not
// sent) and replan clean; anything else is refused at plan time.
func TestAccLifecycle_archivedFlag(t *testing.T) {
	owner := testAccOwnerEmail(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	cfg := func(name string, archived bool, perm, exp string) string {
		return testAccFlagProjectConfig(proj) + fmt.Sprintf(`
resource "featureflip_feature_flag" "perm" {
  project  = featureflip_project.test.key
  key      = "perm"
  name     = %[1]q
  type     = "Boolean"
  archived = %[2]t
  %[3]s
}
resource "featureflip_feature_flag" "exp" {
  project  = featureflip_project.test.key
  key      = "exp"
  name     = "Exp"
  type     = "Boolean"
  archived = %[2]t
  %[4]s
}
data "featureflip_feature_flag" "perm" {
  project = featureflip_feature_flag.perm.project
  key     = featureflip_feature_flag.perm.key
}
data "featureflip_feature_flag" "exp" {
  project = featureflip_feature_flag.exp.project
  key     = featureflip_feature_flag.exp.key
}`, name, archived, perm, exp)
	}
	owned := fmt.Sprintf("owner_email = %q", owner)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("Perm", false, owned+"\n  permanent = true", owned+"\n  expires_at = \"2099-01-31T00:00:00Z\""),
			},
			{
				// Archive and rename together: the PUT goes first, then the archive.
				Config: cfg("Perm Archived", true, owned+"\n  permanent = true", owned+"\n  expires_at = \"2099-01-31T00:00:00Z\""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "archived", "true"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "name", "Perm Archived"),
				),
			},
			{
				// Clear owner, permanent and expiry on the archived flags.
				Config: cfg("Perm Archived", true, `owner_email = ""`+"\n  permanent = false", `owner_email = ""`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "archived", "true"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.perm", "owner_email"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "permanent", "false"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.exp", "owner_email"),
					resource.TestCheckNoResourceAttr("data.featureflip_feature_flag.exp", "expires_at"),
				),
			},
			{
				// Setting a value on a flag that stays archived is refused before any call.
				Config:      cfg("Perm Archived", true, owned+"\n  permanent = false", `owner_email = ""`),
				ExpectError: regexp.MustCompile(`(?s)is archived.*also\s+changes\s+owner_email`),
			},
			{
				Config:      cfg("Renamed While Archived", true, `owner_email = ""`+"\n  permanent = false", `owner_email = ""`),
				ExpectError: regexp.MustCompile(`(?s)is archived.*also\s+changes\s+name,\s+description,\s+tags\s+or\s+client_side_visible`),
			},
			{
				// Unarchive, rename and set an owner together: the restore goes first.
				Config: cfg("Perm Restored", false, owned+"\n  permanent = true", `owner_email = ""`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "archived", "false"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "name", "Perm Restored"),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "owner_email", owner),
					resource.TestCheckResourceAttr("data.featureflip_feature_flag.perm", "permanent", "true"),
				),
			},
		},
	})
}

// An owner_email that differs from the API's stored case only in case plans
// clean, after a config edit and after an import.
func TestAccLifecycle_ownerEmailCase(t *testing.T) {
	owner := testAccOwnerEmail(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	flag := func(email string) string {
		return testAccFlagProjectConfig(proj) + fmt.Sprintf(`
resource "featureflip_feature_flag" "test" {
  project     = featureflip_project.test.key
  key         = "cased"
  name        = "Cased"
  type        = "Boolean"
  owner_email = %q
}`, email)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: flag(strings.ToLower(owner))},
			{
				Config:   flag(strings.ToUpper(owner)),
				PlanOnly: true,
			},
			{
				// Imported state holds the stored case; the config's case must not diff.
				Config:          flag(strings.ToUpper(owner)),
				ResourceName:    "featureflip_feature_flag.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   proj + "/cased",
			},
		},
	})
}

// Importing resources whose lifecycle values are set plans no change, both
// when config names the values and when it leaves them unmanaged.
func TestAccLifecycle_importWithValues(t *testing.T) {
	owner := testAccOwnerEmail(t)
	proj := acctest.RandomWithPrefix("tf-acc-proj")
	config := fmt.Sprintf(`
resource "featureflip_project" "test" {
  key  = %q
  name = "Import Acc"
}
resource "featureflip_feature_flag" "managed" {
  project     = featureflip_project.test.key
  key         = "managed"
  name        = "Managed"
  type        = "Boolean"
  owner_email = %q
  permanent   = true
}
resource "featureflip_feature_flag" "unmanaged" {
  project = featureflip_project.test.key
  key     = "unmanaged"
  name    = "Unmanaged"
  type    = "Boolean"
}`, proj, owner)
	importStep := func(address, id string) resource.TestStep {
		return resource.TestStep{
			Config:          config,
			ResourceName:    address,
			ImportState:     true,
			ImportStateKind: resource.ImportBlockWithID,
			ImportStateId:   id,
		}
	}
	setOutOfBand := resource.TestStep{
		// Set in the dashboard, never in config.
		PreConfig: func() {
			c, ctx := testAccClient(t), context.Background()
			mustDo(t, "set the owner", c.SetFlagOwner(ctx, proj, "unmanaged", owner))
			mustDo(t, "mark permanent", c.SetFlagPermanent(ctx, proj, "unmanaged"))
			days := 45
			mustDo(t, "set the default lifetime", c.SetDefaultFlagLifetime(ctx, proj, &days))
		},
		Config:   config,
		PlanOnly: true,
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			setOutOfBand,
			importStep("featureflip_feature_flag.managed", proj+"/managed"),
			importStep("featureflip_feature_flag.unmanaged", proj+"/unmanaged"),
			importStep("featureflip_project.test", proj),
			{
				ResourceName:      "featureflip_feature_flag.managed",
				ImportState:       true,
				ImportStateId:     proj + "/managed",
				ImportStateVerify: true,
			},
		},
	})
}
