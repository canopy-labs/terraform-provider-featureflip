package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The API stores expiry as a Postgres timestamptz, so it keeps microseconds
// and always answers in UTC ("Z"). Two spellings of one instant — an offset
// instead of Z, or finer-than-microsecond precision — must not read as drift.
const expiryPrecision = time.Microsecond

func parseExpiry(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// sameExpiryInstant reports whether two RFC 3339 strings name the same
// instant at the precision the API stores.
func sameExpiryInstant(a, b string) bool {
	ta, errA := parseExpiry(a)
	tb, errB := parseExpiry(b)
	if errA != nil || errB != nil {
		return a == b
	}
	d := ta.Sub(tb)
	return d > -expiryPrecision && d < expiryPrecision
}

// expiryToModel turns the API's expiresAtUtc into state. When prior (plan or
// state) names the same instant, prior's spelling is kept so Terraform sees no
// diff; otherwise the server value wins.
func expiryToModel(prior types.String, server *string) types.String {
	if server == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && sameExpiryInstant(prior.ValueString(), *server) {
		return prior
	}
	return types.StringValue(*server)
}

// expiryChanged reports whether the planned expiry differs from state.
func expiryChanged(plan, state types.String) bool {
	if plan.IsNull() || state.IsNull() {
		return plan.IsNull() != state.IsNull()
	}
	return !sameExpiryInstant(plan.ValueString(), state.ValueString())
}

// expiryWireValue normalizes a configured expiry to UTC for the request body,
// so the API never has to interpret an offset.
func expiryWireValue(v types.String) (string, error) {
	t, err := parseExpiry(v.ValueString())
	if err != nil {
		return "", err
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

// rfc3339Validator rejects values that are not RFC 3339 timestamps. It does
// not reject past dates: a plan can be applied long after it is made, so that
// check belongs to the API at apply time (EXPIRY_IN_PAST).
type rfc3339Validator struct{}

func (rfc3339Validator) Description(context.Context) string {
	return "value must be an RFC 3339 timestamp, e.g. 2027-01-31T00:00:00Z"
}

func (v rfc3339Validator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v rfc3339Validator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := parseExpiry(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid RFC 3339 timestamp",
			fmt.Sprintf("%q is not an RFC 3339 timestamp (for example 2027-01-31T00:00:00Z): %s", req.ConfigValue.ValueString(), err))
	}
}
