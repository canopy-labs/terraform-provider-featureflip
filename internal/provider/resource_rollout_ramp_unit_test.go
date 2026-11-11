package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	rampEnv  = "/projects/p/flags/f/environments/production"
	rampPath = rampEnv + "/ramps/r1"
	ruleID   = "0199a8c4-5b1e-7c2d-9f3a-4e6b8d0c1a2f"
)

func rampSteps(pairs ...int64) types.List {
	elems := make([]attr.Value, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		elems = append(elems, types.ObjectValueMust(rampStepAttrTypes, map[string]attr.Value{
			"percent": types.Int64Value(pairs[i]), "hold_minutes": types.Int64Value(pairs[i+1]),
		}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: rampStepAttrTypes}, elems)
}

func apiRamp(status string, index int64, isDefault bool) map[string]any {
	m := map[string]any{
		"id": "r1", "isDefaultRule": isDefault, "status": status, "currentStepIndex": index,
		"steps":     []map[string]any{{"percent": 10, "holdMinutes": 60}, {"percent": 50, "holdMinutes": 60}, {"percent": 100, "holdMinutes": 0}},
		"createdAt": "2026-11-10T00:00:00Z", "nextStepAt": "2026-11-10T01:00:00Z",
	}
	if isDefault {
		m["targetVariationId"] = "v1"
	} else {
		m["ruleId"] = ruleID
	}
	return m
}

// planRamp is a configured rule ramp with its computed fields unknown, as a plan has them.
func planRamp() rolloutRampModel {
	return rolloutRampModel{
		ID:                types.StringUnknown(),
		Project:           types.StringValue("p"),
		Flag:              types.StringValue("f"),
		Environment:       types.StringValue("production"),
		RuleID:            types.StringValue(ruleID),
		DefaultRule:       types.BoolValue(false),
		Steps:             rampSteps(10, 60, 50, 60, 100, 0),
		Variation:         types.StringNull(),
		BucketBy:          types.StringNull(),
		AbortPercent:      types.Int64Null(),
		Status:            types.StringUnknown(),
		CurrentStepIndex:  types.Int64Unknown(),
		NextStepAt:        types.StringUnknown(),
		PausedReason:      types.StringUnknown(),
		PausedDetail:      types.StringUnknown(),
		TargetVariationID: types.StringUnknown(),
	}
}

func stateRamp(isDefault bool) rolloutRampModel {
	m := planRamp()
	m.ID = types.StringValue("r1")
	if isDefault {
		m.RuleID, m.DefaultRule = types.StringNull(), types.BoolValue(true)
	}
	m.Status, m.CurrentStepIndex = types.StringValue("Running"), types.Int64Value(0)
	m.NextStepAt, m.PausedReason, m.PausedDetail, m.TargetVariationID = types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull()
	return m
}

func runRampCreate(t *testing.T, routes map[string]func(w http.ResponseWriter), plan rolloutRampModel) (*fakeAPI, rolloutRampModel, resource.CreateResponse) {
	t.Helper()
	f, c := newFakeAPI(t, routes)
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: tfsdkStateNull(s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: testPlan(t, s, plan), Config: testConfig(t, s, plan)}, &resp)
	var got rolloutRampModel
	if !resp.Diagnostics.HasError() {
		if d := resp.State.Get(context.Background(), &got); d.HasError() {
			t.Fatal(d)
		}
	}
	return f, got, resp
}

func runRampDelete(t *testing.T, routes map[string]func(w http.ResponseWriter), state rolloutRampModel) (*fakeAPI, resource.DeleteResponse) {
	t.Helper()
	f, c := newFakeAPI(t, routes)
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.DeleteResponse{State: testState(t, s, state)}
	r.Delete(context.Background(), resource.DeleteRequest{State: testState(t, s, state)}, &resp)
	return f, resp
}

func jsonBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	return m
}

func TestRolloutRampCreateStartsOnTheRule(t *testing.T) {
	route := "POST " + rampEnv + "/rules/" + ruleID + "/ramp"
	f, got, resp := runRampCreate(t, map[string]func(w http.ResponseWriter){
		route: replyJSON(201, apiRamp("Running", 0, false)),
	}, planRamp())
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	body := jsonBody(t, f.bodies[route])
	steps, _ := body["steps"].([]any)
	if len(body) != 1 || len(steps) != 3 || steps[0].(map[string]any)["holdMinutes"] != float64(60) {
		t.Errorf("start body = %s", f.bodies[route])
	}
	if got.ID.ValueString() != "r1" || got.Status.ValueString() != "Running" || got.CurrentStepIndex.ValueInt64() != 0 ||
		got.NextStepAt.ValueString() != "2026-11-10T01:00:00Z" || !got.TargetVariationID.IsNull() {
		t.Errorf("state = %+v", got)
	}
}

func TestRolloutRampCreateOnDefaultRuleSendsTheVariationID(t *testing.T) {
	plan := planRamp()
	plan.RuleID, plan.DefaultRule = types.StringNull(), types.BoolValue(true)
	plan.Variation, plan.BucketBy = types.StringValue("true"), types.StringValue("account_id")
	route := "POST " + rampEnv + "/default-rule/ramp"
	f, got, resp := runRampCreate(t, map[string]func(w http.ResponseWriter){
		"GET /projects/p/flags/f": replyJSON(200, apiFlag("", false, false)),
		route:                     replyJSON(201, apiRamp("Running", 0, true)),
	}, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	body := jsonBody(t, f.bodies[route])
	if body["variationId"] != "v1" || body["bucketBy"] != "account_id" {
		t.Errorf("start body = %s", f.bodies[route])
	}
	if got.TargetVariationID.ValueString() != "v1" || got.Variation.ValueString() != "true" {
		t.Errorf("state = %+v", got)
	}
}

func TestRolloutRampCreateDefaultRuleWithoutVariationSendsNulls(t *testing.T) {
	plan := planRamp()
	plan.RuleID, plan.DefaultRule = types.StringNull(), types.BoolValue(true)
	route := "POST " + rampEnv + "/default-rule/ramp"
	f, _, resp := runRampCreate(t, map[string]func(w http.ResponseWriter){
		route: replyJSON(201, apiRamp("Running", 0, true)),
	}, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	body := jsonBody(t, f.bodies[route])
	if v, ok := body["variationId"]; !ok || v != nil {
		t.Errorf("variationId should be an explicit null: %s", f.bodies[route])
	}
}

func TestRolloutRampCreateUnknownVariationKeyStartsNothing(t *testing.T) {
	plan := planRamp()
	plan.RuleID, plan.DefaultRule = types.StringNull(), types.BoolValue(true)
	plan.Variation = types.StringValue("nope")
	f, _, resp := runRampCreate(t, map[string]func(w http.ResponseWriter){
		"GET /projects/p/flags/f": replyJSON(200, apiFlag("", false, false)),
	}, plan)
	if !resp.Diagnostics.HasError() || len(f.calls) != 1 {
		t.Errorf("diags %v calls %v", resp.Diagnostics, f.calls)
	}
}

func TestRolloutRampCreatePlanRefusalSaysWhy(t *testing.T) {
	_, _, resp := runRampCreate(t, map[string]func(w http.ResponseWriter){
		"POST " + rampEnv + "/rules/" + ruleID + "/ramp": replyJSON(400, map[string]any{
			"error": "bad_request", "message": "PLAN_FEATURE_UNAVAILABLE: Rollout ramps need the Team plan.",
		}),
	}, planRamp())
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), "Team plan or above") {
		t.Errorf("diags = %v", resp.Diagnostics)
	}
}

func TestRolloutRampReadRemovesADeletedRamp(t *testing.T) {
	_, c := newFakeAPI(t, map[string]func(w http.ResponseWriter){"GET " + rampPath: replyStatus(404)})
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.ReadResponse{State: testState(t, s, stateRamp(false))}
	r.Read(context.Background(), resource.ReadRequest{State: testState(t, s, stateRamp(false))}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Errorf("diags %v, state null %v", resp.Diagnostics, resp.State.Raw.IsNull())
	}
}

func TestRolloutRampReadRefreshesProgressAndKeepsConfig(t *testing.T) {
	_, c := newFakeAPI(t, map[string]func(w http.ResponseWriter){"GET " + rampPath: replyJSON(200, func() map[string]any {
		m := apiRamp("Paused", 1, false)
		m["pausedReason"], m["nextStepAt"] = "ManualEdit", nil
		return m
	}())})
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	state := stateRamp(false)
	state.Steps = rampSteps(10, 60, 50, 60, 100, 30) // as configured; the API's echo must not replace it
	resp := resource.ReadResponse{State: testState(t, s, state)}
	r.Read(context.Background(), resource.ReadRequest{State: testState(t, s, state)}, &resp)
	var got rolloutRampModel
	resp.State.Get(context.Background(), &got)
	if got.Status.ValueString() != "Paused" || got.CurrentStepIndex.ValueInt64() != 1 || got.PausedReason.ValueString() != "ManualEdit" ||
		!got.NextStepAt.IsNull() || !got.Steps.Equal(state.Steps) {
		t.Errorf("state = %+v", got)
	}
}

func TestRolloutRampImportFillsTheRampFromTheAPI(t *testing.T) {
	_, c := newFakeAPI(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath:         replyJSON(200, apiRamp("Running", 0, true)),
		"GET /projects/p/flags/f": replyJSON(200, apiFlag("", false, false)),
	})
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	imported := tfsdkStateNull(s)
	var ir resource.ImportStateResponse
	ir.State = imported
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "p/f/production/r1"}, &ir)
	if ir.Diagnostics.HasError() {
		t.Fatal(ir.Diagnostics)
	}
	resp := resource.ReadResponse{State: ir.State}
	r.Read(context.Background(), resource.ReadRequest{State: ir.State}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got rolloutRampModel
	resp.State.Get(context.Background(), &got)
	if !got.DefaultRule.ValueBool() || !got.RuleID.IsNull() || got.Variation.ValueString() != "true" ||
		!got.Steps.Equal(rampSteps(10, 60, 50, 60, 100, 0)) {
		t.Errorf("imported state = %+v", got)
	}
}

func TestRolloutRampDestroyLeavesTheRuleWhereItIs(t *testing.T) {
	abort := "POST " + rampPath + "/abort"
	f, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath: replyJSON(200, apiRamp("Running", 1, false)),
		"GET " + rampEnv + "/targeting": replyJSON(200, map[string]any{"isEnabled": true, "rules": []map[string]any{
			{"id": ruleID, "priority": 0, "variationId": "v1", "rolloutPercentage": 35, "conditionGroups": []any{}},
		}}),
		abort: replyJSON(200, apiRamp("Aborted", 1, false)),
	}, stateRamp(false))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// 35, the rule's live rollout, not 50 (the last step): someone lowered it while paused.
	if strings.TrimSpace(f.bodies[abort]) != `{"percent":35}` {
		t.Errorf("abort body = %q", f.bodies[abort])
	}
}

func TestRolloutRampDestroyUsesAbortPercent(t *testing.T) {
	abort := "POST " + rampPath + "/abort"
	state := stateRamp(false)
	state.AbortPercent = types.Int64Value(0)
	f, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath: replyJSON(200, apiRamp("Paused", 1, false)),
		abort:             replyJSON(200, apiRamp("Aborted", 1, false)),
	}, state)
	if resp.Diagnostics.HasError() || strings.TrimSpace(f.bodies[abort]) != `{"percent":0}` {
		t.Errorf("diags %v, abort body %q", resp.Diagnostics, f.bodies[abort])
	}
}

func TestRolloutRampDestroyOnDefaultRuleAbortsAtTheLastStep(t *testing.T) {
	abort := "POST " + rampPath + "/abort"
	f, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath: replyJSON(200, apiRamp("Running", 1, true)),
		abort:             replyJSON(200, apiRamp("Aborted", 1, true)),
	}, stateRamp(true))
	if resp.Diagnostics.HasError() || strings.TrimSpace(f.bodies[abort]) != `{"percent":50}` {
		t.Errorf("diags %v, abort body %q", resp.Diagnostics, f.bodies[abort])
	}
}

func TestRolloutRampDestroyOfAFinishedRampChangesNothing(t *testing.T) {
	for _, status := range []string{"Completed", "Aborted"} {
		f, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
			"GET " + rampPath: replyJSON(200, apiRamp(status, 2, false)),
		}, stateRamp(false))
		if resp.Diagnostics.HasError() || !slices.Equal(f.calls, []string{"GET " + rampPath}) {
			t.Errorf("%s: diags %v calls %v", status, resp.Diagnostics, f.calls)
		}
	}
}

func TestRolloutRampDestroyToleratesARampThatFinishedMeanwhile(t *testing.T) {
	gets := 0
	f, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath: func(w http.ResponseWriter) {
			gets++
			status := "Running"
			if gets > 1 {
				status = "Completed"
			}
			replyJSON(200, apiRamp(status, 1, true))(w)
		},
		"POST " + rampPath + "/abort": replyJSON(409, map[string]any{"error": "conflict", "message": "The ramp has already finished."}),
	}, stateRamp(true))
	if resp.Diagnostics.HasError() || len(f.calls) != 3 {
		t.Errorf("diags %v calls %v", resp.Diagnostics, f.calls)
	}
}

func TestRolloutRampDestroyOfAGoneRampIsQuiet(t *testing.T) {
	_, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){"GET " + rampPath: replyStatus(404)}, stateRamp(false))
	if resp.Diagnostics.HasError() {
		t.Error(resp.Diagnostics)
	}
}

func validateRamp(t *testing.T, m rolloutRampModel) []string {
	t.Helper()
	r := &rolloutRampResource{}
	s := resourceSchema(t, r)
	var resp resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: testConfig(t, s, m)}, &resp)
	var out []string
	for _, d := range resp.Diagnostics.Errors() {
		out = append(out, d.Summary())
	}
	return out
}

func TestRolloutRampValidateConfig(t *testing.T) {
	both := planRamp()
	both.DefaultRule = types.BoolValue(true)
	neither := planRamp()
	neither.RuleID = types.StringNull()
	falling := planRamp()
	falling.Steps = rampSteps(50, 60, 40, 0)
	shortHold := planRamp()
	shortHold.Steps = rampSteps(10, 4, 100, 0)
	ruleWithVariation := planRamp()
	ruleWithVariation.Variation = types.StringValue("true")
	unknownRule := planRamp()
	unknownRule.RuleID = types.StringUnknown()

	cases := map[string]struct {
		m    rolloutRampModel
		want string
	}{
		"valid":                      {planRamp(), ""},
		"rule_id from another apply": {unknownRule, ""},
		"both":                       {both, "Set exactly one of rule_id and default_rule"},
		"neither":                    {neither, "Set exactly one of rule_id and default_rule"},
		"falling":                    {falling, "Ramp steps must rise"},
		"short hold":                 {shortHold, "Hold too short"},
		"variation on a rule":        {ruleWithVariation, "variation and bucket_by apply only to default-rule ramps"},
	}
	for name, c := range cases {
		got := validateRamp(t, c.m)
		if c.want == "" && len(got) != 0 || c.want != "" && !slices.Contains(got, c.want) {
			t.Errorf("%s: errors %v, want %q", name, got, c.want)
		}
	}
}

func replaces(t *testing.T, m planmodifier.String, state, config, plan types.String) bool {
	t.Helper()
	resp := planmodifier.StringResponse{PlanValue: plan}
	m.PlanModifyString(context.Background(), planmodifier.StringRequest{
		StateValue: state, ConfigValue: config, PlanValue: plan,
		State: tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})},
		Plan:  tfsdk.Plan{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})},
	}, &resp)
	return resp.RequiresReplace
}

func TestRolloutRampStartOnlyInputsReplaceOnlyWhenChangedToANewValue(t *testing.T) {
	v, null := types.StringValue, types.StringNull()
	cases := map[string]struct {
		state, config types.String
		want          bool
	}{
		"changed":                        {v("a"), v("b"), true},
		"unchanged":                      {v("a"), v("a"), false},
		"set on an imported ramp":        {null, v("a"), false},
		"omitted after import filled it": {v("true"), null, false},
		"removed from a ramp it started": {v("a"), null, false},
		"never set":                      {null, null, false},
	}
	for name, c := range cases {
		if got := replaces(t, startOnly(), c.state, c.config, c.config); got != c.want {
			t.Errorf("%s: replace = %v, want %v", name, got, c.want)
		}
	}
}

func TestRolloutRampRuleIDReplacesOnlyForAnotherKnownRule(t *testing.T) {
	old := types.StringValue(ruleID)
	if replaces(t, ruleReplace(), old, types.StringUnknown(), types.StringUnknown()) {
		t.Error("an unknown rule_id (featureflip_flag_environment changed) must not abort the ramp")
	}
	if replaces(t, ruleReplace(), old, old, old) {
		t.Error("the same rule must not replace")
	}
	other := types.StringValue("0199a8c4-0000-7c2d-9f3a-4e6b8d0c1a2f")
	if !replaces(t, ruleReplace(), old, other, other) {
		t.Error("another rule must replace")
	}
}

func runRampUpdate(t *testing.T, routes map[string]func(w http.ResponseWriter), state, plan rolloutRampModel) (rolloutRampModel, resource.UpdateResponse) {
	t.Helper()
	_, c := newFakeAPI(t, routes)
	r := &rolloutRampResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.UpdateResponse{State: testState(t, s, state)}
	r.Update(context.Background(), resource.UpdateRequest{Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, plan)}, &resp)
	var got rolloutRampModel
	resp.State.Get(context.Background(), &got)
	return got, resp
}

func TestRolloutRampUpdateFillsAnUnknownRuleIDFromTheRamp(t *testing.T) {
	plan := planRamp()
	plan.RuleID = types.StringUnknown()
	plan.AbortPercent = types.Int64Value(0)
	got, resp := runRampUpdate(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath: replyJSON(200, apiRamp("Running", 1, false)),
	}, stateRamp(false), plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got.RuleID.ValueString() != ruleID || got.ID.ValueString() != "r1" || got.CurrentStepIndex.ValueInt64() != 1 || got.AbortPercent.ValueInt64() != 0 {
		t.Errorf("state = %+v", got)
	}
}

func TestRolloutRampUpdateOfAGoneRampSaysSo(t *testing.T) {
	_, resp := runRampUpdate(t, map[string]func(w http.ResponseWriter){"GET " + rampPath: replyStatus(404)}, stateRamp(false), planRamp())
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Summary(), "no longer exists") {
		t.Errorf("diags = %v", resp.Diagnostics)
	}
}

func TestRolloutRampDestroyReportsA409WhileStillRunning(t *testing.T) {
	_, resp := runRampDelete(t, map[string]func(w http.ResponseWriter){
		"GET " + rampPath:               replyJSON(200, apiRamp("Running", 1, false)),
		"GET " + rampEnv + "/targeting": replyJSON(200, map[string]any{"isEnabled": true, "rules": []any{}}),
		"POST " + rampPath + "/abort":   replyJSON(409, map[string]any{"error": "conflict", "message": "Changed meanwhile."}),
	}, stateRamp(false))
	if !resp.Diagnostics.HasError() {
		t.Error("a 409 on a ramp still running must surface")
	}
}

func TestRolloutRampValidateConfigSkipsAnUnknownStep(t *testing.T) {
	m := planRamp()
	m.Steps = types.ListValueMust(types.ObjectType{AttrTypes: rampStepAttrTypes}, []attr.Value{
		types.ObjectUnknown(rampStepAttrTypes),
		types.ObjectValueMust(rampStepAttrTypes, map[string]attr.Value{"percent": types.Int64Value(100), "hold_minutes": types.Int64Value(0)}),
	})
	if got := validateRamp(t, m); len(got) != 0 {
		t.Errorf("errors %v", got)
	}
}
