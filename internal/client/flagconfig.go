package client

import (
	"context"
	"net/http"
)

type Prerequisite struct {
	PrerequisiteFlagKey  string `json:"prerequisiteFlagKey"`
	ExpectedVariationKey string `json:"expectedVariationKey"`
}

type FlagEnvConfig struct {
	EnvironmentID      string         `json:"environmentId"`
	EnvironmentKey     string         `json:"environmentKey"`
	IsEnabled          bool           `json:"isEnabled"`
	DefaultVariationID string         `json:"defaultVariationId"`
	Strategy           string         `json:"strategy"`
	Prerequisites      []Prerequisite `json:"prerequisites"`
}

// UpdateFlagEnvConfigRequest: Prerequisites has no omitempty on purpose —
// nil marshals to null (leave prerequisites untouched) while an empty
// slice marshals to [] (clear them).
type UpdateFlagEnvConfigRequest struct {
	DefaultVariationID string         `json:"defaultVariationId"`
	Strategy           *string        `json:"strategy"`
	Prerequisites      []Prerequisite `json:"prerequisites"`
}

type Condition struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"`
	Values    []string `json:"values"`
	Negate    bool     `json:"negate"`
}

type ConditionGroup struct {
	Operator   string      `json:"operator"`
	Conditions []Condition `json:"conditions"`
}

type TargetingRule struct {
	ID                string           `json:"id"`
	Priority          int              `json:"priority"`
	Description       *string          `json:"description"`
	VariationID       string           `json:"variationId"`
	RolloutPercentage *int64           `json:"rolloutPercentage"`
	UserSegmentID     *string          `json:"userSegmentId"`
	ConditionGroups   []ConditionGroup `json:"conditionGroups"`
}

type TargetingConfig struct {
	IsEnabled bool            `json:"isEnabled"`
	Rules     []TargetingRule `json:"rules"`
}

type RuleInput struct {
	Description       *string          `json:"description"`
	VariationID       string           `json:"variationId"`
	RolloutPercentage *int64           `json:"rolloutPercentage"`
	UserSegmentID     *string          `json:"userSegmentId"`
	ConditionGroups   []ConditionGroup `json:"conditionGroups"`
}

func (c *Client) flagEnvPath(project, flag, env string, extra ...string) string {
	parts := append([]string{"projects", project, "flags", flag, "environments", env}, extra...)
	return c.orgPath(parts...)
}

func (c *Client) GetFlagEnvConfig(ctx context.Context, project, flag, env string) (*FlagEnvConfig, error) {
	var out FlagEnvConfig
	if err := c.do(ctx, http.MethodGet, c.flagEnvPath(project, flag, env), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateFlagEnvConfig(ctx context.Context, project, flag, env string, req UpdateFlagEnvConfigRequest) error {
	return c.do(ctx, http.MethodPut, c.flagEnvPath(project, flag, env), nil, req, nil)
}

func (c *Client) ToggleFlag(ctx context.Context, project, flag, env string, enabled bool) error {
	return c.do(ctx, http.MethodPost, c.flagEnvPath(project, flag, env, "toggle"), nil, map[string]bool{"enabled": enabled}, nil)
}

func (c *Client) GetTargeting(ctx context.Context, project, flag, env string) (*TargetingConfig, error) {
	var out TargetingConfig
	if err := c.do(ctx, http.MethodGet, c.flagEnvPath(project, flag, env, "targeting"), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateRule(ctx context.Context, project, flag, env string, req RuleInput) (*TargetingRule, error) {
	var out TargetingRule
	if err := c.do(ctx, http.MethodPost, c.flagEnvPath(project, flag, env, "rules"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateRule(ctx context.Context, project, flag, env, ruleID string, req RuleInput) error {
	return c.do(ctx, http.MethodPut, c.flagEnvPath(project, flag, env, "rules", ruleID), nil, req, nil)
}

func (c *Client) DeleteRule(ctx context.Context, project, flag, env, ruleID string) error {
	return c.do(ctx, http.MethodDelete, c.flagEnvPath(project, flag, env, "rules", ruleID), nil, nil, nil)
}

func (c *Client) ReorderRules(ctx context.Context, project, flag, env string, ruleIDs []string) error {
	return c.do(ctx, http.MethodPut, c.flagEnvPath(project, flag, env, "rules", "reorder"), nil, map[string][]string{"ruleIds": ruleIDs}, nil)
}
