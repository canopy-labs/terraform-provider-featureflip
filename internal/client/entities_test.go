package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type captured struct {
	Method string
	Path   string
	Body   map[string]any
}

// record returns a client whose next requests are captured into *captured
// and answered with status + respBody.
func record(t *testing.T, cap *captured, status int, respBody string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.Method, cap.Path = r.Method, r.URL.Path
		cap.Body = nil
		if r.Body != nil {
			var m map[string]any
			if err := json.NewDecoder(r.Body).Decode(&m); err == nil {
				cap.Body = m
			}
		}
		w.WriteHeader(status)
		w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "ffs_t", "acme", "test")
	if err != nil {
		t.Fatal(err)
	}
	c.backoffBase = time.Millisecond
	return c
}

func strPtr(s string) *string { return &s }

func TestMe(t *testing.T) {
	var cap captured
	ctx := context.Background()

	// user-type caller (session/JWT or ffp_ PAT)
	c := record(t, &cap, 200, `{"type":"user","id":"u1","name":"A","email":"a@b.c","emailVerified":true}`)
	me, err := c.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cap.Method != "GET" || cap.Path != "/api/v1/me" {
		t.Errorf("%s %s", cap.Method, cap.Path)
	}
	if me.Type != "user" || me.Email == nil || *me.Email != "a@b.c" || me.OrganizationID != nil {
		t.Errorf("me = %+v", me)
	}

	// service-token caller (ffs_): org/role instead of email; absent projectScope = unrestricted
	c = record(t, &cap, 200, `{"type":"service_token","id":"t1","name":"terraform-prod","organizationId":"o1","role":"Admin"}`)
	me, err = c.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if me.Type != "service_token" || me.Email != nil || me.OrganizationID == nil || *me.Role != "Admin" || me.ProjectScope != nil {
		t.Errorf("me = %+v", me)
	}
}

func TestGetOrganization(t *testing.T) {
	var cap captured
	c := record(t, &cap, 200, `{"id":"o1","name":"Acme","slug":"acme","plan":"Business","isActive":true}`)
	org, err := c.GetOrganization(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme" {
		t.Errorf("path = %s", cap.Path)
	}
	if org.Plan != "Business" {
		t.Errorf("org = %+v", org)
	}
}

func TestCreateProject(t *testing.T) {
	var cap captured
	c := record(t, &cap, 201, `{"id":"p1","key":"web","name":"Web","description":"d"}`)
	p, err := c.CreateProject(context.Background(), CreateProjectRequest{Key: "web", Name: "Web", Description: strPtr("d")})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Method != "POST" || cap.Path != "/api/v1/orgs/acme/projects" {
		t.Errorf("%s %s", cap.Method, cap.Path)
	}
	if cap.Body["key"] != "web" || cap.Body["name"] != "Web" || cap.Body["description"] != "d" {
		t.Errorf("body = %v", cap.Body)
	}
	if p.ID != "p1" {
		t.Errorf("p = %+v", p)
	}
}

func TestProjectCRUDPaths(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 200, `{"id":"p1","key":"web","name":"Web"}`)
	if _, err := c.GetProject(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "GET" || cap.Path != "/api/v1/orgs/acme/projects/web" {
		t.Errorf("get: %s %s", cap.Method, cap.Path)
	}

	c = record(t, &cap, 204, "")
	if err := c.UpdateProject(ctx, "web", UpdateProjectRequest{Name: "Web2"}); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" || cap.Body["name"] != "Web2" {
		t.Errorf("update: %s %v", cap.Method, cap.Body)
	}

	c = record(t, &cap, 204, "")
	if err := c.DeleteProject(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" || cap.Path != "/api/v1/orgs/acme/projects/web" {
		t.Errorf("delete: %s %s", cap.Method, cap.Path)
	}
}

func TestEnvironmentCRUDPaths(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 201, `{"id":"e1","key":"prod","name":"Prod","color":"#ff0000","sortOrder":1}`)
	env, err := c.CreateEnvironment(ctx, "web", CreateEnvironmentRequest{Key: "prod", Name: "Prod", Color: strPtr("#ff0000"), SortOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/environments" {
		t.Errorf("create path = %s", cap.Path)
	}
	if cap.Body["sortOrder"] != float64(1) {
		t.Errorf("body = %v", cap.Body)
	}
	if env.Color == nil || *env.Color != "#ff0000" {
		t.Errorf("env = %+v", env)
	}

	c = record(t, &cap, 200, `{"id":"e1","key":"prod","name":"Prod","sortOrder":0}`)
	if _, err := c.GetEnvironment(ctx, "web", "prod"); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/environments/prod" {
		t.Errorf("get path = %s", cap.Path)
	}

	c = record(t, &cap, 204, "")
	if err := c.UpdateEnvironment(ctx, "web", "prod", UpdateEnvironmentRequest{Name: "Production", SortOrder: 2}); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" {
		t.Errorf("update method = %s", cap.Method)
	}

	c = record(t, &cap, 204, "")
	if err := c.DeleteEnvironment(ctx, "web", "prod"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" {
		t.Errorf("delete method = %s", cap.Method)
	}
}

func TestCreateFlagWithInitialVariations(t *testing.T) {
	var cap captured
	c := record(t, &cap, 201, `{"id":"f1","key":"limit","name":"Limit","type":"Number","isArchived":false,"clientSideVisible":false,"tags":["t1"],"variations":[{"id":"v1","key":"low","name":"Low","value":"10"},{"id":"v2","key":"high","name":"High","value":"100"}]}`)
	f, err := c.CreateFlag(context.Background(), "web", CreateFlagRequest{
		Key: "limit", Name: "Limit", Type: "Number", Tags: []string{"t1"},
		InitialVariations: []VariationInput{{Key: "low", Name: "Low", Value: "10"}, {Key: "high", Name: "High", Value: "100"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags" {
		t.Errorf("path = %s", cap.Path)
	}
	ivs := cap.Body["initialVariations"].([]any)
	if len(ivs) != 2 || ivs[0].(map[string]any)["key"] != "low" {
		t.Errorf("initialVariations = %v", ivs)
	}
	if len(f.Variations) != 2 || f.Variations[1].ID != "v2" {
		t.Errorf("flag = %+v", f)
	}
}

func TestFlagLifecyclePaths(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 204, "")
	if err := c.ArchiveFlag(ctx, "web", "limit"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "POST" || cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/archive" {
		t.Errorf("archive: %s %s", cap.Method, cap.Path)
	}

	c = record(t, &cap, 204, "")
	if err := c.RestoreFlag(ctx, "web", "limit"); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/restore" {
		t.Errorf("restore: %s", cap.Path)
	}

	c = record(t, &cap, 201, `{"id":"v3","key":"mid","name":"Mid","value":"50"}`)
	v, err := c.AddVariation(ctx, "web", "limit", VariationInput{Key: "mid", Name: "Mid", Value: "50"})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/variations" || v.ID != "v3" {
		t.Errorf("add variation: %s %+v", cap.Path, v)
	}

	c = record(t, &cap, 204, "")
	if err := c.UpdateVariation(ctx, "web", "limit", "v3", UpdateVariationRequest{Name: "Mid", Value: "55"}); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" || cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/variations/v3" {
		t.Errorf("update variation: %s %s", cap.Method, cap.Path)
	}

	c = record(t, &cap, 204, "")
	if err := c.DeleteVariation(ctx, "web", "limit", "v3"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" {
		t.Errorf("delete variation: %s", cap.Method)
	}
}

func TestFlagExpiry(t *testing.T) {
	var cap captured
	ctx := context.Background()

	exp := "2027-01-31T00:00:00Z"
	c := record(t, &cap, 201, `{"id":"f1","key":"limit","name":"Limit","type":"Boolean","expiresAtUtc":"2027-01-31T00:00:00Z"}`)
	f, err := c.CreateFlag(ctx, "web", CreateFlagRequest{Key: "limit", Name: "Limit", Type: "Boolean", ExpiresAtUtc: &exp})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Body["expiresAtUtc"] != exp {
		t.Errorf("create body expiresAtUtc = %v", cap.Body["expiresAtUtc"])
	}
	if f.ExpiresAtUtc == nil || *f.ExpiresAtUtc != exp {
		t.Errorf("create response expiresAtUtc = %v", f.ExpiresAtUtc)
	}

	// No expiry → the field is omitted, not sent as null.
	c = record(t, &cap, 201, `{"id":"f1","key":"limit","name":"Limit","type":"Boolean","expiresAtUtc":null}`)
	f, err = c.CreateFlag(ctx, "web", CreateFlagRequest{Key: "limit", Name: "Limit", Type: "Boolean"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cap.Body["expiresAtUtc"]; present {
		t.Errorf("create without expiry sent expiresAtUtc: %v", cap.Body)
	}
	if f.ExpiresAtUtc != nil {
		t.Errorf("null expiresAtUtc decoded as %q", *f.ExpiresAtUtc)
	}

	// The flag PUT must never carry expiry, or every update would touch it.
	c = record(t, &cap, 204, "")
	if err := c.UpdateFlag(ctx, "web", "limit", UpdateFlagRequest{Name: "Limit", Tags: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, present := cap.Body["expiresAtUtc"]; present {
		t.Errorf("flag PUT carried expiresAtUtc: %v", cap.Body)
	}

	c = record(t, &cap, 204, "")
	if err := c.SetFlagExpiry(ctx, "web", "limit", exp); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" || cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/expiry" || cap.Body["expiresAtUtc"] != exp {
		t.Errorf("set expiry: %s %s %v", cap.Method, cap.Path, cap.Body)
	}

	c = record(t, &cap, 204, "")
	if err := c.ClearFlagExpiry(ctx, "web", "limit"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" || cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/expiry" {
		t.Errorf("clear expiry: %s %s", cap.Method, cap.Path)
	}

	// EXPIRY_IN_PAST arrives inside the validation envelope's fields.
	c = record(t, &cap, 400, `{"error":"validation_failed","message":"Validation failed","fields":{"expiresAtUtc":["EXPIRY_IN_PAST: expiresAtUtc must be in the future."]}}`)
	err = c.SetFlagExpiry(ctx, "web", "limit", "2020-01-01T00:00:00Z")
	if !HasCode(err, "EXPIRY_IN_PAST") {
		t.Errorf("expected EXPIRY_IN_PAST, got %v", err)
	}
}

func TestFlagEnvConfigPrerequisitesNullVsEmpty(t *testing.T) {
	var cap captured
	ctx := context.Background()

	// nil slice → "prerequisites":null (leave untouched)
	c := record(t, &cap, 204, "")
	if err := c.UpdateFlagEnvConfig(ctx, "web", "limit", "prod", UpdateFlagEnvConfigRequest{DefaultVariationID: "v1"}); err != nil {
		t.Fatal(err)
	}
	if cap.Body["defaultVariationId"] != "v1" {
		t.Errorf("body = %v", cap.Body)
	}
	if v, present := cap.Body["prerequisites"]; !present || v != nil {
		t.Errorf("nil prerequisites must marshal to null, got %v (present=%v)", v, present)
	}

	// empty slice → "prerequisites":[] (clear)
	c = record(t, &cap, 204, "")
	if err := c.UpdateFlagEnvConfig(ctx, "web", "limit", "prod", UpdateFlagEnvConfigRequest{DefaultVariationID: "v1", Prerequisites: []Prerequisite{}}); err != nil {
		t.Fatal(err)
	}
	if v, ok := cap.Body["prerequisites"].([]any); !ok || len(v) != 0 {
		t.Errorf("empty prerequisites must marshal to [], got %v", cap.Body["prerequisites"])
	}
}

func TestToggleAndTargeting(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 204, "")
	if err := c.ToggleFlag(ctx, "web", "limit", "prod", true); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/environments/prod/toggle" || cap.Body["enabled"] != true {
		t.Errorf("toggle: %s %v", cap.Path, cap.Body)
	}

	c = record(t, &cap, 200, `{"isEnabled":true,"rules":[{"id":"r1","priority":0,"variationId":"v1","rolloutPercentage":50,"userSegmentId":"s1","conditionGroups":[{"operator":"And","conditions":[{"attribute":"country","operator":"Equals","values":["DE"],"negate":false}]}]}]}`)
	tc, err := c.GetTargeting(ctx, "web", "limit", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(tc.Rules) != 1 || tc.Rules[0].ConditionGroups[0].Conditions[0].Attribute != "country" {
		t.Errorf("targeting = %+v", tc)
	}

	c = record(t, &cap, 201, `{"id":"r2","priority":1,"variationId":"v2","conditionGroups":[]}`)
	r, err := c.CreateRule(ctx, "web", "limit", "prod", RuleInput{VariationID: "v2", ConditionGroups: []ConditionGroup{}})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/environments/prod/rules" || r.ID != "r2" {
		t.Errorf("create rule: %s %+v", cap.Path, r)
	}
	if cap.Body["variationId"] != "v2" {
		t.Errorf("rule body = %v", cap.Body)
	}

	c = record(t, &cap, 204, "")
	if err := c.ReorderRules(ctx, "web", "limit", "prod", []string{"r2", "r1"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/flags/limit/environments/prod/rules/reorder" {
		t.Errorf("reorder path = %s", cap.Path)
	}
	ids := cap.Body["ruleIds"].([]any)
	if len(ids) != 2 || ids[0] != "r2" {
		t.Errorf("ruleIds = %v", ids)
	}
}

func TestSegmentCRUD(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 201, `{"id":"s1","key":"beta","name":"Beta","conditions":[{"attribute":"email","operator":"EndsWith","values":["@acme.io"],"negate":false}]}`)
	s, err := c.CreateSegment(ctx, "web", CreateSegmentRequest{Key: "beta", Name: "Beta", Conditions: []Condition{{Attribute: "email", Operator: "EndsWith", Values: []string{"@acme.io"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/segments" || s.ID != "s1" {
		t.Errorf("create: %s %+v", cap.Path, s)
	}
	conds := cap.Body["conditions"].([]any)
	if conds[0].(map[string]any)["operator"] != "EndsWith" {
		t.Errorf("conditions = %v", conds)
	}

	c = record(t, &cap, 204, "")
	if err := c.UpdateSegment(ctx, "web", "beta", UpdateSegmentRequest{Name: "Beta2", Conditions: []Condition{}}); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" || cap.Path != "/api/v1/orgs/acme/projects/web/segments/beta" {
		t.Errorf("update: %s %s", cap.Method, cap.Path)
	}

	c = record(t, &cap, 204, "")
	if err := c.DeleteSegment(ctx, "web", "beta"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" {
		t.Errorf("delete: %s", cap.Method)
	}
}

func TestSDKKeyLifecycle(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 201, `{"id":"k1","key":"ffc_plaintext_once","lastFourCharacters":"once","type":"ClientSide","name":"web-prod"}`)
	created, err := c.CreateSDKKey(ctx, "web", "prod", CreateSDKKeyRequest{Name: "web-prod", Type: "ClientSide"})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/environments/prod/sdk-keys" {
		t.Errorf("create path = %s", cap.Path)
	}
	if created.Key != "ffc_plaintext_once" {
		t.Errorf("created = %+v", created)
	}

	c = record(t, &cap, 200, `{"id":"k1","name":"web-prod","type":"ClientSide","lastFourCharacters":"once","isActive":true}`)
	k, err := c.GetSDKKey(ctx, "web", "prod", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/projects/web/environments/prod/sdk-keys/k1" || !k.IsActive {
		t.Errorf("get: %s %+v", cap.Path, k)
	}

	c = record(t, &cap, 204, "")
	if err := c.RevokeSDKKey(ctx, "web", "prod", "k1"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "POST" || cap.Path != "/api/v1/orgs/acme/projects/web/environments/prod/sdk-keys/k1/revoke" {
		t.Errorf("revoke: %s %s", cap.Method, cap.Path)
	}
}
