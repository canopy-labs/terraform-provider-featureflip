package client

import (
	"context"
	"net/http"
)

// Me is the type-discriminated caller identity from GET /me. Type is
// "user" (session/JWT or ffp_ PAT; carries Email/EmailVerified) or
// "service_token" (ffs_; carries OrganizationID/Role/ProjectScope —
// ProjectScope nil means unrestricted). Type-inapplicable fields are
// omitted from the response.
type Me struct {
	Type           string   `json:"type"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Email          *string  `json:"email"`
	EmailVerified  *bool    `json:"emailVerified"`
	OrganizationID *string  `json:"organizationId"`
	Role           *string  `json:"role"`
	ProjectScope   []string `json:"projectScope"`
}

// Me calls GET /me. The provider's configure-time fail-fast uses
// GetOrganization instead — it additionally validates the configured org
// slug and that the token belongs to that org.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type Organization struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Plan     string `json:"plan"`
	IsActive bool   `json:"isActive"`
}

func (c *Client) GetOrganization(ctx context.Context, slug string) (*Organization, error) {
	var out Organization
	if err := c.do(ctx, http.MethodGet, "/orgs/"+pathEscape(slug), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
