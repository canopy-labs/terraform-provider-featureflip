package client

import (
	"context"
	"net/http"
)

// RampStep is one rung of a rollout ramp: the percentage to apply, then how
// long to hold it before the next one. The last step's hold is not used.
type RampStep struct {
	Percent     int64 `json:"percent"`
	HoldMinutes int64 `json:"holdMinutes"`
}

// RolloutRamp is a ramp as read back from the API. CurrentStepIndex is the
// index of the last step applied, -1 before the first.
type RolloutRamp struct {
	ID                string     `json:"id"`
	RuleID            *string    `json:"ruleId"`
	IsDefaultRule     bool       `json:"isDefaultRule"`
	TargetVariationID *string    `json:"targetVariationId"`
	Status            string     `json:"status"`
	Steps             []RampStep `json:"steps"`
	CurrentStepIndex  int64      `json:"currentStepIndex"`
	NextStepAt        *string    `json:"nextStepAt"`
	PausedReason      *string    `json:"pausedReason"`
	PausedDetail      *string    `json:"pausedDetail"`
	CreatedAt         string     `json:"createdAt"`
	CompletedAt       *string    `json:"completedAt"`
	AbortedAt         *string    `json:"abortedAt"`
}

// Finished reports whether the ramp can no longer change anything.
func (r *RolloutRamp) Finished() bool {
	return r.Status == "Completed" || r.Status == "Aborted"
}

type StartRuleRampRequest struct {
	Steps []RampStep `json:"steps"`
}

// StartDefaultRuleRampRequest: VariationID and BucketBy may be null, in which
// case the default rule's existing rollout is used.
type StartDefaultRuleRampRequest struct {
	Steps       []RampStep `json:"steps"`
	VariationID *string    `json:"variationId"`
	BucketBy    *string    `json:"bucketBy"`
}

type abortRampRequest struct {
	Percent int64 `json:"percent"`
}

func (c *Client) rampEnvPath(project, flag, env string, extra ...string) string {
	parts := append([]string{"projects", project, "flags", flag, "environments", env}, extra...)
	return c.orgPath(parts...)
}

func (c *Client) StartRuleRamp(ctx context.Context, project, flag, env, ruleID string, req StartRuleRampRequest) (*RolloutRamp, error) {
	var out RolloutRamp
	if err := c.do(ctx, http.MethodPost, c.rampEnvPath(project, flag, env, "rules", ruleID, "ramp"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StartDefaultRuleRamp(ctx context.Context, project, flag, env string, req StartDefaultRuleRampRequest) (*RolloutRamp, error) {
	var out RolloutRamp
	if err := c.do(ctx, http.MethodPost, c.rampEnvPath(project, flag, env, "default-rule", "ramp"), nil, req, &out, Idempotent()); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetRolloutRamp(ctx context.Context, project, flag, env, id string) (*RolloutRamp, error) {
	var out RolloutRamp
	if err := c.do(ctx, http.MethodGet, c.rampEnvPath(project, flag, env, "ramps", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AbortRolloutRamp stops a ramp and leaves its rule at percent, which may
// only keep or lower the current rollout. A finished ramp answers 409.
func (c *Client) AbortRolloutRamp(ctx context.Context, project, flag, env, id string, percent int64) (*RolloutRamp, error) {
	var out RolloutRamp
	if err := c.do(ctx, http.MethodPost, c.rampEnvPath(project, flag, env, "ramps", id, "abort"), nil, abortRampRequest{Percent: percent}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
