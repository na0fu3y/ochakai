package domain

import (
	"strings"
	"testing"
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
