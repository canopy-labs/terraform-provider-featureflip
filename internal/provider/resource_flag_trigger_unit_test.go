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
)

const triggersPath = "/projects/p/flags/f/environments/production/triggers"

func apiTrigger(enabled bool, lastFour string) map[string]any {
	return map[string]any{
		"id": "t1", "name": "Grafana", "description": nil, "action": "Disable", "enabled": enabled,
		"secretLastFour": lastFour, "createdAt": "2026-11-06T00:00:00Z", "updatedAt": "2026-11-06T00:00:00Z",
	}
}

func apiTriggerWithURL(secret string) map[string]any {
	return map[string]any{
		"trigger": apiTrigger(true, secret[len(secret)-4:]),
		"url":     "https://api.test/api/triggers/" + secret, "endpoint": "https://api.test/api/triggers",
		"secret": secret, "warning": "Store this as a secret.",
	}
}

func keepers(v string) types.Map {
	if v == "" {
		return types.MapNull(types.StringType)
	}
	return types.MapValueMust(types.StringType, map[string]attr.Value{"rotated": types.StringValue(v)})
}

func baseTrigger(keeper string) flagTriggerModel {
	return flagTriggerModel{
		ID:              types.StringValue("t1"),
		Project:         types.StringValue("p"),
		Flag:            types.StringValue("f"),
		Environment:     types.StringValue("production"),
		Name:            types.StringValue("Grafana"),
		Description:     types.StringNull(),
		Action:          types.StringValue("Disable"),
		Enabled:         types.BoolValue(true),
		RotationKeepers: keepers(keeper),
		URL:             types.StringValue("https://api.test/api/triggers/fft_old1"),
		Endpoint:        types.StringValue("https://api.test/api/triggers"),
		Secret:          types.StringValue("fft_old1"),
		SecretLastFour:  types.StringValue("old1"),
	}
}

func runTriggerUpdate(t *testing.T, routes map[string]func(w http.ResponseWriter), state, plan flagTriggerModel) (*fakeAPI, flagTriggerModel, resource.UpdateResponse) {
	t.Helper()
	f, c := newFakeAPI(t, routes)
	r := &flagTriggerResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.UpdateResponse{State: testState(t, s, state)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, plan),
	}, &resp)
	var got flagTriggerModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	return f, got, resp
}

func TestFlagTriggerUpdateWithoutKeeperChangeDoesNotRotate(t *testing.T) {
	plan := baseTrigger("1")
	plan.Enabled = types.BoolValue(false)
	f, got, resp := runTriggerUpdate(t, map[string]func(w http.ResponseWriter){
		"PUT " + triggersPath + "/t1": replyJSON(200, apiTrigger(false, "old1")),
	}, baseTrigger("1"), plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !slices.Equal(f.calls, []string{"PUT " + triggersPath + "/t1"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if got.Secret.ValueString() != "fft_old1" || got.Enabled.ValueBool() {
		t.Errorf("secret %q enabled %v: the old secret should survive and enabled follow the API", got.Secret.ValueString(), got.Enabled.ValueBool())
	}
}

func TestFlagTriggerKeeperChangeRotatesInPlace(t *testing.T) {
	plan := baseTrigger("2")
	plan.URL, plan.Secret, plan.SecretLastFour = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	f, got, resp := runTriggerUpdate(t, map[string]func(w http.ResponseWriter){
		"PUT " + triggersPath + "/t1":         replyJSON(200, apiTrigger(true, "old1")),
		"POST " + triggersPath + "/t1/rotate": replyJSON(200, apiTriggerWithURL("fft_new2")),
	}, baseTrigger("1"), plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// Nothing else changed, so no PUT: it would write an audit row for nothing.
	if !slices.Equal(f.calls, []string{"POST " + triggersPath + "/t1/rotate"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if got.ID.ValueString() != "t1" || got.Secret.ValueString() != "fft_new2" || got.SecretLastFour.ValueString() != "new2" ||
		!strings.HasSuffix(got.URL.ValueString(), "/fft_new2") {
		t.Errorf("got id %q secret %q last four %q url %q", got.ID.ValueString(), got.Secret.ValueString(), got.SecretLastFour.ValueString(), got.URL.ValueString())
	}
}

func TestFlagTriggerRenameAndRotateInOneApply(t *testing.T) {
	plan := baseTrigger("2")
	plan.Name = types.StringValue("Datadog")
	plan.URL, plan.Secret, plan.SecretLastFour = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	renamed := apiTrigger(true, "old1")
	renamed["name"] = "Datadog"
	f, got, resp := runTriggerUpdate(t, map[string]func(w http.ResponseWriter){
		"PUT " + triggersPath + "/t1":         replyJSON(200, renamed),
		"POST " + triggersPath + "/t1/rotate": replyJSON(200, apiTriggerWithURL("fft_new2")),
	}, baseTrigger("1"), plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !slices.Equal(f.calls, []string{"PUT " + triggersPath + "/t1", "POST " + triggersPath + "/t1/rotate"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if got.Secret.ValueString() != "fft_new2" {
		t.Errorf("secret %q", got.Secret.ValueString())
	}
}

// A failed rotation must leave the old keepers in state, or the next plan sees
// no change and the URL the user meant to revoke keeps working.
func TestFlagTriggerFailedRotationIsRetriedNextApply(t *testing.T) {
	plan := baseTrigger("2")
	plan.URL, plan.Secret, plan.SecretLastFour = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	_, got, resp := runTriggerUpdate(t, map[string]func(w http.ResponseWriter){
		"PUT " + triggersPath + "/t1": replyJSON(200, apiTrigger(true, "old1")),
		"POST " + triggersPath + "/t1/rotate": replyJSON(403, map[string]any{
			"error": "forbidden", "message": "Flag triggers need the Team plan or above.",
		}),
	}, baseTrigger("1"), plan)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error")
	}
	if !got.RotationKeepers.Equal(keepers("1")) || got.Secret.ValueString() != "fft_old1" || got.SecretLastFour.ValueString() != "old1" {
		t.Errorf("keepers %v secret %q: want the old keepers and secret kept", got.RotationKeepers, got.Secret.ValueString())
	}
}

func TestFlagTriggerRotationRequested(t *testing.T) {
	for _, tc := range []struct {
		name, state, plan string
		unknown, want     bool
	}{
		{"unchanged", "1", "1", false, false},
		{"changed", "1", "2", false, true},
		{"set for the first time", "", "1", false, true},
		{"removed", "1", "", false, false},
		{"never set", "", "", false, false},
		{"unknown until apply", "1", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := baseTrigger(tc.plan)
			if tc.unknown {
				plan.RotationKeepers = types.MapUnknown(types.StringType)
			}
			if got := rotationRequested(plan, baseTrigger(tc.state)); got != tc.want {
				t.Errorf("rotationRequested = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFlagTriggerModifyPlanMarksTheSecretUnknownOnlyWhenRotating(t *testing.T) {
	for _, tc := range []struct {
		name, planKeeper string
		wantUnknown      bool
	}{
		{"keepers changed", "2", true},
		{"keepers unchanged", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &flagTriggerResource{}
			s := resourceSchema(t, r)
			state, plan := baseTrigger("1"), baseTrigger(tc.planKeeper)
			resp := resource.ModifyPlanResponse{Plan: testPlan(t, s, plan)}
			r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
				Plan: testPlan(t, s, plan), State: testState(t, s, state), Config: testConfig(t, s, plan),
			}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got flagTriggerModel
			resp.Plan.Get(context.Background(), &got)
			if got.URL.IsUnknown() != tc.wantUnknown || got.Secret.IsUnknown() != tc.wantUnknown || got.SecretLastFour.IsUnknown() != tc.wantUnknown {
				t.Errorf("url unknown %v secret unknown %v last four unknown %v, want %v",
					got.URL.IsUnknown(), got.Secret.IsUnknown(), got.SecretLastFour.IsUnknown(), tc.wantUnknown)
			}
		})
	}
}

func runTriggerCreate(t *testing.T, routes map[string]func(w http.ResponseWriter), plan flagTriggerModel) (*fakeAPI, flagTriggerModel, resource.CreateResponse) {
	t.Helper()
	f, c := newFakeAPI(t, routes)
	r := &flagTriggerResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: tfsdkStateNull(s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: testPlan(t, s, plan), Config: testConfig(t, s, plan)}, &resp)
	var got flagTriggerModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	return f, got, resp
}

func createPlan(enabled bool) flagTriggerModel {
	p := baseTrigger("")
	p.ID, p.URL, p.Endpoint, p.Secret, p.SecretLastFour =
		types.StringUnknown(), types.StringUnknown(), types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	p.Enabled = types.BoolValue(enabled)
	return p
}

func TestFlagTriggerCreateStoresTheURLFromTheResponse(t *testing.T) {
	f, got, resp := runTriggerCreate(t, map[string]func(w http.ResponseWriter){
		"POST " + triggersPath: replyJSON(201, apiTriggerWithURL("fft_abcd")),
	}, createPlan(true))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %v, want only the create", f.calls)
	}
	if got.ID.ValueString() != "t1" || got.Secret.ValueString() != "fft_abcd" || got.Endpoint.ValueString() != "https://api.test/api/triggers" {
		t.Errorf("got id %q secret %q endpoint %q", got.ID.ValueString(), got.Secret.ValueString(), got.Endpoint.ValueString())
	}
}

func TestFlagTriggerCreateDisabledNeedsASecondWrite(t *testing.T) {
	f, got, resp := runTriggerCreate(t, map[string]func(w http.ResponseWriter){
		"POST " + triggersPath:        replyJSON(201, apiTriggerWithURL("fft_abcd")),
		"PUT " + triggersPath + "/t1": replyJSON(200, apiTrigger(false, "abcd")),
	}, createPlan(false))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !slices.Equal(f.calls, []string{"POST " + triggersPath, "PUT " + triggersPath + "/t1"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if got.Enabled.ValueBool() || got.Secret.ValueString() != "fft_abcd" {
		t.Errorf("enabled %v secret %q", got.Enabled.ValueBool(), got.Secret.ValueString())
	}
}

// If disabling fails after the create, the trigger exists and its URL is only
// in that create response, so state must still hold it.
func TestFlagTriggerCreateKeepsTheURLWhenDisablingFails(t *testing.T) {
	_, got, resp := runTriggerCreate(t, map[string]func(w http.ResponseWriter){
		"POST " + triggersPath:        replyJSON(201, apiTriggerWithURL("fft_abcd")),
		"PUT " + triggersPath + "/t1": replyJSON(400, map[string]any{"error": "validation_error", "message": "nope"}),
	}, createPlan(false))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error")
	}
	if got.ID.ValueString() != "t1" || got.Secret.ValueString() != "fft_abcd" {
		t.Errorf("id %q secret %q: state lost the created trigger", got.ID.ValueString(), got.Secret.ValueString())
	}
}

func TestFlagTriggerReadRemovesADeletedTrigger(t *testing.T) {
	_, c := newFakeAPI(t, map[string]func(w http.ResponseWriter){
		"GET " + triggersPath + "/t1": replyJSON(404, map[string]any{"error": "not_found", "message": "gone"}),
	})
	r := &flagTriggerResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)
	resp := resource.ReadResponse{State: testState(t, s, baseTrigger(""))}
	r.Read(context.Background(), resource.ReadRequest{State: testState(t, s, baseTrigger(""))}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Errorf("diags %v, state null %v: want the trigger dropped from state", resp.Diagnostics, resp.State.Raw.IsNull())
	}
}

func TestSplitFlagTriggerImportID(t *testing.T) {
	for _, tc := range []struct {
		in                     string
		project, flag, env, id string
		wantErr                bool
	}{
		{in: "web/checkout/production/t1", project: "web", flag: "checkout", env: "production", id: "t1"},
		{in: "web/checkout/team/prod/t1", project: "web", flag: "checkout", env: "team/prod", id: "t1"},
		{in: "web/checkout/t1", wantErr: true},
		{in: "web/checkout//t1", wantErr: true},
		{in: "web//production/t1", wantErr: true},
		{in: "web/checkout/production/", wantErr: true},
	} {
		project, flag, env, id, err := splitFlagTriggerImportID(tc.in, "fmt")
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error", tc.in)
			}
			continue
		}
		if err != nil || project != tc.project || flag != tc.flag || env != tc.env || id != tc.id {
			t.Errorf("%q: got %q %q %q %q %v", tc.in, project, flag, env, id, err)
		}
	}
}
