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
		"index/foo", "log/foo", // "index"/"log" are only reserved as the final segment
		// OKF prescribes no character set (design doc 0019): underscores
		// lead BigQuery hidden datasets, and foreign bundles may use any
		// language or spaces in filenames.
		"_intraday/events", "tables/_yesterday", "用語/売上", "notes/My Notes",
	}
	for _, id := range valid {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"", "/", "a/", "/a", "a//b", "../etc", "a/../b",
		".hidden", "a/.hidden", "index", "sales/index", "log", "sales/log",
		"a\x00b", "a\tb", "\xff", // control characters and invalid UTF-8 stay out
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
		"GA_sessions_2017", "notes/2026", "_intraday", "用語",
	}
	for _, p := range valid {
		if !ValidIDPrefix(p) {
			t.Errorf("ValidIDPrefix(%q) = false, want true", p)
		}
	}
	invalid := []string{
		"/", "a/", "/a", "a//b", "../etc", ".hidden", "a\x00b",
		strings.Repeat("a", 129), strings.Repeat("a/", 300) + "a",
	}
	for _, p := range invalid {
		if ValidIDPrefix(p) {
			t.Errorf("ValidIDPrefix(%q) = true, want false", p)
		}
	}
}

// Title is a display-name override (design doc 0022): absent, the id's
// last segment is the name.
func TestDisplayTitle(t *testing.T) {
	k := &Knowledge{ID: "insights/サンプル"}
	if got := k.DisplayTitle(); got != "サンプル" {
		t.Errorf("DisplayTitle() = %q, want the last id segment", got)
	}
	k.Title = "書き添えた題"
	if got := k.DisplayTitle(); got != "書き添えた題" {
		t.Errorf("DisplayTitle() = %q, want the title override", got)
	}
	if got := DisplayTitle("", "revenue"); got != "revenue" {
		t.Errorf("DisplayTitle root-level = %q, want the id itself", got)
	}
}

// Normalize pins NFC as the stored form of byte-compared keys (design
// doc 0022): macOS filesystems hand paths back NFD-decomposed.
func TestNormalize(t *testing.T) {
	nfd := "サンプル" // プ decomposed into フ + combining handakuten
	if got := Normalize(nfd); got != "サンプル" {
		t.Errorf("Normalize(%q) = %q, want the composed spelling", nfd, got)
	}
	if got := Normalize("sales/orders"); got != "sales/orders" {
		t.Errorf("Normalize left ASCII changed: %q", got)
	}
}

// TestSameContent pins the no-op-update predicate: authored content
// decides, server-managed provenance and timestamps never do, and attrs
// compare by value across decoders (YAML ints vs JSON float64s).
func TestSameContent(t *testing.T) {
	base := func() *Knowledge {
		return &Knowledge{
			Type: TypeMetrics, ID: "revenue", Title: "Revenue",
			Description: "monthly revenue", Tags: []string{"sales"},
			Status: StatusVerified, StatusNote: "checked",
			Links: []Link{{Target: "model/sales", Text: "sales model"}},
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
	// Types are the OKF vocabulary verbatim (design doc 0023), so spaces,
	// case, and non-ASCII are all ordinary — a type is spoken vocabulary,
	// not a path.
	for _, typ := range []Type{
		"BigQuery Table", "Golden Query", "runbook", "data-contract", "GA4",
		"日本語", "Data Contract", ".hidden",
	} {
		if !ValidType(typ) {
			t.Errorf("ValidType(%q) = false, want true", typ)
		}
	}
	// Rejected: empty, anything that reads as an address, control
	// characters, and over 128 bytes.
	for _, typ := range []Type{"", "   ", "a/b", "a\nb", "a\rb", "a\x00b", Type(strings.Repeat("x", 129))} {
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

// Filters match types case-insensitively so a caller need not reproduce
// the exact casing, while storage keeps the spelling the writer used
// (design doc 0023 §3.3).
func TestTypeMatchingIsCaseInsensitive(t *testing.T) {
	if !EqualType("bigquery table", TypeTables) || !EqualType("  BIGQUERY TABLE  ", TypeTables) {
		t.Error("EqualType must ignore case and surrounding space")
	}
	if EqualType("BigQuery Tables", TypeTables) {
		t.Error("EqualType must not match a different type")
	}
	if !BuiltinType("golden query") {
		t.Error(`BuiltinType("golden query") = false, want true`)
	}
	if got := CanonicalType("bigquery dataset"); got != TypeDatasets {
		t.Errorf("CanonicalType = %q, want %q", got, TypeDatasets)
	}
	if got := CanonicalType("Data Contract"); got != Type("Data Contract") {
		t.Errorf("CanonicalType must leave a free type alone, got %q", got)
	}
}

// FoldType is the same rule in the form the store needs: one comparison
// key per kind, so a filter can be matched against a folded column.
func TestFoldType(t *testing.T) {
	if got := FoldType("  BigQuery Table "); got != "bigquery table" {
		t.Errorf("FoldType = %q, want %q", got, "bigquery table")
	}
	// Free types fold too — the tolerance is not limited to the built-ins.
	if FoldType("DATA CONTRACT") != FoldType("Data Contract") {
		t.Error("FoldType must fold a free type")
	}
	if FoldType("BigQuery Tables") == FoldType(TypeTables) {
		t.Error("FoldType must keep distinct types distinct")
	}
	// The key is NFC, matching how the write path stores the type, so a
	// decomposed filter still lands on the stored row (design doc 0022).
	if FoldType("\u30bf\u3099") != FoldType("\u30c0") { // decomposed vs composed
		t.Error("FoldType must normalize to NFC")
	}
}

func TestToTypesAndToStatuses(t *testing.T) {
	types := ToTypes([]string{"Metric", "custom-type"})
	if len(types) != 2 || types[0] != TypeMetrics || types[1] != Type("custom-type") {
		t.Errorf("ToTypes = %v", types)
	}
	statuses := ToStatuses([]string{"verified", "draft"})
	if len(statuses) != 2 || statuses[0] != StatusVerified || statuses[1] != StatusDraft {
		t.Errorf("ToStatuses = %v", statuses)
	}
	if got := ToTypes(nil); len(got) != 0 {
		t.Errorf("ToTypes(nil) = %v, want empty", got)
	}
}

// The OKF lifecycle vocabulary is three values to ochakai's four, and the
// verified key — not the status — is what says a concept was confirmed
// (SPEC §5.3-5.4, design doc 0034 §3.4).
func TestOKFStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		in   Status
		want string
	}{
		{StatusDraft, "draft"},
		{StatusVerified, "stable"},
		{StatusDeprecated, "deprecated"},
		{StatusRejected, "deprecated"}, // OKF has no "never accepted"
	} {
		if got := OKFStatus(tc.in); got != tc.want {
			t.Errorf("OKFStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, tc := range []struct {
		raw         string
		hasVerified bool
		want        Status
		wantKnown   bool
	}{
		{"stable", true, StatusVerified, true},
		{"stable", false, StatusDraft, true},
		{"draft", false, StatusDraft, true},
		{"draft", true, StatusDraft, true}, // an explicit draft stays one
		{"deprecated", false, StatusDeprecated, true},
		{"", false, "", true}, // unset: the write path applies its own default
		{"", true, StatusVerified, true},
		{"verified", false, StatusVerified, true}, // written by 0.12 and earlier
		{"rejected", false, StatusRejected, true},
		{"retired", false, StatusDraft, false},
	} {
		got, known := StatusFromOKF(tc.raw, tc.hasVerified)
		if got != tc.want || known != tc.wantKnown {
			t.Errorf("StatusFromOKF(%q, %v) = (%q, %v), want (%q, %v)",
				tc.raw, tc.hasVerified, got, known, tc.want, tc.wantKnown)
		}
	}

	// Every status ochakai can store survives a round-trip through the OKF
	// vocabulary, except the one the mapping folds on purpose.
	for _, s := range Statuses {
		got, known := StatusFromOKF(OKFStatus(s), s == StatusVerified)
		if !known {
			t.Errorf("OKFStatus(%q) produced a value StatusFromOKF does not know", s)
		}
		if s == StatusRejected {
			if got != StatusDeprecated {
				t.Errorf("rejected should land as deprecated, got %q", got)
			}
			continue
		}
		if got != s {
			t.Errorf("round-trip of %q gave %q", s, got)
		}
	}
}

func TestValidStaleAfter(t *testing.T) {
	for _, ok := range []string{"", "2026-12-31", "2026-01-01"} {
		if !ValidStaleAfter(ok) {
			t.Errorf("ValidStaleAfter(%q) = false", ok)
		}
	}
	for _, bad := range []string{"soon", "2026-13-01", "2026-02-30", "2026-1-2",
		"2026-12-31T00:00:00Z", "30 days"} {
		if ValidStaleAfter(bad) {
			t.Errorf("ValidStaleAfter(%q) = true", bad)
		}
	}
}

// The recommended vocabulary is one list; every surface that names it
// reads from there, so adding a type cannot leave a stale copy behind.
func TestTypesHintCoversEveryBuiltin(t *testing.T) {
	hint := TypesHint()
	for _, ty := range Types {
		if !strings.Contains(hint, string(ty)) {
			t.Errorf("TypesHint() omits %q: %s", ty, hint)
		}
	}
	if !BuiltinType(TypeComputations) {
		t.Error("Attested Computation should be a recommended type (design doc 0034 §3.6)")
	}
}
