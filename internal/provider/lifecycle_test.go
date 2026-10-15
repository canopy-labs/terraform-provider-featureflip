package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

func TestOwnerToModel(t *testing.T) {
	prior := types.StringValue("Jane@Example.com")
	if got := ownerToModel(prior, &client.FlagOwner{Email: "jane@example.com"}); !got.Equal(prior) {
		t.Errorf("same email in another case must keep the configured spelling, got %v", got)
	}
	if got := ownerToModel(prior, &client.FlagOwner{Email: "bob@example.com"}); got.ValueString() != "bob@example.com" {
		t.Errorf("a different owner must take the server value, got %v", got)
	}
	if got := ownerToModel(prior, nil); !got.IsNull() {
		t.Errorf("unowned must be null, got %v", got)
	}
	if got := ownerToModel(types.StringNull(), &client.FlagOwner{Email: "a@b.c"}); got.ValueString() != "a@b.c" {
		t.Errorf("import / no prior must take the server value, got %v", got)
	}
}

func TestOwnerChanged(t *testing.T) {
	a := types.StringValue("a@b.c")
	for _, tc := range []struct {
		name        string
		plan, state types.String
		want        bool
	}{
		{"both null", types.StringNull(), types.StringNull(), false},
		{"set", a, types.StringNull(), true},
		{"cleared", types.StringNull(), a, true},
		{"case only", types.StringValue("A@B.C"), a, false},
		{"moved", types.StringValue("x@b.c"), a, true},
	} {
		if got := ownerChanged(tc.plan, tc.state); got != tc.want {
			t.Errorf("%s: ownerChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPermanentConflictsWithExpiry(t *testing.T) {
	exp := types.StringValue("2099-01-01T00:00:00Z")
	for _, tc := range []struct {
		name string
		perm types.Bool
		exp  types.String
		want bool
	}{
		{"permanent with expiry", types.BoolValue(true), exp, true},
		{"permanent without expiry", types.BoolValue(true), types.StringNull(), false},
		{"temporary with expiry", types.BoolValue(false), exp, false},
		{"permanent unset", types.BoolNull(), exp, false},
		// Unknown at plan time: the API is the backstop at apply.
		{"expiry unknown", types.BoolValue(true), types.StringUnknown(), false},
		{"permanent unknown", types.BoolUnknown(), exp, false},
	} {
		if got := permanentConflictsWithExpiry(tc.perm, tc.exp); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLifetime(t *testing.T) {
	d := 30
	if got := lifetimeToModel(&d); got.ValueInt64() != 30 {
		t.Errorf("lifetimeToModel(30) = %v", got)
	}
	if got := lifetimeToModel(nil); !got.IsNull() {
		t.Errorf("lifetimeToModel(nil) = %v", got)
	}
	for _, tc := range []struct {
		name        string
		plan, state types.Int64
		want        bool
	}{
		// A new project with no default must make no lifetime call at all.
		{"both null", types.Int64Null(), types.Int64Null(), false},
		{"both none", types.Int64Value(0), types.Int64Value(0), false},
		// 0 and null both mean "no default": no call.
		{"none vs null", types.Int64Value(0), types.Int64Null(), false},
		{"set", types.Int64Value(30), types.Int64Value(0), true},
		{"removed", types.Int64Value(0), types.Int64Value(30), true},
		{"same", types.Int64Value(30), types.Int64Value(30), false},
		{"moved", types.Int64Value(60), types.Int64Value(30), true},
		{"unknown", types.Int64Unknown(), types.Int64Value(30), false},
	} {
		if got := lifetimeChanged(tc.plan, tc.state); got != tc.want {
			t.Errorf("%s: lifetimeChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLifetimeWireValue(t *testing.T) {
	if got := lifetimeWireValue(types.Int64Value(0)); got != nil {
		t.Errorf("0 must be sent as null (remove the default), got %v", *got)
	}
	if got := lifetimeWireValue(types.Int64Value(45)); got == nil || *got != 45 {
		t.Errorf("45 must be sent as 45, got %v", got)
	}
}

func TestResourceLifetimeReadsZeroWhenUnset(t *testing.T) {
	// The resource reads "no default" as 0, the value that removes it, so a
	// config of 0 plans clean against a project without one.
	if got := projectToModel(&client.Project{ID: "p", Key: "p", Name: "P"}); got.DefaultFlagLifetimeDays.ValueInt64() != 0 || got.DefaultFlagLifetimeDays.IsNull() {
		t.Errorf("no default must read as 0, got %v", got.DefaultFlagLifetimeDays)
	}
}

func TestLifetimeValidator(t *testing.T) {
	for _, tc := range []struct {
		v  int64
		ok bool
	}{
		{0, true}, // removes the default
		{1, true},
		{3650, true},
		{-1, false},
		{3651, false},
	} {
		var resp validator.Int64Response
		lifetimeValidator().ValidateInt64(context.Background(), validator.Int64Request{Path: path.Root("default_flag_lifetime_days"), ConfigValue: types.Int64Value(tc.v)}, &resp)
		if resp.Diagnostics.HasError() == tc.ok {
			t.Errorf("%d: error=%v, want ok=%v", tc.v, resp.Diagnostics.HasError(), tc.ok)
		}
	}
}

func TestWithLifecycleHint(t *testing.T) {
	apiErr := func(code string) error {
		return &client.APIError{StatusCode: 400, Code: "validation_failed", Message: "x", Fields: map[string][]string{"f": {code}}}
	}
	for _, tc := range []struct {
		code, attr, want string
	}{
		{"EXPIRY_IN_PAST", "expires_at", "must be in the future"},
		{"FEATURE_NOT_ENABLED", "owner_email", "Remove owner_email"},
		{"FEATURE_NOT_ENABLED", "default_flag_lifetime_days", "Remove default_flag_lifetime_days"},
		{"PLAN_FEATURE_UNAVAILABLE", "owner_email", "Pro plan"},
		{"OWNER_NOT_MEMBER", "owner_email", "active member"},
		{"PERMANENT_FLAG_HAS_EXPIRY", "permanent", "Remove expires_at"},
	} {
		if got := withLifecycleHint(apiErr(tc.code), tc.attr); !strings.Contains(got, tc.want) {
			t.Errorf("%s/%s: %q does not contain %q", tc.code, tc.attr, got, tc.want)
		}
	}
	if got := withLifecycleHint(apiErr("SOMETHING_ELSE"), "owner_email"); strings.Contains(got, "\n\n") {
		t.Errorf("unknown codes get no hint, got %q", got)
	}
}

func TestWithLifecycleHintNamesTheRejectedField(t *testing.T) {
	// One create body carries several gated fields; the field key says which was refused.
	for _, tc := range []struct {
		field, passed, want string
	}{
		{"isPermanent", "expires_at", "Remove permanent"},
		{"owner", "expires_at", "Remove owner_email"},
		{"expiresAtUtc", "owner_email", "Remove expires_at"},
		{"days", "expires_at", "Remove default_flag_lifetime_days"},
	} {
		err := &client.APIError{StatusCode: 400, Code: "validation_failed", Message: "x", Fields: map[string][]string{tc.field: {"FEATURE_NOT_ENABLED"}}}
		if got := withLifecycleHint(err, tc.passed); !strings.Contains(got, tc.want) {
			t.Errorf("field %s: %q does not contain %q", tc.field, got, tc.want)
		}
	}
	err := &client.APIError{StatusCode: 400, Code: "validation_failed", Message: "x", Fields: map[string][]string{"isPermanent": {"FEATURE_NOT_ENABLED"}}}
	if got := withLifecycleHint(err, "expires_at"); !strings.Contains(got, "Permanent flags") {
		t.Errorf("the hint must name the feature, got %q", got)
	}
}

func TestOwnerEmailValidator(t *testing.T) {
	for _, tc := range []struct {
		v  string
		ok bool
	}{
		{"jane@example.com", true},
		{"Jane.Doe+ff@Example.co.uk", true},
		// The API trims, so a padded value would come back different and break apply.
		{" jane@example.com", false},
		{"jane@example.com ", false},
		// "" clears the owner.
		{"", true},
		{"   ", false},
		{"not-an-email", false},
	} {
		var resp validator.StringResponse
		ownerEmailValidator().ValidateString(context.Background(), validator.StringRequest{Path: path.Root("owner_email"), ConfigValue: types.StringValue(tc.v)}, &resp)
		if resp.Diagnostics.HasError() == tc.ok {
			t.Errorf("%q: error=%v, want ok=%v", tc.v, resp.Diagnostics.HasError(), tc.ok)
		}
	}
}

func TestShouldClearDefaultOwner(t *testing.T) {
	owned := &client.Flag{Owner: &client.FlagOwner{Email: "a@b.c"}}
	unowned := &client.Flag{}
	if !shouldClearDefaultOwner(types.StringValue(""), owned) {
		t.Error(`owner_email = "" with an API-defaulted owner must be cleared`)
	}
	if shouldClearDefaultOwner(types.StringUnknown(), owned) {
		t.Error("an omitted owner_email (unknown in the plan) must keep the API's default owner")
	}
	if shouldClearDefaultOwner(types.StringNull(), owned) {
		t.Error("a null owner_email must keep the API's default owner")
	}
	if shouldClearDefaultOwner(types.StringValue(""), unowned) {
		t.Error("nothing to clear when the API set no owner (service token)")
	}
	if shouldClearDefaultOwner(types.StringValue("a@b.c"), owned) {
		t.Error("a configured owner must never be cleared")
	}
}

func TestOwnerChangedTreatsEmptyAsUnowned(t *testing.T) {
	for _, tc := range []struct {
		name        string
		plan, state types.String
		want        bool
	}{
		{"clear", types.StringValue(""), types.StringValue("a@b.c"), true},
		{"already clear", types.StringValue(""), types.StringValue(""), false},
		{"empty vs null", types.StringValue(""), types.StringNull(), false},
		{"set from empty", types.StringValue("a@b.c"), types.StringValue(""), true},
	} {
		if got := ownerChanged(tc.plan, tc.state); got != tc.want {
			t.Errorf("%s: ownerChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOwnerEmailCaseInsensitivePlanModifier(t *testing.T) {
	for _, tc := range []struct {
		name          string
		config, state types.String
		want          types.String
	}{
		// The case-only difference keeps the prior value, so there's no diff.
		{"case only", types.StringValue("Bob@Example.com"), types.StringValue("bob@example.com"), types.StringValue("bob@example.com")},
		{"other owner", types.StringValue("jane@example.com"), types.StringValue("bob@example.com"), types.StringValue("jane@example.com")},
		{"clear", types.StringValue(""), types.StringValue("bob@example.com"), types.StringValue("")},
		{"create", types.StringValue("Bob@Example.com"), types.StringNull(), types.StringValue("Bob@Example.com")},
	} {
		req := planmodifier.StringRequest{Path: path.Root("owner_email"), ConfigValue: tc.config, StateValue: tc.state, PlanValue: tc.config}
		resp := planmodifier.StringResponse{PlanValue: tc.config}
		ownerEmailCaseInsensitive{}.PlanModifyString(context.Background(), req, &resp)
		if !resp.PlanValue.Equal(tc.want) {
			t.Errorf("%s: plan = %v, want %v", tc.name, resp.PlanValue, tc.want)
		}
	}
}
