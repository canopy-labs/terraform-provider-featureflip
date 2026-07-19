package provider

import "testing"

func TestSplitImportID(t *testing.T) {
	parts, err := splitImportID("web/prod", 2, "<project>/<key>")
	if err != nil || parts[0] != "web" || parts[1] != "prod" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}
	if _, err := splitImportID("web", 2, "<project>/<key>"); err == nil {
		t.Error("expected error for wrong part count")
	}
	if _, err := splitImportID("web//x", 3, "<a>/<b>/<c>"); err == nil {
		t.Error("expected error for empty part")
	}
}

func TestSplitImportIDTail(t *testing.T) {
	// Tail segment (environment/segment key) may itself contain slashes.
	parts, err := splitImportIDTail("web/eu/west", 2, "<project>/<key>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts[0] != "web" || parts[1] != "eu/west" {
		t.Fatalf("parts=%v", parts)
	}

	// 3-part form (flag_environment): project/flag slash-free, environment greedy.
	parts, err = splitImportIDTail("web/my-flag/env/with/slashes", 3, "<project>/<flag>/<environment>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts[0] != "web" || parts[1] != "my-flag" || parts[2] != "env/with/slashes" {
		t.Fatalf("parts=%v", parts)
	}

	// No slash at all in the tail is still fine.
	parts, err = splitImportIDTail("web/prod", 2, "<project>/<key>")
	if err != nil || parts[0] != "web" || parts[1] != "prod" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}

	// Greedy-tail semantics, documented intent: once past the first n-1
	// slash-free identifiers, the tail is taken verbatim — keys are any
	// non-empty string upstream, so leading or trailing slashes *within* the
	// tail are accepted as-is rather than rejected.
	parts, err = splitImportIDTail("web/eu/west/", 2, "<project>/<key>")
	if err != nil || parts[0] != "web" || parts[1] != "eu/west/" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}
	parts, err = splitImportIDTail("web//x", 2, "<project>/<key>")
	if err != nil || parts[0] != "web" || parts[1] != "/x" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}

	if _, err := splitImportIDTail("web", 2, "<project>/<key>"); err == nil {
		t.Error("expected error for too few parts")
	}
	if _, err := splitImportIDTail("/west", 2, "<project>/<key>"); err == nil {
		t.Error("expected error for empty leading segment")
	}
	if _, err := splitImportIDTail("web/", 2, "<project>/<key>"); err == nil {
		t.Error("expected error for empty tail segment")
	}
}

func TestSplitSDKKeyImportID(t *testing.T) {
	project, env, id, err := splitSDKKeyImportID(
		"web/env/with/slash/0197b69f-aaaa-bbbb-cccc-121212121212",
		"<project>/<environment>/<sdk-key-id>",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if project != "web" {
		t.Errorf("project = %q, want web", project)
	}
	if env != "env/with/slash" {
		t.Errorf("environment = %q, want env/with/slash", env)
	}
	if id != "0197b69f-aaaa-bbbb-cccc-121212121212" {
		t.Errorf("sdkKeyID = %q", id)
	}

	// Simple case, no slashes in environment.
	project, env, id, err = splitSDKKeyImportID("web/prod/abc-123", "<project>/<environment>/<sdk-key-id>")
	if err != nil || project != "web" || env != "prod" || id != "abc-123" {
		t.Fatalf("project=%q env=%q id=%q err=%v", project, env, id, err)
	}

	// Fewer than 2 slashes.
	if _, _, _, err := splitSDKKeyImportID("web-prod-abc", "<project>/<environment>/<sdk-key-id>"); err == nil {
		t.Error("expected error for no slashes")
	}
	if _, _, _, err := splitSDKKeyImportID("web/prod-abc", "<project>/<environment>/<sdk-key-id>"); err == nil {
		t.Error("expected error for only one slash")
	}

	// Empty segments.
	if _, _, _, err := splitSDKKeyImportID("/env/id", "<project>/<environment>/<sdk-key-id>"); err == nil {
		t.Error("expected error for empty project")
	}
	if _, _, _, err := splitSDKKeyImportID("web//id", "<project>/<environment>/<sdk-key-id>"); err == nil {
		t.Error("expected error for empty environment")
	}
	if _, _, _, err := splitSDKKeyImportID("web/env/", "<project>/<environment>/<sdk-key-id>"); err == nil {
		t.Error("expected error for empty sdk-key-id")
	}
}
