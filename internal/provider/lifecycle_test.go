package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
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
		{"set", types.Int64Value(30), types.Int64Null(), true},
		{"removed", types.Int64Null(), types.Int64Value(30), true},
		{"same", types.Int64Value(30), types.Int64Value(30), false},
		{"moved", types.Int64Value(60), types.Int64Value(30), true},
		{"unknown", types.Int64Unknown(), types.Int64Value(30), false},
	} {
		if got := lifetimeChanged(tc.plan, tc.state); got != tc.want {
			t.Errorf("%s: lifetimeChanged = %v, want %v", tc.name, got, tc.want)
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
	if !shouldClearDefaultOwner(types.StringNull(), owned) {
		t.Error("omitted owner_email with an API-defaulted owner must be cleared")
	}
	if shouldClearDefaultOwner(types.StringNull(), unowned) {
		t.Error("nothing to clear when the API set no owner (service token)")
	}
	if shouldClearDefaultOwner(types.StringValue("a@b.c"), owned) {
		t.Error("a configured owner must never be cleared")
	}
	if shouldClearDefaultOwner(types.StringUnknown(), owned) {
		t.Error("an unknown owner must not be cleared")
	}
}
