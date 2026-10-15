package provider

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func runModifyPlan(t *testing.T, plan, state featureFlagModel) diag.Diagnostics {
	t.Helper()
	r := &featureFlagResource{}
	s := resourceSchema(t, r)
	req := resource.ModifyPlanRequest{Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, plan)}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(context.Background(), req, &resp)
	return resp.Diagnostics
}

// A change to project, key or type replaces the flag: the new one is
// created, so neither the archived-flag nor the permanent-flag check
// applies to it, even though this first plan still carries the old state.
func TestModifyPlanSkipsReplacements(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state, plan func() featureFlagModel
	}{
		{
			name:  "archived flag, key and name change",
			state: archivedFlag,
			plan: func() featureFlagModel {
				m := archivedFlag()
				m.Key, m.Name = types.StringValue("k2"), types.StringValue("Renamed")
				return m
			},
		},
		{
			name:  "archived flag, type change with variations",
			state: archivedFlag,
			plan: func() featureFlagModel {
				m := archivedFlag()
				m.Type = types.StringValue("String")
				m.Variations = variationsList(t, variation("", "a", "A"))
				return m
			},
		},
		{
			name:  "archived flag, project change",
			state: archivedFlag,
			plan: func() featureFlagModel {
				m := archivedFlag()
				m.Project = types.StringValue("other")
				m.Name = types.StringValue("Renamed")
				return m
			},
		},
		{
			name:  "permanent in state, key change with a new expiry",
			state: func() featureFlagModel { m := baseFlag(); m.Permanent = types.BoolValue(true); return m },
			plan: func() featureFlagModel {
				m := baseFlag()
				m.Permanent = types.BoolValue(true) // carried by UseStateForUnknown
				m.Key = types.StringValue("k2")
				m.ExpiresAt = types.StringValue("2099-01-01T00:00:00Z")
				return m
			},
		},
		{
			name:  "unknown key counts as a replacement",
			state: archivedFlag,
			plan: func() featureFlagModel {
				m := archivedFlag()
				m.Key = types.StringUnknown()
				m.Name = types.StringValue("Renamed")
				return m
			},
		},
	} {
		if d := runModifyPlan(t, tc.plan(), tc.state()); d.ErrorsCount() > 0 || d.WarningsCount() > 0 {
			t.Errorf("%s: want no diagnostics, got %v", tc.name, d)
		}
	}
}

func TestModifyPlanStillChecksInPlaceUpdates(t *testing.T) {
	d := runModifyPlan(t, func() featureFlagModel { m := archivedFlag(); m.Name = types.StringValue("Renamed"); return m }(), archivedFlag())
	if !d.HasError() {
		t.Error("renaming a flag that stays archived must be refused at plan time")
	}
	d = runModifyPlan(t, func() featureFlagModel {
		m := baseFlag()
		m.Permanent = types.BoolValue(true)
		m.ExpiresAt = types.StringValue("2099-01-01T00:00:00Z")
		return m
	}(), func() featureFlagModel { m := baseFlag(); m.Permanent = types.BoolValue(true); return m }())
	if !d.HasError() {
		t.Error("an expiry on a flag permanent in state must be refused at plan time")
	} else if got := d.Errors()[0].Detail(); !strings.Contains(got, "permanent is true in state and not set in this configuration") {
		t.Errorf("the conflict message must say where permanent comes from, got %q", got)
	}
}

// A value unknown at plan time may resolve to a change the API refuses on an
// archived flag. It can't be refused yet (it may resolve equal), so it warns.
func TestModifyPlanWarnsAboutUnknownsOnArchivedFlag(t *testing.T) {
	for _, tc := range []struct {
		attr string
		plan func() featureFlagModel
	}{
		{"name", func() featureFlagModel { m := archivedFlag(); m.Name = types.StringUnknown(); return m }},
		{"description", func() featureFlagModel { m := archivedFlag(); m.Description = types.StringUnknown(); return m }},
		{"tags", func() featureFlagModel { m := archivedFlag(); m.Tags = types.SetUnknown(types.StringType); return m }},
		{"client_side_visible", func() featureFlagModel { m := archivedFlag(); m.ClientSideVisible = types.BoolUnknown(); return m }},
		{"expires_at", func() featureFlagModel { m := archivedFlag(); m.ExpiresAt = types.StringUnknown(); return m }},
		{"permanent", func() featureFlagModel { m := archivedFlag(); m.Permanent = types.BoolUnknown(); return m }},
		{"owner_email", func() featureFlagModel { m := archivedFlag(); m.OwnerEmail = types.StringUnknown(); return m }},
		{"variations", func() featureFlagModel {
			m := archivedFlag()
			m.Type = types.StringValue("String")
			v := variation("", "a", "A")
			v.Value = types.StringUnknown()
			m.Variations = variationsList(t, v)
			return m
		}},
	} {
		state := archivedFlag()
		if tc.attr == "variations" {
			state.Type = types.StringValue("String")
			state.Variations = variationsList(t, variation("1", "a", "A"))
		}
		d := runModifyPlan(t, tc.plan(), state)
		if d.HasError() {
			t.Errorf("%s: unknown must not be refused, got %v", tc.attr, d)
			continue
		}
		if d.WarningsCount() != 1 || !strings.Contains(d.Warnings()[0].Detail(), tc.attr) || !strings.Contains(d.Warnings()[0].Detail(), "apply will fail") {
			t.Errorf("%s: want one warning naming it, got %v", tc.attr, d)
		}
	}
	// Only the ids are unknown (always, in a plan with changes): no warning.
	plan := archivedFlag()
	plan.Type = types.StringValue("String")
	plan.Variations = variationsList(t, variation("", "a", "A"))
	plan.OwnerEmail = types.StringValue("")
	state := archivedFlag()
	state.Type = types.StringValue("String")
	state.Variations = variationsList(t, variation("1", "a", "A"))
	state.OwnerEmail = types.StringValue("a@b.c")
	if d := runModifyPlan(t, plan, state); d.ErrorsCount()+d.WarningsCount() > 0 {
		t.Errorf("unknown variation ids alone must not warn, got %v", d)
	}
}

// An attribute the config omits is planned from state. If the server's value
// moves between plan and apply, state must still match the plan; the next
// Read picks up the new value.
func TestUpdateKeepsPlannedValueOfUnmanagedAttributes(t *testing.T) {
	ctx := context.Background()
	r := &featureFlagResource{}
	s := resourceSchema(t, r)
	_, c := newFakeAPI(t, map[string]func(http.ResponseWriter){
		"PUT /projects/web/flags/k": replyStatus(http.StatusNoContent),
		// Someone changed both in the dashboard after the plan was made.
		"GET /projects/web/flags/k": replyJSON(http.StatusOK, apiFlag("someone-else@example.com", false, false)),
	})
	r.client = c

	state := baseFlag()
	state.OwnerEmail, state.Permanent = types.StringValue("a@b.c"), types.BoolValue(true)
	plan := state
	plan.Name = types.StringValue("Renamed")
	config := plan
	config.ID, config.OwnerEmail, config.Permanent = types.StringNull(), types.StringNull(), types.BoolNull()

	req := resource.UpdateRequest{Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, config)}
	resp := resource.UpdateResponse{State: testState(t, s, state)}
	r.Update(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got featureFlagModel
	resp.Diagnostics.Append(resp.State.Get(ctx, &got)...)
	if got.OwnerEmail.ValueString() != "a@b.c" || !got.Permanent.ValueBool() {
		t.Errorf("unmanaged attributes must keep their planned values, got owner=%v permanent=%v", got.OwnerEmail, got.Permanent)
	}
	if got.Name.ValueString() != "K" {
		t.Errorf("managed attributes still come from the server, got name=%v", got.Name)
	}
}

func TestProjectUpdateKeepsPlannedLifetimeWhenUnmanaged(t *testing.T) {
	ctx := context.Background()
	r := &projectResource{}
	s := resourceSchema(t, r)
	_, c := newFakeAPI(t, map[string]func(http.ResponseWriter){
		"PUT /projects/web": replyStatus(http.StatusNoContent),
		"GET /projects/web": replyJSON(http.StatusOK, map[string]any{"id": "p", "key": "web", "name": "Renamed", "defaultFlagLifetimeDays": 60}),
	})
	r.client = c
	state := projectModel{ID: types.StringValue("p"), Key: types.StringValue("web"), Name: types.StringValue("Web"), Description: types.StringNull(), DefaultFlagLifetimeDays: types.Int64Value(30)}
	plan := state
	plan.Name = types.StringValue("Renamed")
	config := plan
	config.ID, config.DefaultFlagLifetimeDays = types.StringNull(), types.Int64Null()

	req := resource.UpdateRequest{Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, config)}
	resp := resource.UpdateResponse{State: testState(t, s, state)}
	r.Update(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got projectModel
	resp.Diagnostics.Append(resp.State.Get(ctx, &got)...)
	if got.DefaultFlagLifetimeDays.ValueInt64() != 30 {
		t.Errorf("an unmanaged lifetime must keep its planned value, got %v", got.DefaultFlagLifetimeDays)
	}
}

// The flag exists once POST succeeds, so a failed archive must still track it
// (Terraform then taints it) rather than orphan it.
func TestCreateTracksFlagWhenArchiveFails(t *testing.T) {
	ctx := context.Background()
	r := &featureFlagResource{}
	s := resourceSchema(t, r)
	api, c := newFakeAPI(t, map[string]func(http.ResponseWriter){
		"POST /projects/web/flags":           replyJSON(http.StatusCreated, apiFlag("member@example.com", false, false)),
		"DELETE /projects/web/flags/k/owner": replyStatus(http.StatusNoContent),
		"POST /projects/web/flags/k/archive": replyJSON(http.StatusBadRequest, map[string]any{"error": "bad_request", "message": "nope"}),
	})
	r.client = c
	plan := archivedFlag()
	plan.ID, plan.Variations = types.StringUnknown(), types.ListUnknown(types.ObjectType{AttrTypes: variationAttrTypes()})
	plan.OwnerEmail, plan.Permanent = types.StringValue(""), types.BoolUnknown()
	config := plan
	config.ID, config.Variations, config.Permanent = types.StringNull(), types.ListNull(types.ObjectType{AttrTypes: variationAttrTypes()}), types.BoolNull()

	req := resource.CreateRequest{Plan: testPlan(t, s, plan), Config: testConfig(t, s, config)}
	resp := resource.CreateResponse{State: tfsdkStateNull(s)}
	r.Create(ctx, req, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a failed archive must fail the create")
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("the created flag must be tracked in state so it is not orphaned")
	}
	var got featureFlagModel
	resp.State.Get(ctx, &got)
	if got.ID.ValueString() != "id" || got.OwnerEmail.ValueString() != "" {
		t.Errorf("tracked state = id %v owner %v", got.ID, got.OwnerEmail)
	}
	if want := "DELETE /projects/web/flags/k/owner"; !strings.Contains(strings.Join(api.calls, ","), want) {
		t.Errorf("owner_email = \"\" must clear the default owner before archiving, calls = %v", api.calls)
	}
}

func TestOwnerEmailValidatorRejectsUnicodeAndVerticalWhitespace(t *testing.T) {
	for _, v := range []string{"jane@example.com ", " jane@example.com", "jane@example.com\v", "jane@example.com ", "　"} {
		var resp validator.StringResponse
		ownerEmailValidator().ValidateString(context.Background(), validator.StringRequest{Path: path.Root("owner_email"), ConfigValue: types.StringValue(v)}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("%q must be rejected: the API trims it, so it would come back different", v)
		}
	}
}

// replacesFlag must cover exactly the attributes that carry RequiresReplace.
func TestReplacesFlagMatchesRequiresReplace(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(t, &featureFlagResource{})
	marker := stringplanmodifier.RequiresReplace().Description(ctx)
	var replacing []string
	for name, a := range s.Attributes {
		sa, ok := a.(schema.StringAttribute)
		if !ok {
			continue
		}
		for _, m := range sa.PlanModifiers {
			if m.Description(ctx) == marker {
				replacing = append(replacing, name)
			}
		}
	}
	sort.Strings(replacing)
	if strings.Join(replacing, ",") != "key,project,type" {
		t.Fatalf("RequiresReplace attributes are %v; update replacesFlag to match", replacing)
	}
	for _, change := range []func(*featureFlagModel){
		func(m *featureFlagModel) { m.Project = types.StringValue("other") },
		func(m *featureFlagModel) { m.Key = types.StringValue("other") },
		func(m *featureFlagModel) { m.Type = types.StringValue("String") },
	} {
		plan := baseFlag()
		change(&plan)
		if !replacesFlag(plan, baseFlag()) {
			t.Errorf("replacesFlag missed a change: %+v", plan)
		}
	}
	plan := baseFlag()
	plan.Name, plan.Archived = types.StringValue("Renamed"), types.BoolValue(true)
	if replacesFlag(plan, baseFlag()) {
		t.Error("an in-place change is not a replacement")
	}
}
