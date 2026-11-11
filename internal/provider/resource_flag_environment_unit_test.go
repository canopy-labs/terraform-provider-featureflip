package provider

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

func ptr[T any](v T) *T { return &v }

func TestRuleUnchanged(t *testing.T) {
	cond := func(attr string, values ...string) client.Condition {
		return client.Condition{Attribute: attr, Operator: "Equals", Values: values}
	}
	base := func() (client.TargetingRule, client.RuleInput) {
		groups := func() []client.ConditionGroup {
			return []client.ConditionGroup{
				{Operator: "And", Conditions: []client.Condition{cond("plan", "beta"), cond("country", "US")}},
				{Operator: "Or", Conditions: []client.Condition{cond("tier", "gold")}},
			}
		}
		return client.TargetingRule{ID: "r1", Priority: 0, VariationID: "v1", RolloutPercentage: ptr[int64](40), ConditionGroups: groups()},
			client.RuleInput{VariationID: "v1", RolloutPercentage: ptr[int64](40), ConditionGroups: groups()}
	}
	cases := map[string]struct {
		edit func(*client.TargetingRule, *client.RuleInput)
		want bool
	}{
		"identical":                  {func(*client.TargetingRule, *client.RuleInput) {}, true},
		"absent description as \"\"": {func(s *client.TargetingRule, _ *client.RuleInput) { s.Description = ptr("") }, true},
		"groups and conditions reordered": {func(s *client.TargetingRule, _ *client.RuleInput) {
			s.ConditionGroups[0], s.ConditionGroups[1] = s.ConditionGroups[1], s.ConditionGroups[0]
			c := s.ConditionGroups[1].Conditions
			c[0], c[1] = c[1], c[0]
		}, true},
		"description changed": {func(_ *client.TargetingRule, in *client.RuleInput) { in.Description = ptr("beta users") }, false},
		"variation changed":   {func(_ *client.TargetingRule, in *client.RuleInput) { in.VariationID = "v2" }, false},
		"rollout changed":     {func(_ *client.TargetingRule, in *client.RuleInput) { in.RolloutPercentage = ptr[int64](50) }, false},
		"rollout removed":     {func(_ *client.TargetingRule, in *client.RuleInput) { in.RolloutPercentage = nil }, false},
		"segment added":       {func(_ *client.TargetingRule, in *client.RuleInput) { in.UserSegmentID = ptr("s1") }, false},
		"segment changed": {func(s *client.TargetingRule, in *client.RuleInput) {
			s.UserSegmentID, in.UserSegmentID = ptr("s1"), ptr("s2")
		}, false},
		"group operator changed": {func(_ *client.TargetingRule, in *client.RuleInput) { in.ConditionGroups[1].Operator = "And" }, false},
		"condition value changed": {func(_ *client.TargetingRule, in *client.RuleInput) {
			in.ConditionGroups[0].Conditions[0].Values = []string{"alpha"}
		}, false},
		"condition negated": {func(_ *client.TargetingRule, in *client.RuleInput) { in.ConditionGroups[0].Conditions[1].Negate = true }, false},
		"group added": {func(_ *client.TargetingRule, in *client.RuleInput) {
			in.ConditionGroups = append(in.ConditionGroups, client.ConditionGroup{Operator: "And", Conditions: []client.Condition{cond("a", "b")}})
		}, false},
		"condition added": {func(_ *client.TargetingRule, in *client.RuleInput) {
			in.ConditionGroups[1].Conditions = append(in.ConditionGroups[1].Conditions, cond("a", "b"))
		}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, in := base()
			tc.edit(&s, &in)
			if got := ruleUnchanged(s, in); got != tc.want {
				t.Fatalf("ruleUnchanged = %v, want %v", got, tc.want)
			}
		})
	}
}

// Updating one rule writes only that rule: a PUT of an unchanged rule would
// pause a rollout ramp running on it.
func TestFlagEnvironmentUpdateWritesOnlyChangedRules(t *testing.T) {
	const env = "/projects/p/flags/f/environments/production"
	beta := []map[string]any{{"operator": "And", "conditions": []map[string]any{
		{"attribute": "plan", "operator": "Equals", "values": []string{"beta"}, "negate": false},
	}}}
	api, c := newFakeAPI(t, map[string]func(http.ResponseWriter){
		"GET /projects/p/flags/f": replyJSON(200, apiFlag("", false, false)),
		"GET " + env: replyJSON(200, map[string]any{
			"isEnabled": true, "defaultVariationId": "v2", "strategy": "SingleVariation",
		}),
		"PUT " + env: replyStatus(204),
		"GET " + env + "/targeting": replyJSON(200, map[string]any{"isEnabled": true, "rules": []map[string]any{
			// The ramped rule: its rollout moved to 50, the config ignores it.
			{"id": "ramped", "priority": 0, "description": "", "variationId": "v1", "rolloutPercentage": 50, "conditionGroups": beta},
			{"id": "other", "priority": 1, "description": "old", "variationId": "v1", "conditionGroups": []any{}},
		}}),
		"PUT " + env + "/rules/other":   replyStatus(204),
		"PUT " + env + "/rules/reorder": replyStatus(204),
	})
	r := &flagEnvironmentResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)

	groups := types.ListValueMust(types.ObjectType{AttrTypes: conditionGroupAttrTypes()}, []attr.Value{
		types.ObjectValueMust(conditionGroupAttrTypes(), map[string]attr.Value{
			"operator": types.StringValue("And"),
			"conditions": types.ListValueMust(types.ObjectType{AttrTypes: conditionAttrTypes()}, []attr.Value{
				types.ObjectValueMust(conditionAttrTypes(), map[string]attr.Value{
					"attribute": types.StringValue("plan"), "operator": types.StringValue("Equals"),
					"values": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("beta")}),
					"negate": types.BoolValue(false),
				}),
			}),
		}),
	})
	rule := func(desc types.String, rollout types.Int64, cg types.List) attr.Value {
		return types.ObjectValueMust(ruleAttrTypes(), map[string]attr.Value{
			"id": types.StringUnknown(), "description": desc, "variation": types.StringValue("true"),
			"rollout_percentage": rollout, "segment": types.StringNull(), "condition_groups": cg,
		})
	}
	plan := flagEnvModel{
		ID: types.StringValue("p/f/production"), Project: types.StringValue("p"), Flag: types.StringValue("f"),
		Environment: types.StringValue("production"), Enabled: types.BoolValue(true),
		DefaultVariation: types.StringValue("false"), Strategy: types.StringValue("SingleVariation"),
		Prerequisites: types.ListNull(types.ObjectType{AttrTypes: prereqAttrTypes()}),
		Rules: types.ListValueMust(types.ObjectType{AttrTypes: ruleAttrTypes()}, []attr.Value{
			rule(types.StringNull(), types.Int64Value(50), groups),
			rule(types.StringValue("new"), types.Int64Null(), types.ListNull(types.ObjectType{AttrTypes: conditionGroupAttrTypes()})),
		}),
	}
	req := resource.UpdateRequest{Plan: testPlan(t, s, plan)}
	resp := resource.UpdateResponse{State: tfsdkStateNull(s)}
	r.Update(context.Background(), req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if slices.Contains(api.calls, "PUT "+env+"/rules/ramped") {
		t.Errorf("the unchanged ramped rule was rewritten: %v", api.calls)
	}
	if !slices.Contains(api.calls, "PUT "+env+"/rules/other") {
		t.Errorf("the changed rule was not written: %v", api.calls)
	}
	var got flagEnvModel
	resp.Diagnostics.Append(resp.State.Get(context.Background(), &got)...)
	var rules []ruleModel
	resp.Diagnostics.Append(got.Rules.ElementsAs(context.Background(), &rules, false)...)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if rules[0].ID.ValueString() != "ramped" || rules[1].ID.ValueString() != "other" {
		t.Errorf("rule ids = %s, %s; want ramped, other", rules[0].ID, rules[1].ID)
	}
}

// An environment created after its flag has no configuration for it yet, and the
// API reports defaultVariationId as the empty GUID. Sending that back was a 400
// ("Default variation ID is required"), so a config without default_variation
// could not be applied. The server's own default goes out instead.
func TestFlagEnvironmentCreateOnUnconfiguredEnvironmentSendsTheServerDefault(t *testing.T) {
	const env = "/projects/p/flags/f/environments/acctest"
	gets := 0
	api, c := newFakeAPI(t, map[string]func(http.ResponseWriter){
		"GET /projects/p/flags/f": replyJSON(200, apiFlag("", false, false)),
		"GET " + env: func(w http.ResponseWriter) {
			gets++
			id := unconfiguredVariationID
			if gets > 1 {
				id = "v2" // the PUT materialised the configuration
			}
			replyJSON(200, map[string]any{"isEnabled": false, "defaultVariationId": id, "strategy": "SingleVariation"})(w)
		},
		"PUT " + env: replyStatus(204),
	})
	r := &flagEnvironmentResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	plan := flagEnvModel{
		ID: types.StringUnknown(), Project: types.StringValue("p"), Flag: types.StringValue("f"),
		Environment: types.StringValue("acctest"), Enabled: types.BoolValue(false),
		DefaultVariation: types.StringUnknown(), Strategy: types.StringUnknown(),
		Prerequisites: types.ListNull(types.ObjectType{AttrTypes: prereqAttrTypes()}),
		Rules:         types.ListNull(types.ObjectType{AttrTypes: ruleAttrTypes()}),
	}
	resp := resource.CreateResponse{State: tfsdkStateNull(s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: testPlan(t, s, plan)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if body := api.bodies["PUT "+env]; !strings.Contains(body, `"defaultVariationId":"v2"`) {
		t.Errorf("PUT body = %s, want the flag's false variation v2", body)
	}
	var got flagEnvModel
	resp.Diagnostics.Append(resp.State.Get(context.Background(), &got)...)
	if got.DefaultVariation.ValueString() != "false" {
		t.Errorf("default_variation = %s, want false", got.DefaultVariation)
	}
}

func TestServerDefaultVariationID(t *testing.T) {
	vars := []client.Variation{{ID: "a", Key: "true"}, {ID: "b", Key: "false"}}
	if got := serverDefaultVariationID(&client.Flag{Type: "Boolean", Variations: vars}); got != "b" {
		t.Errorf("boolean = %q, want the false variation b", got)
	}
	if got := serverDefaultVariationID(&client.Flag{Type: "String", Variations: vars}); got != "a" {
		t.Errorf("string = %q, want the first variation a", got)
	}
	if got := serverDefaultVariationID(&client.Flag{Type: "Boolean", Variations: vars[:1]}); got != "a" {
		t.Errorf("boolean without false = %q, want the first variation a", got)
	}
	if got := serverDefaultVariationID(&client.Flag{Type: "Boolean"}); got != "" {
		t.Errorf("no variations = %q, want empty", got)
	}
}
