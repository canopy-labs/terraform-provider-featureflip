package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestStringOr(t *testing.T) {
	if got := stringOr(types.StringValue("cfg"), "env"); got != "cfg" {
		t.Errorf("config value must win, got %q", got)
	}
	if got := stringOr(types.StringNull(), "env"); got != "env" {
		t.Errorf("null falls back to env, got %q", got)
	}
	if got := stringOr(types.StringNull(), ""); got != "" {
		t.Errorf("both empty → empty, got %q", got)
	}
}

func TestTokenFormatOK(t *testing.T) {
	for token, want := range map[string]bool{
		"ffs_abc": true, "ffp_abc": true, "sk_live_nope": false, "": false,
	} {
		if got := tokenFormatOK(token); got != want {
			t.Errorf("tokenFormatOK(%q) = %v, want %v", token, got, want)
		}
	}
}
