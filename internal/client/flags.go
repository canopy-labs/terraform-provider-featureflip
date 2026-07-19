package client

import (
	"context"
	"net/http"
)

type Variation struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Value       string  `json:"value"`
	Description *string `json:"description"`
}

type Flag struct {
	ID                string      `json:"id"`
	Key               string      `json:"key"`
	Name              string      `json:"name"`
	Description       *string     `json:"description"`
	Type              string      `json:"type"`
	IsArchived        bool        `json:"isArchived"`
	ClientSideVisible bool        `json:"clientSideVisible"`
	Tags              []string    `json:"tags"`
	Variations        []Variation `json:"variations"`
}

type VariationInput struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Value       string  `json:"value"`
	Description *string `json:"description"`
}

type CreateFlagRequest struct {
	Key               string           `json:"key"`
	Name              string           `json:"name"`
	Type              string           `json:"type"`
	Description       *string          `json:"description"`
	Tags              []string         `json:"tags"`
	InitialVariations []VariationInput `json:"initialVariations,omitempty"`
	ClientSideVisible bool             `json:"clientSideVisible"`
}

type UpdateFlagRequest struct {
	Name              string   `json:"name"`
	Description       *string  `json:"description"`
	Tags              []string `json:"tags"`
	ClientSideVisible bool     `json:"clientSideVisible"`
}

type UpdateVariationRequest struct {
	Name        string  `json:"name"`
	Value       string  `json:"value"`
	Description *string `json:"description"`
}

func (c *Client) CreateFlag(ctx context.Context, project string, req CreateFlagRequest) (*Flag, error) {
	var out Flag
	if err := c.do(ctx, http.MethodPost, c.orgPath("projects", project, "flags"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetFlag(ctx context.Context, project, key string) (*Flag, error) {
	var out Flag
	if err := c.do(ctx, http.MethodGet, c.orgPath("projects", project, "flags", key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateFlag(ctx context.Context, project, key string, req UpdateFlagRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "flags", key), nil, req, nil)
}

func (c *Client) DeleteFlag(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "flags", key), nil, nil, nil)
}

func (c *Client) ArchiveFlag(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodPost, c.orgPath("projects", project, "flags", key, "archive"), nil, nil, nil)
}

func (c *Client) RestoreFlag(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodPost, c.orgPath("projects", project, "flags", key, "restore"), nil, nil, nil)
}

func (c *Client) AddVariation(ctx context.Context, project, flag string, req VariationInput) (*Variation, error) {
	var out Variation
	if err := c.do(ctx, http.MethodPost, c.orgPath("projects", project, "flags", flag, "variations"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateVariation(ctx context.Context, project, flag, variationID string, req UpdateVariationRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "flags", flag, "variations", variationID), nil, req, nil)
}

func (c *Client) DeleteVariation(ctx context.Context, project, flag, variationID string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "flags", flag, "variations", variationID), nil, nil, nil)
}
