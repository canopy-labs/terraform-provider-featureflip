package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

// These helpers drive a resource's CRUD and plan methods directly, offline,
// against an httptest API, for behavior acceptance tests can't reach (a
// value changing between plan and apply, a failure halfway through Create).

func resourceSchema(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func emptyRaw(s schema.Schema) tftypes.Value {
	return tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)
}

func testPlan(t *testing.T, s schema.Schema, m any) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: s, Raw: emptyRaw(s)}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return p
}

func testState(t *testing.T, s schema.Schema, m any) tfsdk.State {
	t.Helper()
	st := tfsdk.State{Schema: s, Raw: emptyRaw(s)}
	if d := st.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return st
}

func testConfig(t *testing.T, s schema.Schema, m any) tfsdk.Config {
	t.Helper()
	// Config has no Set; build it through a Plan of the same schema.
	return tfsdk.Config{Schema: s, Raw: testPlan(t, s, m).Raw}
}

// fakeAPI answers "METHOD /path-suffix" routes; anything else is a test failure.
type fakeAPI struct {
	t      *testing.T
	routes map[string]func(w http.ResponseWriter)
	calls  []string
}

func newFakeAPI(t *testing.T, routes map[string]func(w http.ResponseWriter)) (*fakeAPI, *client.Client) {
	t.Helper()
	f := &fakeAPI{t: t, routes: routes}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1/orgs/acme")
		f.calls = append(f.calls, route)
		h, ok := f.routes[route]
		if !ok {
			t.Errorf("unexpected API call %s", route)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		h(w)
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, "ffs_test", "acme", "test")
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func replyJSON(status int, body any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func replyStatus(status int) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(status) }
}

func apiFlag(owner string, permanent, archived bool) map[string]any {
	f := map[string]any{
		"id": "id", "key": "k", "name": "K", "type": "Boolean", "isArchived": archived,
		"clientSideVisible": false, "tags": []string{}, "isPermanent": permanent,
		"variations": []map[string]any{
			{"id": "v1", "key": "true", "name": "True", "value": "true"},
			{"id": "v2", "key": "false", "name": "False", "value": "false"},
		},
	}
	if owner != "" {
		f["owner"] = map[string]any{"id": "u", "email": owner, "name": "U"}
	}
	return f
}

func tfsdkStateNull(s schema.Schema) tfsdk.State {
	return tfsdk.State{Schema: s, Raw: emptyRaw(s)}
}
