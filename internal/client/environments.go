package client

import (
	"context"
	"net/http"
)

type Environment struct {
	ID        string  `json:"id"`
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	SortOrder int64   `json:"sortOrder"`
}

type CreateEnvironmentRequest struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	SortOrder int64   `json:"sortOrder"`
}

type UpdateEnvironmentRequest struct {
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	SortOrder int64   `json:"sortOrder"`
}

func (c *Client) CreateEnvironment(ctx context.Context, project string, req CreateEnvironmentRequest) (*Environment, error) {
	var out Environment
	if err := c.do(ctx, http.MethodPost, c.orgPath("projects", project, "environments"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEnvironment(ctx context.Context, project, key string) (*Environment, error) {
	var out Environment
	if err := c.do(ctx, http.MethodGet, c.orgPath("projects", project, "environments", key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateEnvironment(ctx context.Context, project, key string, req UpdateEnvironmentRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "environments", key), nil, req, nil)
}

func (c *Client) DeleteEnvironment(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "environments", key), nil, nil, nil)
}
