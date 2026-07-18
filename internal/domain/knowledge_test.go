package domain

import (
	"strings"
	"testing"
	"time"
)

func TestValidID(t *testing.T) {
	valid := []string{
		"revenue", "monthly-revenue", "sales/orders", "a/b/c",
		"GA_sessions_2017", "notes/2026/q3", "v1.2", "sales/index-page",
		"index/foo", // "index" is only reserved as the final segment
	}
	for _, id := range valid {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"", "/", "a/", "/a", "a//b", "日本語", "a b", "../etc", "a/../b",
		".hidden", "a/.hidden", "index", "sales/index",
		strings.Repeat("a", 129), "a/" + strings.Repeat("b/", 300),
	}
	for _, id := range invalid {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestValidIDPrefix(t *testing.T) {
	valid := []string{
		"", "sales", "sales/orders", "index", "a/index", // "index" only names a directory here
		"GA_sessions_2017", "notes/2026",
	}
	for _, p := range valid {
		if !ValidIDPrefix(p) {
			t.Errorf("ValidIDPrefix(%q) = false, want true", p)
		}
	}
	invalid := []string{
		"/", "a/", "/a", "a//b", "日本語", "a b", "../etc", ".hidden",
		strings.Repeat("a", 129), strings.Repeat("a/", 300) + "a",
	}
	for _, p := range invalid {
		if ValidIDPrefix(p) {
			t.Errorf("ValidIDPrefix(%q) = true, want false", p)
		}
	}
}

// TestSameContent pins the no-op-update predicate: authored content
// decides, server-managed provenance and timestamps never do, and attrs
// compare by value across decoders (YAML ints vs JSON float64s).
func TestSameContent(t *testing.T) {
	base := func() *Knowledge {
		return &Knowledge{
			Type: TypeMetric, ID: "revenue", Title: "Revenue",
			Description: "monthly revenue", Tags: []string{"sales"},
			Status: StatusVerified, StatusNote: "checked",
			Links: []Link{{Rel: "defined_in", Target: "model/sales"}},
			Attrs: map[string]any{"threshold": 5, "model": "sales"},
			Body:  "Sum of order totals.",
		}
	}

	same := base()
	// Server-managed fields must not count as content: an import payload
	// never carries them, the stored entry always does.
	now := time.Now()
	actor := Actor{Kind: ActorHuman, Name: "na0"}
	same.CreatedBy = actor
	same.CreatedAt, same.UpdatedAt = now, now
	same.VerifiedBy, same.VerifiedAt = &actor, &now
	same.Attachments = []Attachment{{Name: "chart.png"}}
	// The same number decoded from JSONB arrives as float64, from YAML as
	// int — both are the value 5.
	same.Attrs = map[string]any{"threshold": float64(5), "model": "sales"}
	if !base().SameContent(same) {
		t.Error("entries differing only in server-managed fields and attr number types must compare equal")
	}

	nilVsEmpty := base()
	nilVsEmpty.Tags, nilVsEmpty.Links, nilVsEmpty.Attrs = []string{}, []Link{}, map[string]any{}
	empty := base()
	empty.Tags, empty.Links, empty.Attrs = nil, nil, nil
	if !nilVsEmpty.SameContent(empty) {
		t.Error("nil and empty tags/links/attrs must compare equal")
	}

	for name, mutate := range map[string]func(*Knowledge){
		"title":       func(k *Knowledge) { k.Title = "Net Revenue" },
		"description": func(k *Knowledge) { k.Description = "net" },
		"tags":        func(k *Knowledge) { k.Tags = []string{"sales", "finance"} },
		"status":      func(k *Knowledge) { k.Status = StatusDraft },
		"status_note": func(k *Knowledge) { k.StatusNote = "" },
		"links":       func(k *Knowledge) { k.Links[0].Target = "model/billing" },
		"attrs":       func(k *Knowledge) { k.Attrs["threshold"] = 6 },
		"body":        func(k *Knowledge) { k.Body = "Sum of order totals, net of refunds." },
	} {
		changed := base()
		mutate(changed)
		if base().SameContent(changed) {
			t.Errorf("a %s change must not compare equal", name)
		}
	}
}

func TestValidType(t *testing.T) {
	for _, typ := range []Type{"metric", "runbook", "data-contract", "GA4"} {
		if !ValidType(typ) {
			t.Errorf("ValidType(%q) = false, want true", typ)
		}
	}
	for _, typ := range []Type{"", "a/b", "日本語", ".hidden", "a b"} {
		if ValidType(typ) {
			t.Errorf("ValidType(%q) = true, want false", typ)
		}
	}
	for _, typ := range Types {
		if !BuiltinType(typ) || !ValidType(typ) {
			t.Errorf("recommended type %q must be builtin and valid", typ)
		}
	}
	if BuiltinType("runbook") {
		t.Error(`BuiltinType("runbook") = true, want false`)
	}
}
