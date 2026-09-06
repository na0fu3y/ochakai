package mcpserver

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

// The declaration a client reads at initialize says what this server
// can do and nothing it cannot. Left to the SDK it said two untrue
// things — `logging`, never used and deprecated at 2026-07-28, and
// `listChanged` on lists a process cannot change (design doc 0118 §7)
// and a stateless transport could not notify about (0118 §2) — so each
// half is pinned: what is declared because it is offered, and what is
// absent because it is not.
func TestTheServerDeclaresOnlyWhatItDoes(t *testing.T) {
	caps := connect(t).InitializeResult().Capabilities
	if caps == nil {
		t.Fatal("initialize carried no capabilities")
	}

	// Offered.
	if caps.Tools == nil {
		t.Error("tools is not declared, and six are served")
	}
	if caps.Resources == nil {
		t.Error("resources is not declared, and a template is served")
	}

	// Not offered. A list fixed at build time changes on a release, which
	// a running process cannot announce; a subscription needs a session,
	// and there is none.
	if caps.Tools != nil && caps.Tools.ListChanged {
		t.Error("tools.listChanged promises a notification the tool list never sends")
	}
	if caps.Resources != nil && caps.Resources.ListChanged {
		t.Error("resources.listChanged promises a notification the resource list never sends")
	}
	if caps.Resources != nil && caps.Resources.Subscribe {
		t.Error("resources.subscribe is declared on a transport with no session to hold one")
	}
	// Read off the wire form rather than the struct's fields: `logging`'s
	// field is deprecated with the feature, and what matters is which
	// keys a client sees. Exactly the two offered, and no `logging` among
	// them (0118 §2: ochakai has never sent a log message).
	raw, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(keys))
	if want := []string{"resources", "tools"}; !slices.Equal(got, want) {
		t.Errorf("initialize declares %v, want exactly %v", got, want)
	}
}
