package main

import (
	"os"
	"strings"
	"testing"
)

// Guard: the help text addresses entries by path (design doc 0017). The
// two-part "<type>/<id>" form predates it and no longer parses as two
// arguments — an id is one path.
func TestUsageAddressesEntriesByPath(t *testing.T) {
	var b strings.Builder
	usage(&b)
	if strings.Contains(b.String(), "<type>/<id>") {
		t.Errorf("usage() still documents the pre-0017 <type>/<id> address:\n%s", b.String())
	}
}

// Guard: instructions we ship to agents name types in the 0023 vocabulary.
// Free types mean a stale name is not rejected — an agent following the old
// wording silently grows a "query"-typed entry beside the Golden Queries,
// so these files are the only thing standing between the vocabulary and
// drift.
func TestShippedInstructionsUseCurrentTypeVocabulary(t *testing.T) {
	for _, f := range []string{
		"../../examples/claude-code/hooks/ochakai-write-back.sh",
		"../../docs/guides/golden-query-canary.md",
	} {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(content)
		for _, stale := range []string{"type: query", "type: insight", "type=query", "type=insight"} {
			if strings.Contains(s, stale) {
				t.Errorf("%s uses the pre-0023 type name %q", f, stale)
			}
		}
		if !strings.Contains(s, "Golden Query") {
			t.Errorf("%s never names the Golden Query type", f)
		}
	}
}
