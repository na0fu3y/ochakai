package store

import "testing"

// The version is the only thing that answers on any connection, so it is
// the thing that has to be read correctly. Unreadable counts as old:
// being wrong that way costs today's behaviour, and being wrong the other
// way fails every vector search on a setting the server does not have.
func TestPgvectorVersionsThatCarryIterativeScan(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"0.8.0", true},  // the release that added it
		{"0.8.5", true},  // what the test image ships
		{"0.8.1", true},  // what Cloud SQL ships
		{"0.9", true},    // two components is a version too
		{"1.0.0", true},  // a major past 0 carries it whatever the minor
		{"0.7.4", false}, // the last release without it
		{"0.5.1", false},
		{"", false},
		{"unknown", false},
	} {
		if got := pgvectorHasIterativeScan(tc.version); got != tc.want {
			t.Errorf("pgvectorHasIterativeScan(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}
