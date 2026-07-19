package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

func TestConditionsRoundTrip(t *testing.T) {
	ctx := context.Background()
	in := []client.Condition{
		{Attribute: "country", Operator: "In", Values: []string{"DE", "FR"}, Negate: true},
		{Attribute: "version", Operator: "SemverGreaterThan", Values: []string{"2.0.0"}, Negate: false},
	}
	var diags diag.Diagnostics
	l := conditionsToList(ctx, in, &diags)
	if diags.HasError() {
		t.Fatalf("toList: %v", diags)
	}
	out, ok := conditionsFromList(ctx, l, &diags)
	if !ok || diags.HasError() {
		t.Fatalf("fromList: %v", diags)
	}
	if len(out) != 2 || out[0].Values[1] != "FR" || !out[0].Negate || out[1].Operator != "SemverGreaterThan" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestReorderConditionsLike(t *testing.T) {
	condA := client.Condition{Attribute: "email", Operator: "EndsWith", Values: []string{"@acme.io"}}
	condB := client.Condition{Attribute: "country", Operator: "In", Values: []string{"DE", "FR"}, Negate: true}
	condC := client.Condition{Attribute: "version", Operator: "SemverGreaterThan", Values: []string{"2.0.0"}}
	// Differs from condB ONLY by the order of Values. conditionsEqual is
	// intentionally strict-positional on Values: values-order instability
	// upstream is unconfirmed, so two conditions whose Values differ only in
	// order are treated as NOT equal for now.
	condBValuesSwapped := client.Condition{Attribute: "country", Operator: "In", Values: []string{"FR", "DE"}, Negate: true}

	tests := []struct {
		name   string
		prior  []client.Condition
		actual []client.Condition
		want   []client.Condition
	}{
		{
			name:   "exact reorder",
			prior:  []client.Condition{condA, condB},
			actual: []client.Condition{condB, condA},
			want:   []client.Condition{condA, condB},
		},
		{
			name:   "unmatched extras appended",
			prior:  []client.Condition{condA},
			actual: []client.Condition{condB, condA},
			want:   []client.Condition{condA, condB},
		},
		{
			name:   "duplicates preserved",
			prior:  []client.Condition{condA, condA, condB},
			actual: []client.Condition{condB, condA, condA},
			want:   []client.Condition{condA, condA, condB},
		},
		{
			name:   "empty prior leaves actual unchanged",
			prior:  nil,
			actual: []client.Condition{condB, condA, condC},
			want:   []client.Condition{condB, condA, condC},
		},
		{
			name:   "dropped prior entries ignored",
			prior:  []client.Condition{condC, condA},
			actual: []client.Condition{condA},
			want:   []client.Condition{condA},
		},
		{
			name:   "values order differing means not equal",
			prior:  []client.Condition{condB},
			actual: []client.Condition{condBValuesSwapped, condB},
			want:   []client.Condition{condB, condBValuesSwapped},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderConditionsLike(tt.prior, tt.actual)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d conditions, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if !conditionsEqual(got[i], tt.want[i]) {
					t.Errorf("position %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
			// Losslessness invariant: output length matches actual, and
			// output is a permutation of actual (nothing invented or lost).
			if len(got) != len(tt.actual) {
				t.Fatalf("losslessness: got %d conditions, actual had %d", len(got), len(tt.actual))
			}
			used := make([]bool, len(tt.actual))
			for _, g := range got {
				found := false
				for i, a := range tt.actual {
					if !used[i] && conditionsEqual(g, a) {
						used[i] = true
						found = true
						break
					}
				}
				if !found {
					t.Errorf("losslessness: output element %+v is not in actual (or appears too often)", g)
				}
			}
		})
	}
}

func TestReorderConditionGroupsLike(t *testing.T) {
	condA := client.Condition{Attribute: "email", Operator: "EndsWith", Values: []string{"@acme.io"}}
	condB := client.Condition{Attribute: "country", Operator: "In", Values: []string{"DE", "FR"}, Negate: true}
	condC := client.Condition{Attribute: "version", Operator: "SemverGreaterThan", Values: []string{"2.0.0"}}

	groupAnd := client.ConditionGroup{Operator: "And", Conditions: []client.Condition{condA, condB}}
	groupAndShuffled := client.ConditionGroup{Operator: "And", Conditions: []client.Condition{condB, condA}}
	groupOr := client.ConditionGroup{Operator: "Or", Conditions: []client.Condition{condC}}
	// Same conditions as groupAnd but a different operator: must NOT match.
	groupOrSameConds := client.ConditionGroup{Operator: "Or", Conditions: []client.Condition{condA, condB}}

	groupsEqual := func(a, b client.ConditionGroup) bool {
		if a.Operator != b.Operator || len(a.Conditions) != len(b.Conditions) {
			return false
		}
		for i := range a.Conditions {
			if !conditionsEqual(a.Conditions[i], b.Conditions[i]) {
				return false
			}
		}
		return true
	}

	tests := []struct {
		name   string
		prior  []client.ConditionGroup
		actual []client.ConditionGroup
		want   []client.ConditionGroup
	}{
		{
			name:   "group reorder",
			prior:  []client.ConditionGroup{groupAnd, groupOr},
			actual: []client.ConditionGroup{groupOr, groupAnd},
			want:   []client.ConditionGroup{groupAnd, groupOr},
		},
		{
			name:   "within-group condition reorder realigned",
			prior:  []client.ConditionGroup{groupAnd},
			actual: []client.ConditionGroup{groupAndShuffled},
			want:   []client.ConditionGroup{groupAnd},
		},
		{
			name:   "operator mismatch means no match",
			prior:  []client.ConditionGroup{groupAnd},
			actual: []client.ConditionGroup{groupOrSameConds},
			want:   []client.ConditionGroup{groupOrSameConds},
		},
		{
			name:   "unmatched extras appended in original order",
			prior:  []client.ConditionGroup{groupOr},
			actual: []client.ConditionGroup{groupAnd, groupOr, groupOrSameConds},
			want:   []client.ConditionGroup{groupOr, groupAnd, groupOrSameConds},
		},
		{
			name:   "empty prior leaves actual unchanged",
			prior:  nil,
			actual: []client.ConditionGroup{groupOr, groupAnd},
			want:   []client.ConditionGroup{groupOr, groupAnd},
		},
		{
			name:   "dropped prior groups ignored",
			prior:  []client.ConditionGroup{groupAnd, groupOr},
			actual: []client.ConditionGroup{groupOr},
			want:   []client.ConditionGroup{groupOr},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderConditionGroupsLike(tt.prior, tt.actual)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d groups, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if !groupsEqual(got[i], tt.want[i]) {
					t.Errorf("position %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
			// Losslessness invariant: output is a permutation of actual's
			// groups (conditions within a matched group may be realigned but
			// remain a permutation of that group's conditions).
			if len(got) != len(tt.actual) {
				t.Fatalf("losslessness: got %d groups, actual had %d", len(got), len(tt.actual))
			}
			used := make([]bool, len(tt.actual))
			for _, g := range got {
				found := false
				for i, a := range tt.actual {
					if used[i] || a.Operator != g.Operator || len(a.Conditions) != len(g.Conditions) {
						continue
					}
					realigned := reorderConditionsLike(g.Conditions, a.Conditions)
					match := true
					for j := range g.Conditions {
						if !conditionsEqual(g.Conditions[j], realigned[j]) {
							match = false
							break
						}
					}
					if match {
						used[i] = true
						found = true
						break
					}
				}
				if !found {
					t.Errorf("losslessness: output group %+v is not a permutation of any actual group", g)
				}
			}
		})
	}
}
