package provider

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

// ownerToModel turns the API's owner into state. Emails compare
// case-insensitively, so when prior names the same address its spelling is
// kept and Terraform sees no diff.
func ownerToModel(prior types.String, owner *client.FlagOwner) types.String {
	if owner == nil || owner.Email == "" {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), owner.Email) {
		return prior
	}
	return types.StringValue(owner.Email)
}

// ownerChanged reports whether the planned owner differs from state. "" and
// null both mean unowned.
func ownerChanged(plan, state types.String) bool {
	return !strings.EqualFold(plan.ValueString(), state.ValueString())
}

// ownerEmailCaseInsensitive plans the prior value when the configured
// owner_email differs from it only in case. Emails compare
// case-insensitively, so this keeps an import, or a config spelled
// differently from the API's stored case, from planning a no-op change.
// Terraform accepts a planned value equal to the prior one in place of the
// configured value.
type ownerEmailCaseInsensitive struct{}

func (ownerEmailCaseInsensitive) Description(context.Context) string {
	return "owner_email values that differ only in case are the same owner"
}

func (m ownerEmailCaseInsensitive) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (ownerEmailCaseInsensitive) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	cfg, prior := req.ConfigValue.ValueString(), req.StateValue.ValueString()
	if cfg != prior && strings.EqualFold(cfg, prior) {
		resp.PlanValue = req.StateValue
	}
}

// permanentConflictsWithExpiry reports a config the API would refuse with
// PERMANENT_FLAG_HAS_EXPIRY. Unknown values pass; the API checks at apply.
func permanentConflictsWithExpiry(permanent types.Bool, expiresAt types.String) bool {
	return !permanent.IsNull() && !permanent.IsUnknown() && permanent.ValueBool() &&
		!expiresAt.IsNull() && !expiresAt.IsUnknown()
}

func lifetimeToModel(days *int) types.Int64 {
	if days == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*days))
}

// lifetimeChanged reports whether the planned default lifetime differs from
// state. 0 and null both mean "no default", so a project without one never
// calls the endpoint (setting it can be refused on plans without the feature).
func lifetimeChanged(plan, state types.Int64) bool {
	if plan.IsUnknown() {
		return false
	}
	return plan.ValueInt64() != state.ValueInt64()
}

// lifetimeWireValue is the request's days: 0 removes the default (null).
func lifetimeWireValue(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() || v.ValueInt64() == 0 {
		return nil
	}
	d := int(v.ValueInt64())
	return &d
}

// lifetimeValidator accepts 0 (no default) or 1–3650 days.
func lifetimeValidator() validator.Int64 {
	return int64validator.Between(0, 3650)
}

// wireFieldAttribute maps the API's request field names to the attributes
// that set them, so a rejection can name what the user wrote.
var wireFieldAttribute = map[string]string{
	"expiresAtUtc": "expires_at",
	"isPermanent":  "permanent",
	"owner":        "owner_email",
	"ownerEmail":   "owner_email",
	"email":        "owner_email",
	"days":         "default_flag_lifetime_days",
}

// attributeFeature names the feature behind each lifecycle attribute.
var attributeFeature = map[string]string{
	"expires_at":                 "Flag expiration dates",
	"permanent":                  "Permanent flags",
	"owner_email":                "Flag owners",
	"default_flag_lifetime_days": "Default flag lifetimes",
}

// rejectedAttribute is the attribute whose field carries code, or fallback
// when the error doesn't say (one create body carries several gated fields).
func rejectedAttribute(err error, code, fallback string) string {
	var ae *client.APIError
	if errors.As(err, &ae) {
		for field, msgs := range ae.Fields {
			for _, m := range msgs {
				if attr, ok := wireFieldAttribute[field]; ok && strings.Contains(m, code) {
					return attr
				}
			}
		}
	}
	return fallback
}

// withLifecycleHint appends guidance to the API's rejections of the
// lifecycle attributes. attribute names the one being written when the
// error itself doesn't identify it.
func withLifecycleHint(err error, attribute string) string {
	msg := err.Error()
	switch {
	case client.HasCode(err, "EXPIRY_IN_PAST"):
		msg += "\n\nexpires_at must be in the future when it is set or changed. Pick a later date, or remove expires_at to clear it."
	case client.HasCode(err, "PERMANENT_FLAG_HAS_EXPIRY"):
		msg += "\n\nA permanent flag can't have an expiry date. Remove expires_at, or set permanent = false."
	case client.HasCode(err, "OWNER_NOT_MEMBER"):
		msg += "\n\nowner_email must be the email of an active member of the organization."
	case client.HasCode(err, "PLAN_FEATURE_UNAVAILABLE"):
		attr := rejectedAttribute(err, "PLAN_FEATURE_UNAVAILABLE", attribute)
		msg += "\n\nFlag owners need the Pro plan or above. Remove " + attr + ", or upgrade the organization."
	case client.HasCode(err, "FEATURE_NOT_ENABLED"):
		attr := rejectedAttribute(err, "FEATURE_NOT_ENABLED", attribute)
		feature := attributeFeature[attr]
		if feature == "" {
			feature = "These features"
		}
		msg += "\n\n" + feature + " are not enabled for this organization yet. Remove " + attr + " until they are."
	}
	return msg
}

// ownerEmailValidator accepts "" (no owner) or an email with no surrounding
// whitespace. The API trims the value, so a padded one would come back
// different from the plan.
func ownerEmailValidator() validator.String {
	return stringvalidator.RegexMatches(regexp.MustCompile(`^$|^\S+@\S+$`),
		`must be "" (no owner) or an email address with no surrounding whitespace`)
}

// shouldClearDefaultOwner reports whether a just-created flag has an owner the
// config asked it not to have: the API makes a member caller the owner by
// default, and owner_email = "" means unowned. An omitted owner_email keeps
// whatever the API assigned.
func shouldClearDefaultOwner(planOwner types.String, f *client.Flag) bool {
	return !planOwner.IsNull() && !planOwner.IsUnknown() && planOwner.ValueString() == "" && f.Owner != nil
}
