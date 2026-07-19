package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

// reorderConditionsLike reorders actual to match prior's element order,
// matching by full value equality. Extra elements in actual that have no
// match in prior are appended at the end in their original order; elements
// of prior with no match in actual are simply dropped (they were removed
// server-side).
//
// The API does not guarantee stable ordering of a segment's (or targeting
// rule's) condition list between requests — the same persisted set of
// conditions can come back in a different order on repeated calls, and the
// order returned immediately after a create/update frequently does not match
// the order submitted. Since Terraform's `conditions` attribute is an
// ordered List (matching the shared conditionsFromList/conditionsToList
// contract), returning the API's raw order verbatim causes spurious
// "Provider produced inconsistent result after apply" errors and drift on
// refresh. Reordering the API's response to match the last known-good order
// (the plan on Create/Update, the prior state on Read) keeps the List stable
// without changing its element type or the shared conversion functions'
// signatures.
func reorderConditionsLike(prior, actual []client.Condition) []client.Condition {
	used := make([]bool, len(actual))
	out := make([]client.Condition, 0, len(actual))
	for _, p := range prior {
		for i, a := range actual {
			if used[i] {
				continue
			}
			if conditionsEqual(p, a) {
				out = append(out, a)
				used[i] = true
				break
			}
		}
	}
	for i, a := range actual {
		if !used[i] {
			out = append(out, a)
		}
	}
	return out
}

// reorderConditionGroupsLike realigns actual condition groups to prior's
// order, and each matched group's conditions to the prior group's order.
// A group matches when operators are equal and its conditions are a
// multiset-equal permutation of the prior group's. Lossless like
// reorderConditionsLike: unmatched actual groups append in original order.
func reorderConditionGroupsLike(prior, actual []client.ConditionGroup) []client.ConditionGroup {
	used := make([]bool, len(actual))
	out := make([]client.ConditionGroup, 0, len(actual))
	for _, p := range prior {
		for i, a := range actual {
			if used[i] {
				continue
			}
			if a.Operator != p.Operator || len(a.Conditions) != len(p.Conditions) {
				continue
			}
			realigned := reorderConditionsLike(p.Conditions, a.Conditions)
			match := true
			for j := range p.Conditions {
				if !conditionsEqual(p.Conditions[j], realigned[j]) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			out = append(out, client.ConditionGroup{Operator: a.Operator, Conditions: realigned})
			used[i] = true
			break
		}
	}
	for i, a := range actual {
		if !used[i] {
			out = append(out, a)
		}
	}
	return out
}

func conditionsEqual(a, b client.Condition) bool {
	if a.Attribute != b.Attribute || a.Operator != b.Operator || a.Negate != b.Negate || len(a.Values) != len(b.Values) {
		return false
	}
	for i := range a.Values {
		if a.Values[i] != b.Values[i] {
			return false
		}
	}
	return true
}

// operatorValues is the API's OperatorType allowlist, canonical casing.
var operatorValues = []string{
	"Equals", "NotEquals", "Contains", "NotContains",
	"GreaterThan", "LessThan", "GreaterThanOrEqual", "LessThanOrEqual",
	"In", "NotIn", "MatchesRegex", "StartsWith", "EndsWith",
	"Before", "After",
	"SemverEquals", "SemverGreaterThan", "SemverGreaterThanOrEqual",
	"SemverLessThan", "SemverLessThanOrEqual",
}

type conditionModel struct {
	Attribute types.String `tfsdk:"attribute"`
	Operator  types.String `tfsdk:"operator"`
	Values    types.List   `tfsdk:"values"`
	Negate    types.Bool   `tfsdk:"negate"`
}

func conditionAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"attribute": types.StringType,
		"operator":  types.StringType,
		"values":    types.ListType{ElemType: types.StringType},
		"negate":    types.BoolType,
	}
}

func conditionsSchemaAttribute(required bool) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Required: required,
		Optional: !required,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"attribute": schema.StringAttribute{Required: true, Description: "User attribute to match, e.g. country or email."},
				"operator": schema.StringAttribute{
					Required:   true,
					Validators: []validator.String{stringvalidator.OneOf(operatorValues...)},
				},
				"values": schema.ListAttribute{Required: true, ElementType: types.StringType},
				"negate": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			},
		},
	}
}

func conditionsFromList(ctx context.Context, l types.List, diags *diag.Diagnostics) ([]client.Condition, bool) {
	if l.IsNull() || l.IsUnknown() {
		return []client.Condition{}, true
	}
	var models []conditionModel
	diags.Append(l.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, false
	}
	out := make([]client.Condition, 0, len(models))
	for _, m := range models {
		var values []string
		diags.Append(m.Values.ElementsAs(ctx, &values, false)...)
		if diags.HasError() {
			return nil, false
		}
		out = append(out, client.Condition{
			Attribute: m.Attribute.ValueString(),
			Operator:  m.Operator.ValueString(),
			Values:    values,
			Negate:    m.Negate.ValueBool(),
		})
	}
	return out, true
}

func conditionsToList(ctx context.Context, conds []client.Condition, diags *diag.Diagnostics) types.List {
	elemType := types.ObjectType{AttrTypes: conditionAttrTypes()}
	models := make([]conditionModel, 0, len(conds))
	for _, c := range conds {
		values, d := types.ListValueFrom(ctx, types.StringType, c.Values)
		diags.Append(d...)
		models = append(models, conditionModel{
			Attribute: types.StringValue(c.Attribute),
			Operator:  types.StringValue(c.Operator),
			Values:    values,
			Negate:    types.BoolValue(c.Negate),
		})
	}
	l, d := types.ListValueFrom(ctx, elemType, models)
	diags.Append(d...)
	return l
}
