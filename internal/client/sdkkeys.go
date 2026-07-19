package client

import (
	"context"
	"net/http"
)

type SDKKey struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        *string `json:"description"`
	Type               string  `json:"type"`
	LastFourCharacters string  `json:"lastFourCharacters"`
	IsActive           bool    `json:"isActive"`
}

// CreatedSDKKey is the create response — the ONLY place Key (plaintext)
// ever appears; it cannot be re-read afterwards.
type CreatedSDKKey struct {
	ID                 string `json:"id"`
	Key                string `json:"key"`
	LastFourCharacters string `json:"lastFourCharacters"`
	Type               string `json:"type"`
	Name               string `json:"name"`
}

type CreateSDKKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Type        string  `json:"type"`
}

type UpdateSDKKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (c *Client) sdkKeyPath(project, env string, extra ...string) string {
	parts := append([]string{"projects", project, "environments", env, "sdk-keys"}, extra...)
	return c.orgPath(parts...)
}

func (c *Client) CreateSDKKey(ctx context.Context, project, env string, req CreateSDKKeyRequest) (*CreatedSDKKey, error) {
	var out CreatedSDKKey
	if err := c.do(ctx, http.MethodPost, c.sdkKeyPath(project, env), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSDKKey(ctx context.Context, project, env, id string) (*SDKKey, error) {
	var out SDKKey
	if err := c.do(ctx, http.MethodGet, c.sdkKeyPath(project, env, id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateSDKKey(ctx context.Context, project, env, id string, req UpdateSDKKeyRequest) error {
	return c.do(ctx, http.MethodPut, c.sdkKeyPath(project, env, id), nil, req, nil)
}

func (c *Client) RevokeSDKKey(ctx context.Context, project, env, id string) error {
	return c.do(ctx, http.MethodPost, c.sdkKeyPath(project, env, id, "revoke"), nil, nil, nil)
}
