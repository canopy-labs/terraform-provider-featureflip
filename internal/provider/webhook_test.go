package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/canopy-labs/terraform-provider-featureflip/internal/client"
)

func TestSetToSliceNeverNil(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	for name, s := range map[string]types.Set{
		"null":    types.SetNull(types.StringType),
		"unknown": types.SetUnknown(types.StringType),
	} {
		got := setToSlice(ctx, s, &diags)
		if got == nil || len(got) != 0 {
			t.Errorf("%s: setToSlice = %#v, want empty non-nil slice", name, got)
		}
	}
	s, _ := types.SetValueFrom(ctx, types.StringType, []string{"a", "b"})
	if got := setToSlice(ctx, s, &diags); len(got) != 2 {
		t.Errorf("setToSlice = %v", got)
	}
	if diags.HasError() {
		t.Fatal(diags)
	}
}

func TestSliceToSetPreservesNullIntent(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	null := types.SetNull(types.StringType)
	empty, _ := types.SetValueFrom(ctx, types.StringType, []string{})

	if got := sliceToSet(ctx, null, []string{}, &diags); !got.IsNull() {
		t.Errorf("omitted attribute vs upstream []: got %v, want null", got)
	}
	if got := sliceToSet(ctx, empty, []string{}, &diags); got.IsNull() || len(got.Elements()) != 0 {
		t.Errorf("explicit [] vs upstream []: got %v, want empty set", got)
	}
	// Drift: a value set outside Terraform must surface even when config omits the attribute.
	if got := sliceToSet(ctx, null, []string{"p1"}, &diags); got.IsNull() || len(got.Elements()) != 1 {
		t.Errorf("omitted attribute vs upstream [p1]: got %v, want [p1]", got)
	}
	if diags.HasError() {
		t.Fatal(diags)
	}
}

func TestKeepUntrimmed(t *testing.T) {
	cases := []struct {
		prior types.String
		got   string
		want  string
	}{
		{types.StringValue("  audit  "), "audit", "  audit  "}, // upstream only trimmed
		{types.StringValue("audit"), "renamed", "renamed"},     // real drift
		{types.StringNull(), "audit", "audit"},                 // import
		{types.StringUnknown(), "audit", "audit"},
	}
	for _, c := range cases {
		if got := keepUntrimmed(c.prior, c.got); got.ValueString() != c.want {
			t.Errorf("keepUntrimmed(%v, %q) = %q, want %q", c.prior, c.got, got.ValueString(), c.want)
		}
	}
}

func TestFindActiveSecret(t *testing.T) {
	retired := "2026-09-24T10:00:00Z"
	w := &client.Webhook{Secrets: []client.WebhookSecret{
		{ID: "s0", RetiredAt: &retired},
		{ID: "s1", CreatedAt: "c1"},
	}}
	if s, ok := findActiveSecret(w, "s1"); !ok || s.CreatedAt != "c1" {
		t.Errorf("active secret: %+v %v", s, ok)
	}
	if _, ok := findActiveSecret(w, "s0"); ok {
		t.Error("retired secret reported active")
	}
	if _, ok := findActiveSecret(w, "missing"); ok {
		t.Error("missing secret reported active")
	}
}
