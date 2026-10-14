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

// FlagOwner is the member responsible for a flag. Only Email is managed by
// Terraform; the API resolves it to a user.
type FlagOwner struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
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
	ExpiresAtUtc      *string     `json:"expiresAtUtc"`
	// Owner, IsPermanent and the effective expiry come back on every read;
	// each is written through its own endpoint, never the flag PUT.
	Owner                 *FlagOwner `json:"owner"`
	IsPermanent           bool       `json:"isPermanent"`
	EffectiveExpiresAtUtc *string    `json:"effectiveExpiresAtUtc"`
	ExpirySource          *string    `json:"expirySource"`
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
	ExpiresAtUtc      *string          `json:"expiresAtUtc,omitempty"`
	OwnerEmail        *string          `json:"ownerEmail,omitempty"`
	IsPermanent       bool             `json:"isPermanent"`
}

// UpdateFlagRequest deliberately has no expiry, owner or permanent field: the
// API's flag PUT never touches them, and each has its own endpoints.
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

type setFlagExpiryRequest struct {
	ExpiresAtUtc string `json:"expiresAtUtc"`
}

// SetFlagExpiry sets the flag's advisory expiry date (RFC 3339). The API
// rejects a date that is not in the future with EXPIRY_IN_PAST.
func (c *Client) SetFlagExpiry(ctx context.Context, project, key, expiresAtUtc string) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "flags", key, "expiry"), nil, setFlagExpiryRequest{ExpiresAtUtc: expiresAtUtc}, nil)
}

func (c *Client) ClearFlagExpiry(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "flags", key, "expiry"), nil, nil, nil)
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

type setFlagOwnerRequest struct {
	Email string `json:"email"`
}

// SetFlagOwner names the flag's owner by email. The API refuses an email that
// is not an active member of the organization with OWNER_NOT_MEMBER.
func (c *Client) SetFlagOwner(ctx context.Context, project, key, email string) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "flags", key, "owner"), nil, setFlagOwnerRequest{Email: email}, nil)
}

// ClearFlagOwner leaves the flag unowned. Always allowed, whatever the plan.
func (c *Client) ClearFlagOwner(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "flags", key, "owner"), nil, nil, nil)
}

// SetFlagPermanent marks the flag permanent. Refused with
// PERMANENT_FLAG_HAS_EXPIRY while the flag has an expiry date.
func (c *Client) SetFlagPermanent(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "flags", key, "permanent"), nil, nil, nil)
}

// ClearFlagPermanent makes the flag temporary again. Always allowed.
func (c *Client) ClearFlagPermanent(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "flags", key, "permanent"), nil, nil, nil)
}
