package client

import (
	"context"
	"net/http"
)

// FlagTrigger is a trigger as read back from the API. It never carries the
// secret: only its last four characters.
type FlagTrigger struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	Action         string  `json:"action"`
	Enabled        bool    `json:"enabled"`
	SecretLastFour string  `json:"secretLastFour"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	LastFiredAt    *string `json:"lastFiredAt"`
}

// FlagTriggerWithURL is the create and rotate response — the ONLY place the
// trigger's URL and secret appear; they cannot be re-read afterwards.
type FlagTriggerWithURL struct {
	Trigger  FlagTrigger `json:"trigger"`
	URL      string      `json:"url"`
	Endpoint string      `json:"endpoint"`
	Secret   string      `json:"secret"`
}

type CreateFlagTriggerRequest struct {
	Name        string  `json:"name"`
	Action      string  `json:"action"`
	Description *string `json:"description"`
}

// UpdateFlagTriggerRequest replaces all three fields. Enabled is required
// upstream (a missing one is a 400, not false), so it is never omitted.
type UpdateFlagTriggerRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Enabled     bool    `json:"enabled"`
}

func (c *Client) flagTriggerPath(project, flag, env string, extra ...string) string {
	parts := append([]string{"projects", project, "flags", flag, "environments", env, "triggers"}, extra...)
	return c.orgPath(parts...)
}

func (c *Client) CreateFlagTrigger(ctx context.Context, project, flag, env string, req CreateFlagTriggerRequest) (*FlagTriggerWithURL, error) {
	var out FlagTriggerWithURL
	if err := c.do(ctx, http.MethodPost, c.flagTriggerPath(project, flag, env), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetFlagTrigger(ctx context.Context, project, flag, env, id string) (*FlagTrigger, error) {
	var out FlagTrigger
	if err := c.do(ctx, http.MethodGet, c.flagTriggerPath(project, flag, env, id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateFlagTrigger(ctx context.Context, project, flag, env, id string, req UpdateFlagTriggerRequest) (*FlagTrigger, error) {
	var out FlagTrigger
	if err := c.do(ctx, http.MethodPut, c.flagTriggerPath(project, flag, env, id), nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RotateFlagTrigger replaces the trigger's secret. The old URL stops working
// at once; the new one is in the response and nowhere else.
func (c *Client) RotateFlagTrigger(ctx context.Context, project, flag, env, id string) (*FlagTriggerWithURL, error) {
	var out FlagTriggerWithURL
	if err := c.do(ctx, http.MethodPost, c.flagTriggerPath(project, flag, env, id, "rotate"), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteFlagTrigger(ctx context.Context, project, flag, env, id string) error {
	return c.do(ctx, http.MethodDelete, c.flagTriggerPath(project, flag, env, id), nil, nil, nil)
}
