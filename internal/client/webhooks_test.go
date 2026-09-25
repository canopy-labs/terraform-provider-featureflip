package client

import (
	"context"
	"errors"
	"testing"
)

func TestWebhookLifecycle(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 201, `{"id":"w1","secret":"whsec_once","warning":"Store this secret securely"}`)
	created, err := c.CreateWebhook(ctx, CreateWebhookRequest{
		Name: "audit", URL: "https://example.com/hook", Provider: "GenericHttp",
		EventTypes: []string{}, ProjectIDs: []string{"p1"}, EnvironmentIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.Method != "POST" || cap.Path != "/api/v1/orgs/acme/webhooks" {
		t.Errorf("create: %s %s", cap.Method, cap.Path)
	}
	if created.ID != "w1" || created.Secret != "whsec_once" {
		t.Errorf("created = %+v", created)
	}
	// Empty lists mean "all" upstream and must go out as [], never null or absent.
	for _, k := range []string{"eventTypes", "environmentIds"} {
		v, ok := cap.Body[k].([]any)
		if !ok || len(v) != 0 {
			t.Errorf("create body %s = %#v, want []", k, cap.Body[k])
		}
	}

	c = record(t, &cap, 200, `{"id":"w1","name":"audit","url":"https://example.com/hook","provider":"GenericHttp",
		"isEnabled":false,"eventTypes":["flag.toggled"],"projectIds":["p1"],"environmentIds":[],
		"consecutiveFailureCount":20,"autoDisabledAt":"2026-09-24T10:00:00Z","createdAt":"c","updatedAt":"u",
		"secrets":[{"id":"s1","createdAt":"c","retiredAt":null},{"id":"s0","createdAt":"c","retiredAt":"r"}]}`)
	w, err := c.GetWebhook(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/webhooks/w1" {
		t.Errorf("get path = %s", cap.Path)
	}
	if w.IsEnabled || w.AutoDisabledAt == nil || len(w.Secrets) != 2 || w.Secrets[0].RetiredAt != nil || w.Secrets[1].RetiredAt == nil {
		t.Errorf("get = %+v", w)
	}

	c = record(t, &cap, 204, "")
	if err := c.UpdateWebhook(ctx, "w1", UpdateWebhookRequest{Name: "audit", URL: "https://example.com/hook",
		EventTypes: []string{}, ProjectIDs: []string{}, EnvironmentIDs: []string{}, IsEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "PUT" || cap.Path != "/api/v1/orgs/acme/webhooks/w1" {
		t.Errorf("update: %s %s", cap.Method, cap.Path)
	}
	// isEnabled is required upstream; false must be sent explicitly, not dropped.
	if v, ok := cap.Body["isEnabled"].(bool); !ok || v {
		t.Errorf("update isEnabled = %#v, want false", cap.Body["isEnabled"])
	}

	c = record(t, &cap, 204, "")
	if err := c.DeleteWebhook(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" || cap.Path != "/api/v1/orgs/acme/webhooks/w1" {
		t.Errorf("delete: %s %s", cap.Method, cap.Path)
	}
}

func TestWebhookSecretsAndEventTypes(t *testing.T) {
	var cap captured
	ctx := context.Background()

	c := record(t, &cap, 201, `{"secretId":"s2","secret":"whsec_next","warning":"x"}`)
	s, err := c.AddWebhookSecret(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if cap.Method != "POST" || cap.Path != "/api/v1/orgs/acme/webhooks/w1/secrets" || s.SecretID != "s2" || s.Secret != "whsec_next" {
		t.Errorf("add secret: %s %s %+v", cap.Method, cap.Path, s)
	}

	c = record(t, &cap, 204, "")
	if err := c.RetireWebhookSecret(ctx, "w1", "s1"); err != nil {
		t.Fatal(err)
	}
	if cap.Method != "DELETE" || cap.Path != "/api/v1/orgs/acme/webhooks/w1/secrets/s1" {
		t.Errorf("retire: %s %s", cap.Method, cap.Path)
	}

	c = record(t, &cap, 200, `["flag.created","flag.toggled"]`)
	types, err := c.ListWebhookEventTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/api/v1/orgs/acme/webhooks/event-types" || len(types) != 2 || types[1] != "flag.toggled" {
		t.Errorf("event types: %s %v", cap.Path, types)
	}
}

func TestIsLastActiveSecret(t *testing.T) {
	var cap captured
	c := record(t, &cap, 400, `{"error":"bad_request","message":"Cannot retire the last active signing secret."}`)
	err := c.RetireWebhookSecret(context.Background(), "w1", "s1")
	if !IsLastActiveSecret(err) {
		t.Errorf("IsLastActiveSecret(%v) = false, want true", err)
	}

	for _, other := range []error{
		nil,
		errors.New("Cannot retire the last active signing secret."),
		&APIError{StatusCode: 400, Code: "bad_request", Message: "Secret does not belong to this subscription."},
		&APIError{StatusCode: 404, Code: "not_found", Message: "last active signing secret"},
	} {
		if IsLastActiveSecret(other) {
			t.Errorf("IsLastActiveSecret(%v) = true, want false", other)
		}
	}
}
