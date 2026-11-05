package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

// envAPI records the JSON body of each environment write and answers reads
// with the critical settings it was given.
func envAPI(t *testing.T, changeReason, confirmation bool) (*client.Client, map[string]map[string]any) {
	t.Helper()
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s body: %v", r.Method, err)
			}
			bodies[r.Method] = body
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "env-id", "key": "production", "name": "Production", "color": nil, "sortOrder": 0,
			"requireChangeReason": changeReason, "requireConfirmation": confirmation,
		})
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, "ffs_test", "acme", "test")
	if err != nil {
		t.Fatal(err)
	}
	return c, bodies
}

func baseEnvironment() environmentModel {
	return environmentModel{
		ID:                  types.StringValue("env-id"),
		Project:             types.StringValue("p"),
		Key:                 types.StringValue("production"),
		Name:                types.StringValue("Production"),
		Color:               types.StringNull(),
		SortOrder:           types.Int64Value(0),
		RequireChangeReason: types.BoolNull(),
		RequireConfirmation: types.BoolNull(),
	}
}

// Omitting a critical setting must send null (false on create, unchanged on
// update), never the value the plan carried from state: resending a carried
// true is harmless, but resending a stale one would overwrite a dashboard edit.
func TestEnvironmentUpdateSendsOnlyConfiguredCriticalSettings(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		cfgReason, cfgConfirm   types.Bool
		wantReason, wantConfirm any
	}{
		{"both omitted", types.BoolNull(), types.BoolNull(), nil, nil},
		{"reason on, confirmation omitted", types.BoolValue(true), types.BoolNull(), true, nil},
		{"both off", types.BoolValue(false), types.BoolValue(false), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, bodies := envAPI(t, true, true)
			r := &environmentResource{resourceWithClient{client: c}}
			s := resourceSchema(t, r)

			state := baseEnvironment()
			state.RequireChangeReason, state.RequireConfirmation = types.BoolValue(true), types.BoolValue(true)
			cfg := baseEnvironment()
			cfg.ID = types.StringUnknown()
			cfg.Name = types.StringValue("Prod")
			cfg.RequireChangeReason, cfg.RequireConfirmation = tc.cfgReason, tc.cfgConfirm
			plan := cfg
			plan.ID = state.ID
			// UseStateForUnknown carries an omitted setting from state.
			if tc.cfgReason.IsNull() {
				plan.RequireChangeReason = state.RequireChangeReason
			}
			if tc.cfgConfirm.IsNull() {
				plan.RequireConfirmation = state.RequireConfirmation
			}

			req := resource.UpdateRequest{Plan: testPlan(t, s, plan), Config: testConfig(t, s, cfg), State: testState(t, s, state)}
			resp := resource.UpdateResponse{State: testState(t, s, state)}
			r.Update(context.Background(), req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}

			put := bodies[http.MethodPut]
			if put["requireChangeReason"] != tc.wantReason || put["requireConfirmation"] != tc.wantConfirm {
				t.Errorf("PUT sent requireChangeReason=%v requireConfirmation=%v, want %v and %v",
					put["requireChangeReason"], put["requireConfirmation"], tc.wantReason, tc.wantConfirm)
			}
			var got environmentModel
			resp.Diagnostics.Append(resp.State.Get(context.Background(), &got)...)
			if !got.RequireChangeReason.ValueBool() || !got.RequireConfirmation.ValueBool() {
				t.Errorf("state after update = %v/%v, want the re-read true/true", got.RequireChangeReason, got.RequireConfirmation)
			}
		})
	}
}

func TestEnvironmentCreateLeavesOmittedCriticalSettingsToTheAPI(t *testing.T) {
	c, bodies := envAPI(t, false, true)
	r := &environmentResource{resourceWithClient{client: c}}
	s := resourceSchema(t, r)

	plan := baseEnvironment()
	plan.ID = types.StringUnknown()
	plan.RequireChangeReason = types.BoolUnknown() // omitted: Computed, unknown until apply
	plan.RequireConfirmation = types.BoolValue(true)

	req := resource.CreateRequest{Plan: testPlan(t, s, plan), Config: testConfig(t, s, plan)}
	resp := resource.CreateResponse{State: tfsdkStateNull(s)}
	r.Create(context.Background(), req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}

	post := bodies[http.MethodPost]
	if v, ok := post["requireChangeReason"]; !ok || v != nil {
		t.Errorf("POST requireChangeReason = %v (present %v), want null", v, ok)
	}
	if post["requireConfirmation"] != true {
		t.Errorf("POST requireConfirmation = %v, want true", post["requireConfirmation"])
	}
	var got environmentModel
	resp.Diagnostics.Append(resp.State.Get(context.Background(), &got)...)
	if got.RequireChangeReason.IsUnknown() || got.RequireChangeReason.ValueBool() {
		t.Errorf("state require_change_reason = %v, want the API's false", got.RequireChangeReason)
	}
}
