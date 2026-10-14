package client

import (
	"context"
	"net/http"
)

type Project struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	// DefaultFlagLifetimeDays is written through SetDefaultFlagLifetime, never
	// the project PUT.
	DefaultFlagLifetimeDays *int `json:"defaultFlagLifetimeDays"`
}

type CreateProjectRequest struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type UpdateProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, c.orgPath("projects"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetProject(ctx context.Context, key string) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodGet, c.orgPath("projects", key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateProject(ctx context.Context, key string, req UpdateProjectRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", key), nil, req, nil)
}

func (c *Client) DeleteProject(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", key), nil, nil, nil)
}

// setDefaultFlagLifetimeRequest has no omitempty on purpose: the API requires
// days, so removal must be sent as an explicit {"days":null}.
type setDefaultFlagLifetimeRequest struct {
	Days *int `json:"days"`
}

// SetDefaultFlagLifetime sets how many days a temporary flag with no expiry of
// its own lives before it counts as expired; nil removes the default.
func (c *Client) SetDefaultFlagLifetime(ctx context.Context, key string, days *int) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", key, "default-flag-lifetime"), nil, setDefaultFlagLifetimeRequest{Days: days}, nil)
}
