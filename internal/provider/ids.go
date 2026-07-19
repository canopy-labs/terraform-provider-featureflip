package provider

import (
	"fmt"
	"strings"
)

// splitImportID splits an import ID like "project/flag" into exactly n
// non-empty parts, erroring with the expected format otherwise. Use this
// only when every segment is itself guaranteed slash-free (e.g. project and
// flag keys, which upstream regex-constrains to exclude "/").
func splitImportID(id string, n int, format string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != n {
		return nil, fmt.Errorf("unexpected import ID %q: expected %s", id, format)
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("unexpected import ID %q: empty segment, expected %s", id, format)
		}
	}
	return parts, nil
}

// splitImportIDTail splits an import ID into n parts, left-anchored: the
// first n-1 parts are slash-free identifiers, and the last part is greedy,
// absorbing any remaining "/" characters. Use this when the final segment is
// an environment or segment key, which upstream leaves format-unconstrained
// (unlike project/flag keys) and so may itself contain "/". Errors when
// there are fewer than n parts or any part is empty.
func splitImportIDTail(id string, n int, format string) ([]string, error) {
	parts := strings.SplitN(id, "/", n)
	if len(parts) != n {
		return nil, fmt.Errorf("unexpected import ID %q: expected %s", id, format)
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("unexpected import ID %q: empty segment, expected %s", id, format)
		}
	}
	return parts, nil
}

// splitSDKKeyImportID parses "<project>/<environment>/<sdk-key-id>" import
// IDs. project and sdk-key-id are slash-free (project keys are
// regex-constrained upstream; SDK key IDs are GUIDs), but the middle
// environment segment is format-unconstrained upstream and may itself
// contain "/". project is everything up to the first "/"; sdkKeyID is
// everything after the last "/"; environment is whatever remains between
// them. Errors when there are fewer than 2 slashes or any part is empty.
func splitSDKKeyImportID(id string, format string) (project, environment, sdkKeyID string, err error) {
	first := strings.Index(id, "/")
	last := strings.LastIndex(id, "/")
	if first == -1 || last == first {
		return "", "", "", fmt.Errorf("unexpected import ID %q: expected %s", id, format)
	}
	project = id[:first]
	environment = id[first+1 : last]
	sdkKeyID = id[last+1:]
	if project == "" || environment == "" || sdkKeyID == "" {
		return "", "", "", fmt.Errorf("unexpected import ID %q: empty segment, expected %s", id, format)
	}
	return project, environment, sdkKeyID, nil
}
