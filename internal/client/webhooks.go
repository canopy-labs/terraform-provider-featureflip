package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Webhook is a subscription as read back from the API. It never carries
// secret material: signing secrets are listed by id and lifecycle only.
type Webhook struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	URL                     string          `json:"url"`
	Provider                string          `json:"provider"`
	IsEnabled               bool            `json:"isEnabled"`
	EventTypes              []string        `json:"eventTypes"`
	ProjectIDs              []string        `json:"projectIds"`
	EnvironmentIDs          []string        `json:"environmentIds"`
	ConsecutiveFailureCount int64           `json:"consecutiveFailureCount"`
	AutoDisabledAt          *string         `json:"autoDisabledAt"`
	CreatedAt               string          `json:"createdAt"`
	UpdatedAt               string          `json:"updatedAt"`
	Secrets                 []WebhookSecret `json:"secrets"`
}

// WebhookSecret is a signing secret's id and lifecycle. A nil RetiredAt means
// the secret is active and signs every delivery.
type WebhookSecret struct {
	ID        string  `json:"id"`
	CreatedAt string  `json:"createdAt"`
	RetiredAt *string `json:"retiredAt"`
}

// CreatedWebhook is the create response — the ONLY place the subscription's
// first signing secret appears; it cannot be re-read afterwards.
type CreatedWebhook struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

// CreatedWebhookSecret is the add-secret response — the only place the new
// secret's plaintext appears.
type CreatedWebhookSecret struct {
	SecretID string `json:"secretId"`
	Secret   string `json:"secret"`
}

// Empty lists mean "all" upstream, so the list fields deliberately omit
// omitempty: callers always send an explicit (possibly empty) array.
type CreateWebhookRequest struct {
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	Provider       string   `json:"provider"`
	EventTypes     []string `json:"eventTypes"`
	ProjectIDs     []string `json:"projectIds"`
	EnvironmentIDs []string `json:"environmentIds"`
}

// UpdateWebhookRequest is a full replace. IsEnabled is required upstream (a
// missing value is a 400, never a silent disable).
type UpdateWebhookRequest struct {
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	EventTypes     []string `json:"eventTypes"`
	ProjectIDs     []string `json:"projectIds"`
	EnvironmentIDs []string `json:"environmentIds"`
	IsEnabled      bool     `json:"isEnabled"`
}

func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (*CreatedWebhook, error) {
	var out CreatedWebhook
	if err := c.do(ctx, http.MethodPost, c.orgPath("webhooks"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	var out Webhook
	if err := c.do(ctx, http.MethodGet, c.orgPath("webhooks", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateWebhook(ctx context.Context, id string, req UpdateWebhookRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("webhooks", id), nil, req, nil)
}

func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("webhooks", id), nil, nil, nil)
}

func (c *Client) ListWebhookEventTypes(ctx context.Context) ([]string, error) {
	var out []string
	if err := c.do(ctx, http.MethodGet, c.orgPath("webhooks", "event-types"), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddWebhookSecret adds a new active signing secret. Existing secrets keep
// signing until retired, so receivers can switch over without dropping
// deliveries.
func (c *Client) AddWebhookSecret(ctx context.Context, webhookID string) (*CreatedWebhookSecret, error) {
	var out CreatedWebhookSecret
	if err := c.do(ctx, http.MethodPost, c.orgPath("webhooks", webhookID, "secrets"), nil, nil, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

// RetireWebhookSecret retires a signing secret. Upstream refuses (400) to
// retire a subscription's last active secret; retiring an already-retired
// secret is a no-op.
func (c *Client) RetireWebhookSecret(ctx context.Context, webhookID, secretID string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("webhooks", webhookID, "secrets", secretID), nil, nil, nil)
}

// IsLastActiveSecret reports whether err is upstream's refusal to retire a
// subscription's last active signing secret. That refusal carries no guard
// code, only its message, so the message text is what identifies it.
func IsLastActiveSecret(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(ae.Message), "last active signing secret")
}
