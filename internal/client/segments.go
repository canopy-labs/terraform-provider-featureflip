package client

import (
	"context"
	"net/http"
)

type Segment struct {
	ID          string      `json:"id"`
	Key         string      `json:"key"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Conditions  []Condition `json:"conditions"`
}

type CreateSegmentRequest struct {
	Key         string      `json:"key"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Conditions  []Condition `json:"conditions"`
}

type UpdateSegmentRequest struct {
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Conditions  []Condition `json:"conditions"`
}

func (c *Client) CreateSegment(ctx context.Context, project string, req CreateSegmentRequest) (*Segment, error) {
	var out Segment
	if err := c.do(ctx, http.MethodPost, c.orgPath("projects", project, "segments"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSegment(ctx context.Context, project, key string) (*Segment, error) {
	var out Segment
	if err := c.do(ctx, http.MethodGet, c.orgPath("projects", project, "segments", key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateSegment(ctx context.Context, project, key string, req UpdateSegmentRequest) error {
	return c.do(ctx, http.MethodPut, c.orgPath("projects", project, "segments", key), nil, req, nil)
}

func (c *Client) DeleteSegment(ctx context.Context, project, key string) error {
	return c.do(ctx, http.MethodDelete, c.orgPath("projects", project, "segments", key), nil, nil, nil)
}

func (c *Client) ListSegments(ctx context.Context, project string) ([]Segment, error) {
	return listAll[Segment](ctx, c, c.orgPath("projects", project, "segments"))
}
