package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// baseFlag is an active, unowned, temporary Boolean flag as state holds it.
func baseFlag() featureFlagModel {
	return featureFlagModel{
		ID:                types.StringValue("id"),
		Project:           types.StringValue("web"),
		Key:               types.StringValue("k"),
		Name:              types.StringValue("K"),
		Type:              types.StringValue("Boolean"),
		Description:       types.StringNull(),
		Tags:              types.SetNull(types.StringType),
		ClientSideVisible: types.BoolValue(false),
		Archived:          types.BoolValue(false),
		Variations:        types.ListNull(types.ObjectType{AttrTypes: variationAttrTypes()}),
		ExpiresAt:         types.StringNull(),
		OwnerEmail:        types.StringValue(""),
		Permanent:         types.BoolValue(false),
	}
}

func archivedFlag() featureFlagModel {
	m := baseFlag()
	m.Archived = types.BoolValue(true)
	return m
}

func variationsList(t *testing.T, vs ...variationModel) types.List {
	t.Helper()
	l, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: variationAttrTypes()}, vs)
	if d.HasError() {
		t.Fatal(d)
	}
	return l
}

func variation(id, key, value string) variationModel {
	idv := types.StringUnknown() // computed nested ids are unknown in a plan with changes
	if id != "" {
		idv = types.StringValue(id)
	}
	return variationModel{ID: idv, Key: types.StringValue(key), Name: types.StringValue(key), Value: types.StringValue(value), Description: types.StringNull()}
}

func TestPlanFlagUpdate(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		state, plan func() featureFlagModel
		want        []flagUpdateStep
	}{
		{
			name:  "no change makes no call",
			state: baseFlag, plan: baseFlag,
			want: nil,
		},
		{
			name:  "rename only sends the PUT",
			state: baseFlag,
			plan:  func() featureFlagModel { m := baseFlag(); m.Name = types.StringValue("Renamed"); return m },
			want:  []flagUpdateStep{stepPut},
		},
		{
			name: "tags reordered is no change",
			state: func() featureFlagModel {
				m := baseFlag()
				m.Tags = types.SetValueMust(types.StringType, tagValues("a", "b"))
				return m
			},
			plan: func() featureFlagModel {
				m := baseFlag()
				m.Tags = types.SetValueMust(types.StringType, tagValues("b", "a"))
				return m
			},
			want: nil,
		},
		{
			// The archived-flag bug: lifecycle changes must not send the flag PUT.
			name:  "owner change on an archived flag uses only the owner endpoint",
			state: func() featureFlagModel { m := archivedFlag(); m.OwnerEmail = types.StringValue("a@b.c"); return m },
			plan:  archivedFlag,
			want:  []flagUpdateStep{stepClearOwner},
		},
		{
			name:  "lifecycle clears on an archived flag use only their endpoints",
			state: func() featureFlagModel { m := archivedFlag(); m.Permanent = types.BoolValue(true); return m },
			plan:  archivedFlag,
			want:  []flagUpdateStep{stepClearPermanent},
		},
		{
			name: "expiry cleared on an archived flag",
			state: func() featureFlagModel {
				m := archivedFlag()
				m.ExpiresAt = types.StringValue("2099-01-01T00:00:00Z")
				return m
			},
			plan: archivedFlag,
			want: []flagUpdateStep{stepClearExpiry},
		},
		{
			name:  "unarchive and rename: restore first, then PUT",
			state: archivedFlag,
			plan:  func() featureFlagModel { m := baseFlag(); m.Name = types.StringValue("Renamed"); return m },
			want:  []flagUpdateStep{stepRestore, stepPut},
		},
		{
			name:  "archive and rename: PUT first, then archive",
			state: baseFlag,
			plan:  func() featureFlagModel { m := archivedFlag(); m.Name = types.StringValue("Renamed"); return m },
			want:  []flagUpdateStep{stepPut, stepArchive},
		},
		{
			name:  "unarchive and set owner: restore first",
			state: archivedFlag,
			plan:  func() featureFlagModel { m := baseFlag(); m.OwnerEmail = types.StringValue("a@b.c"); return m },
			want:  []flagUpdateStep{stepRestore, stepSetOwner},
		},
		{
			name:  "permanent to expiry: drop permanent before setting the expiry",
			state: func() featureFlagModel { m := baseFlag(); m.Permanent = types.BoolValue(true); return m },
			plan: func() featureFlagModel {
				m := baseFlag()
				m.ExpiresAt = types.StringValue("2099-01-01T00:00:00Z")
				return m
			},
			want: []flagUpdateStep{stepClearPermanent, stepSetExpiry},
		},
		{
			name: "expiry to permanent: clear the expiry before marking permanent",
			state: func() featureFlagModel {
				m := baseFlag()
				m.ExpiresAt = types.StringValue("2099-01-01T00:00:00Z")
				return m
			},
			plan: func() featureFlagModel { m := baseFlag(); m.Permanent = types.BoolValue(true); return m },
			want: []flagUpdateStep{stepClearExpiry, stepSetPermanent},
		},
		{
			name: "case-only owner change makes no call",
			state: func() featureFlagModel {
				m := baseFlag()
				m.OwnerEmail = types.StringValue("bob@example.com")
				return m
			},
			plan: func() featureFlagModel {
				m := baseFlag()
				m.OwnerEmail = types.StringValue("Bob@Example.com")
				return m
			},
			want: nil,
		},
		{
			name: "variations unchanged apart from unknown computed ids make no call",
			state: func() featureFlagModel {
				m := baseFlag()
				m.Type = types.StringValue("String")
				m.Variations = variationsList(t, variation("1", "a", "A"), variation("2", "b", "B"))
				return m
			},
			plan: func() featureFlagModel {
				m := baseFlag()
				m.Type = types.StringValue("String")
				m.Variations = variationsList(t, variation("", "b", "B"), variation("", "a", "A"))
				m.Name = types.StringValue("Renamed")
				return m
			},
			want: []flagUpdateStep{stepPut},
		},
		{
			name: "a changed variation value reconciles variations",
			state: func() featureFlagModel {
				m := baseFlag()
				m.Type = types.StringValue("String")
				m.Variations = variationsList(t, variation("1", "a", "A"))
				return m
			},
			plan: func() featureFlagModel {
				m := baseFlag()
				m.Type = types.StringValue("String")
				m.Variations = variationsList(t, variation("", "a", "A2"))
				return m
			},
			want: []flagUpdateStep{stepVariations},
		},
	} {
		var diags diag.Diagnostics
		got := planFlagUpdate(ctx, tc.plan(), tc.state(), &diags)
		if diags.HasError() {
			t.Fatalf("%s: %v", tc.name, diags)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: steps = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func tagValues(tags ...string) []attr.Value {
	out := make([]attr.Value, 0, len(tags))
	for _, tag := range tags {
		out = append(out, types.StringValue(tag))
	}
	return out
}

func TestArchivedFlagRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []flagUpdateStep
		want  []string
	}{
		{"clears are accepted", []flagUpdateStep{stepClearPermanent, stepClearExpiry, stepClearOwner}, nil},
		{"setting an owner is refused", []flagUpdateStep{stepSetOwner}, []string{"owner_email"}},
		{"the flag PUT is refused", []flagUpdateStep{stepPut}, []string{"name, description, tags or client_side_visible"}},
		{"setting permanent and an expiry is refused", []flagUpdateStep{stepSetExpiry, stepSetPermanent}, []string{"expires_at", "permanent"}},
		{"variations are refused", []flagUpdateStep{stepVariations}, []string{"variations"}},
	} {
		if got := archivedFlagRefusals(tc.steps); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestLifecycleAttributesAreOptIn pins the upgrade contract: omitting one of
// these attributes leaves the value alone (Optional + Computed, kept from
// state) instead of clearing it.
func TestLifecycleAttributesAreOptIn(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		r    resource.Resource
		attr string
	}{
		{newFeatureFlagResource(), "owner_email"},
		{newFeatureFlagResource(), "permanent"},
		{newProjectResource(), "default_flag_lifetime_days"},
	} {
		var resp resource.SchemaResponse
		tc.r.Schema(ctx, resource.SchemaRequest{}, &resp)
		a := resp.Schema.Attributes[tc.attr]
		if !a.IsOptional() || !a.IsComputed() {
			t.Errorf("%s must be Optional + Computed, got optional=%v computed=%v", tc.attr, a.IsOptional(), a.IsComputed())
		}
		var desc string
		var keepsState bool
		switch sa := a.(type) {
		case schema.StringAttribute:
			desc, keepsState = sa.Description, sa.Default == nil && len(sa.PlanModifiers) > 0
		case schema.BoolAttribute:
			desc, keepsState = sa.Description, sa.Default == nil && len(sa.PlanModifiers) > 0
		case schema.Int64Attribute:
			desc, keepsState = sa.Description, sa.Default == nil && len(sa.PlanModifiers) > 0
		}
		if !keepsState {
			t.Errorf("%s must have no default and keep its state value (UseStateForUnknown)", tc.attr)
		}
		if !strings.Contains(desc, "Omitting it") {
			t.Errorf("%s: the description must say what omitting it does, got %q", tc.attr, desc)
		}
	}
}
