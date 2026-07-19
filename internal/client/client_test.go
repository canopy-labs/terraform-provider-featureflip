package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "ffs_testtoken", "acme", "test")
	if err != nil {
		t.Fatal(err)
	}
	c.backoffBase = time.Millisecond
	return c
}

func TestDoSendsAuthUserAgentAndPrefix(t *testing.T) {
	var gotPath, gotAuth, gotUA string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotUA = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		w.Write([]byte(`{"id":"x"}`))
	}))
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(context.Background(), http.MethodGet, "/me", nil, nil, &out); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/me" {
		t.Errorf("path = %q, want /api/v1/me", gotPath)
	}
	if gotAuth != "Bearer ffs_testtoken" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotUA != "terraform-provider-featureflip/test" {
		t.Errorf("user-agent = %q", gotUA)
	}
	if out.ID != "x" {
		t.Errorf("decoded id = %q", out.ID)
	}
}

func TestDoParsesErrorEnvelope(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not_found","message":"flag 'new-checkot' not found","docs_url":"https://featureflip.io/docs/api/errors/not_found","did_you_mean":["new-checkout"]}`))
	}))
	err := c.do(context.Background(), http.MethodGet, "/orgs/acme/projects/p/flags/new-checkot", nil, nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("expected not-found APIError, got %v", err)
	}
	if !strings.Contains(err.Error(), "did you mean: new-checkout") {
		t.Errorf("error should surface did_you_mean, got %q", err.Error())
	}
}

func TestDoToleratesNonJSONErrorBody(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("nope"))
	}))
	err := c.do(context.Background(), http.MethodGet, "/me", nil, nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 APIError, got %v", err)
	}
}

func TestDoRetries429WithStableIdempotencyKey(t *testing.T) {
	var keys []string
	var n int
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limited","message":"slow down","retry_after":0}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"p1"}`))
	}))
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(context.Background(), http.MethodPost, "/orgs/acme/projects", nil, map[string]string{"key": "p"}, &out, Idempotent())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
	if keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Errorf("idempotency key must be non-empty and stable across retries: %v", keys)
	}
}

func TestDoDoesNotRetry400(t *testing.T) {
	var n int
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"validation_failed","message":"bad","fields":{"key":["required"]}}`))
	}))
	err := c.do(context.Background(), http.MethodPost, "/orgs/acme/projects", nil, map[string]string{}, nil, Idempotent())
	if n != 1 {
		t.Fatalf("400 must not be retried, got %d attempts", n)
	}
	apiErr := err.(*APIError)
	if apiErr.Fields["key"][0] != "required" {
		t.Errorf("fields not parsed: %+v", apiErr.Fields)
	}
}

func TestListAllFollowsCursor(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q, want 100", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("cursor") == "" {
			w.Write([]byte(`{"items":[{"key":"a"}],"next_cursor":"c2"}`))
		} else {
			w.Write([]byte(`{"items":[{"key":"b"}],"next_cursor":null}`))
		}
	}))
	type item struct {
		Key string `json:"key"`
	}
	items, err := listAll[item](context.Background(), c, "/orgs/acme/projects")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Key != "a" || items[1].Key != "b" {
		t.Errorf("items = %+v", items)
	}
}

func TestDoPreservesEscapedPathSegments(t *testing.T) {
	var gotEscapedPath string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.Write([]byte(`{}`))
	}))
	if err := c.do(context.Background(), http.MethodGet, c.orgPath("projects", "my proj"), nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotEscapedPath != "/api/v1/orgs/acme/projects/my%20proj" {
		t.Errorf("escaped path = %q, want /api/v1/orgs/acme/projects/my%%20proj", gotEscapedPath)
	}
}

func TestDoExhaustsRetriesOn503(t *testing.T) {
	var n int
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"unavailable","message":"down"}`))
	}))
	err := c.do(context.Background(), http.MethodGet, "/me", nil, nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 APIError, got %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 attempts (1 initial + 4 retries), got %d", n)
	}
}

func TestOrgPathEscapes(t *testing.T) {
	c := &Client{Org: "acme"}
	if got := c.orgPath("projects", "my proj"); got != "/orgs/acme/projects/my%20proj" {
		t.Errorf("orgPath = %q", got)
	}
}

func TestAPIErrorRendersFields(t *testing.T) {
	ae := &APIError{
		StatusCode: 422,
		Code:       "validation_failed",
		Message:    "One or more fields are invalid.",
		Fields: map[string][]string{
			"environment": {"LAST_ENVIRONMENT: A project must have at least one environment."},
			"name":        {"required", "must be unique"},
		},
	}
	msg := ae.Error()
	if !strings.Contains(msg, "One or more fields are invalid.") {
		t.Errorf("message missing top-level message: %q", msg)
	}
	if !strings.Contains(msg, "environment: LAST_ENVIRONMENT: A project must have at least one environment.") {
		t.Errorf("message missing environment field: %q", msg)
	}
	if !strings.Contains(msg, "name: required; must be unique") {
		t.Errorf("message missing name field: %q", msg)
	}
	// Deterministic order: "environment" sorts before "name".
	envIdx := strings.Index(msg, "environment:")
	nameIdx := strings.Index(msg, "name:")
	if envIdx == -1 || nameIdx == -1 || envIdx > nameIdx {
		t.Errorf("fields not rendered in sorted-key order: %q", msg)
	}
}

func TestHasCode(t *testing.T) {
	fieldsErr := &APIError{
		StatusCode: 422,
		Code:       "validation_failed",
		Message:    "One or more fields are invalid.",
		Fields: map[string][]string{
			"environment": {"LAST_ENVIRONMENT: A project must have at least one environment."},
		},
	}
	msgErr := &APIError{StatusCode: 409, Code: "conflict", Message: "FLAG_HAS_DEPENDENTS: cannot delete"}
	absentErr := &APIError{StatusCode: 400, Code: "bad_request", Message: "nope"}

	tests := []struct {
		name string
		err  error
		code string
		want bool
	}{
		{"code in fields value", fieldsErr, "LAST_ENVIRONMENT", true},
		{"code in message", msgErr, "FLAG_HAS_DEPENDENTS", true},
		{"absent", absentErr, "LAST_ENVIRONMENT", false},
		{"non-APIError error", errors.New("LAST_ENVIRONMENT boom"), "LAST_ENVIRONMENT", false},
		{"wrapped with %w", fmt.Errorf("wrapping: %w", fieldsErr), "LAST_ENVIRONMENT", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCode(tt.err, tt.code); got != tt.want {
				t.Errorf("HasCode(%v, %q) = %v, want %v", tt.err, tt.code, got, tt.want)
			}
		})
	}
}
