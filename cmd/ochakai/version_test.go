package main

import "testing"

// resolveVersion order: ldflags stamp > module build info > "dev".
func TestResolveVersion(t *testing.T) {
	defer func(v string) { version = v }(version)

	version = "v9.9.9"
	if got := resolveVersion(); got != "v9.9.9" {
		t.Errorf("stamped: got %q", got)
	}

	// In a test binary the main-module version is "" or "(devel)", so the
	// unstamped path must fall through to "dev" — and never be empty.
	version = ""
	if got := resolveVersion(); got != "dev" {
		t.Errorf("unstamped: got %q, want dev", got)
	}
}
