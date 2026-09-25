package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strp(s string) *string { return &s }

func TestSameExpiryInstant(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"2027-01-31T00:00:00Z", "2027-01-31T00:00:00Z", true},
		{"2027-01-31T02:00:00+02:00", "2027-01-31T00:00:00Z", true},
		{"2027-01-31T00:00:00.000000Z", "2027-01-31T00:00:00Z", true},
		// The API stores microseconds; finer precision is not drift.
		{"2027-01-31T00:00:00.1234567Z", "2027-01-31T00:00:00.123456Z", true},
		{"2027-01-31T00:00:00.000001Z", "2027-01-31T00:00:00Z", false},
		{"2027-01-31T00:00:01Z", "2027-01-31T00:00:00Z", false},
	} {
		if got := sameExpiryInstant(tc.a, tc.b); got != tc.want {
			t.Errorf("sameExpiryInstant(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestExpiryToModel(t *testing.T) {
	// Server echoes the same instant in UTC: the configured spelling is kept.
	prior := types.StringValue("2027-01-31T02:00:00+02:00")
	if got := expiryToModel(prior, strp("2027-01-31T00:00:00Z")); !got.Equal(prior) {
		t.Errorf("same instant must keep prior, got %v", got)
	}
	// Changed out of band: the server value wins so the plan shows the drift.
	if got := expiryToModel(prior, strp("2028-01-01T00:00:00Z")); got.ValueString() != "2028-01-01T00:00:00Z" {
		t.Errorf("different instant must take server value, got %v", got)
	}
	// Import / data source: no prior.
	if got := expiryToModel(types.StringNull(), strp("2027-01-31T00:00:00Z")); got.ValueString() != "2027-01-31T00:00:00Z" {
		t.Errorf("null prior must take server value, got %v", got)
	}
	if got := expiryToModel(prior, nil); !got.IsNull() {
		t.Errorf("no server expiry must be null, got %v", got)
	}
}

func TestExpiryChanged(t *testing.T) {
	a := types.StringValue("2027-01-31T00:00:00Z")
	for _, tc := range []struct {
		name        string
		plan, state types.String
		want        bool
	}{
		{"both null", types.StringNull(), types.StringNull(), false},
		{"set", a, types.StringNull(), true},
		{"cleared", types.StringNull(), a, true},
		{"same instant, other spelling", types.StringValue("2027-01-31T01:00:00+01:00"), a, false},
		// A stored date that has passed is unchanged, so no call is made that
		// could fail with EXPIRY_IN_PAST.
		{"past date unchanged", types.StringValue("2020-01-01T00:00:00Z"), types.StringValue("2020-01-01T00:00:00Z"), false},
		{"moved", types.StringValue("2027-02-01T00:00:00Z"), a, true},
	} {
		if got := expiryChanged(tc.plan, tc.state); got != tc.want {
			t.Errorf("%s: expiryChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExpiryWireValue(t *testing.T) {
	got, err := expiryWireValue(types.StringValue("2027-01-31T02:00:00+02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "2027-01-31T00:00:00Z" {
		t.Errorf("wire value = %q, want UTC", got)
	}
}

func TestRFC3339Validator(t *testing.T) {
	for v, wantErr := range map[string]bool{
		"2027-01-31T00:00:00Z":      false,
		"2027-01-31T00:00:00+02:00": false,
		"2020-01-01T00:00:00Z":      false, // past dates are the API's call, at apply time
		"2027-01-31":                true,
		"31/01/2027":                true,
	} {
		var resp validator.StringResponse
		rfc3339Validator{}.ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("expires_at"), ConfigValue: types.StringValue(v),
		}, &resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("%q: error = %v, want %v", v, resp.Diagnostics.HasError(), wantErr)
		}
	}
}
