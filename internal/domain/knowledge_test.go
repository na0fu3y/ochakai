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
			Status: StatusStable, StatusNote: "checked",
			Sources: []Source{
				{Resource: "policies/revenue.md", ID: "rev", UsageCount: new(0)},
				{Resource: "https://example.test/ga4", LastModified: "2026-05-30"},
			},
			UsageWindow: &UsageWindow{From: "2026-06-01", To: "2026-06-30"},
			Runtime:     "bigquery",
			Parameters:  []Parameter{{Name: "year", Type: "integer", Required: true}},
			Executor:    &Executor{Resource: "skills/run.md", Receipt: []string{"job_id"}},
			Attester:    &Attester{Resource: "attesters/revenue.py"},
			Links:       []Link{{Target: "model/sales", Text: "sales model"}},
			Attrs:       map[string]any{"threshold": 5, "model": "sales"},
			Body:        "Sum of order totals.",
		}
	}

	same := base()
	// Server-managed fields must not count as content: an import payload
	// never carries them, the stored entry always does.
	now := time.Now()
	actor := Actor{Kind: ActorHuman, Name: "na0"}
	same.CreatedBy = actor
	same.CreatedAt, same.UpdatedAt = now, now
	same.Verifications = []Verification{{By: actor, At: now}}
	same.Files = []File{{Name: "chart.png"}}
	// The same number decoded from JSONB arrives as float64, from YAML as
	// int — both are the value 5.
	same.Attrs = map[string]any{"threshold": float64(5), "model": "sales"}
	if !base().SameContent(same) {
		t.Error("entries differing only in server-managed fields and attr number types must compare equal")
	}

	nilVsEmpty := base()
	nilVsEmpty.Tags, nilVsEmpty.Links, nilVsEmpty.Attrs = []string{}, []Link{}, map[string]any{}
	nilVsEmpty.Sources, nilVsEmpty.Parameters = []Source{}, []Parameter{}
	empty := base()
	empty.Tags, empty.Links, empty.Attrs = nil, nil, nil
	empty.Sources, empty.Parameters = nil, nil
	if !nilVsEmpty.SameContent(empty) {
		t.Error("nil and empty tags/links/attrs/sources/parameters must compare equal")
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
		// OKF v0.2 families are authored content too (design doc 0036): a
		// write that only edits the citations must not be swallowed.
		"source title":  func(k *Knowledge) { k.Sources[0].Title = "Revenue policy" },
		"source order":  func(k *Knowledge) { k.Sources[0], k.Sources[1] = k.Sources[1], k.Sources[0] },
		"source drop":   func(k *Knowledge) { k.Sources = k.Sources[:1] },
		"usage_window":  func(k *Knowledge) { k.UsageWindow.To = "2026-07-31" },
		"window unset":  func(k *Knowledge) { k.UsageWindow = nil },
		"runtime":       func(k *Knowledge) { k.Runtime = "postgres" },
		"computation":   func(k *Knowledge) { k.Computation = "queries/revenue.sql" },
		"param order":   func(k *Knowledge) { k.Parameters = append(k.Parameters, Parameter{Name: "q", Type: "string"}) },
		"param require": func(k *Knowledge) { k.Parameters[0].Required = false },
		"receipt":       func(k *Knowledge) { k.Executor.Receipt = []string{"job_id", "result"} },
		"executor drop": func(k *Knowledge) { k.Executor = nil },
		"attester":      func(k *Knowledge) { k.Attester.Resource = "attesters/other.py" },
	} {
		changed := base()
		mutate(changed)
		if base().SameContent(changed) {
			t.Errorf("a %s change must not compare equal", name)
		}
	}

	// A usage_count of zero is a count, not the absence of one: a source
	// exercised zero times over the window says something an uncounted
	// source does not (design doc 0036 §3.1).
	counted, uncounted := base(), base()
	uncounted.Sources[0].UsageCount = nil
	if counted.SameContent(uncounted) {
		t.Error("usage_count 0 and an absent usage_count must not compare equal")
	}
}

// The OKF v0.2 families carry their own shape rules (SPEC §5.1, §10.2).
// domain holds the predicate; service turns a false into a 400 and the
// bundle parser turns it into a note (design doc 0036 §3.4).
func TestOKFFamilyValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		ok    bool
		valid func() bool
	}{
		"source needs a resource":     {false, func() bool { return Source{ID: "rev"}.Valid() }},
		"a resource is enough":        {true, func() bool { return Source{Resource: "policies/rev.md"}.Valid() }},
		"source date must be a date":  {false, func() bool { return Source{Resource: "r", LastModified: "2026-06"}.Valid() }},
		"source date may be absent":   {true, func() bool { return Source{Resource: "r"}.Valid() }},
		"usage_count may not be < 0":  {false, func() bool { return Source{Resource: "r", UsageCount: new(-1)}.Valid() }},
		"usage_count may be 0":        {true, func() bool { return Source{Resource: "r", UsageCount: new(0)}.Valid() }},
		"per-source window is dated":  {false, func() bool { return Source{Resource: "r", UsageWindow: &UsageWindow{From: "nope"}}.Valid() }},
		"parameter needs a name":      {false, func() bool { return Parameter{Type: "integer"}.Valid() }},
		"parameter needs a type":      {false, func() bool { return Parameter{Name: "year"}.Valid() }},
		"parameter with both is fine": {true, func() bool { return Parameter{Name: "year", Type: "integer"}.Valid() }},
		"executor needs a resource":   {false, func() bool { return (&Executor{Receipt: []string{"job_id"}}).Valid() }},
		// SPEC §10.2 marks only runtime REQUIRED, and describes receipt
		// with no requirement word; §11 forbids rejecting a concept for a
		// missing optional field. ochakai demanded one until 0079, citing
		// a §10.2 rule §10.2 does not state.
		"executor without a receipt is fine": {true, func() bool { return (&Executor{Resource: "run.md"}).Valid() }},
		"absent executor is fine":            {true, func() bool { return (*Executor)(nil).Valid() }},
		"attester needs a resource":          {false, func() bool { return (&Attester{}).Valid() }},
		"absent attester is fine":            {true, func() bool { return (*Attester)(nil).Valid() }},
		"absent window is fine":              {true, func() bool { return (*UsageWindow)(nil).Valid() }},
	} {
		if got := tc.valid(); got != tc.ok {
			t.Errorf("%s: Valid() = %v, want %v", name, got, tc.ok)
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
		// A namespaced type is legal OKF: SPEC §4.1 registers no
		// taxonomy and requires a consumer to tolerate unknown types,
		// and §11 forbids rejecting a bundle over one (design doc 0064).
		"acme/Table", "a/b",
	} {
		if !ValidType(typ) {
			t.Errorf("ValidType(%q) = false, want true", typ)
		}
	}
	// Rejected: empty, more than one line, control characters, and over
	// 128 bytes.
	for _, typ := range []Type{"", "   ", "a\nb", "a\rb", "a\x00b", Type(strings.Repeat("x", 129))} {
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
	// Retired from the vocabulary but still perfectly valid to write
	// (design doc 0038): the narrowing must not have left either spelling
	// half-in, recommended by one surface and unknown to the next.
	for _, retired := range []Type{"Semantic Model", "Golden Query"} {
		if BuiltinType(retired) {
			t.Errorf("BuiltinType(%q) = true, want false: 0038 retired it", retired)
		}
		if !ValidType(retired) {
			t.Errorf("ValidType(%q) = false: a retired type stays writable as a free type", retired)
		}
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
	if !BuiltinType("attested computation") {
		t.Error(`BuiltinType("attested computation") = false, want true`)
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
	statuses := ToStatuses([]string{"stable", "draft"})
	if len(statuses) != 2 || statuses[0] != StatusStable || statuses[1] != StatusDraft {
		t.Errorf("ToStatuses = %v", statuses)
	}
	if got := ToTypes(nil); len(got) != 0 {
		t.Errorf("ToTypes(nil) = %v, want empty", got)
	}
}

// The status vocabulary is OKF's own (design doc 0043 §3.2), so the
// mapping is the identity on every value the spec defines and the whole
// of what remains is what to do with the two cases it leaves open.
func TestOKFStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		want      Status
		wantKnown bool
	}{
		{"draft", StatusDraft, true},
		{"stable", StatusStable, true},
		{"deprecated", StatusDeprecated, true},
		{"", "", true}, // unset: the write path applies its own default
		// The values ochakai wrote before 0.16 are not read back as
		// themselves. "verified" was never an OKF lifecycle value, and
		// "rejected" is a ruling rather than a stage — both arrive as
		// unrecognized, which SPEC §11 says to accept as a draft rather
		// than reject the document over.
		{"verified", StatusDraft, false},
		{"rejected", StatusDraft, false},
		{"retired", StatusDraft, false},
	} {
		got, known := StatusFromOKF(tc.raw)
		if got != tc.want || known != tc.wantKnown {
			t.Errorf("StatusFromOKF(%q) = (%q, %v), want (%q, %v)",
				tc.raw, got, known, tc.want, tc.wantKnown)
		}
	}

	// A stable entry nobody confirmed keeps its lifecycle value. Until
	// 0.16 it was demoted to a draft, because with confirmation held as a
	// status there was no way to say "ready for consumption, unverified"
	// (design doc 0043 §3.2).
	if got, known := StatusFromOKF("stable"); got != StatusStable || !known {
		t.Errorf("an unconfirmed stable entry read as (%q, %v)", got, known)
	}

	for _, s := range Statuses {
		if got, known := StatusFromOKF(string(s)); got != s || !known {
			t.Errorf("round-trip of %q gave (%q, %v)", s, got, known)
		}
	}
}

// Ruling is what the curation guards ask, and it is not a property of
// the status alone: one of the two is a ledger (design doc 0043 §3.2).
// Turning a concept down is not among them — that is a deletion carrying
// its reason, and it gates nothing (design doc 0135).
func TestRuling(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	v := []Verification{{By: Actor{Kind: ActorHuman, Name: "na0"}, At: at}}
	for _, tc := range []struct {
		what string
		k    Knowledge
		want Ruling
	}{
		{"a plain draft", Knowledge{Status: StatusDraft}, ""},
		{"a stable entry nobody checked", Knowledge{Status: StatusStable}, ""},
		{"a verified draft", Knowledge{Status: StatusDraft, Verifications: v}, RulingVerified},
		{"a deprecated entry", Knowledge{Status: StatusDeprecated}, RulingDeprecated},
	} {
		if got := tc.k.Ruling(); got != tc.want {
			t.Errorf("%s: Ruling() = %q, want %q", tc.what, got, tc.want)
		}
		if got := tc.k.Curated(); got != (tc.want != "") {
			t.Errorf("%s: Curated() = %v", tc.what, got)
		}
	}
}

// Both spellings SPEC allows, and nothing else. The instants are the
// half that used to be refused: SPEC §5 fixes every timestamp-valued key
// as "an ISO 8601 datetime with an explicit UTC offset", and the worked
// example of Appendix A writes stale_after that way (design doc 0133).
func TestValidStaleAfter(t *testing.T) {
	for _, ok := range []string{"", "2026-12-31", "2026-01-01",
		"2026-12-31T00:00:00Z", "2026-12-31T18:30:00Z", "2026-12-31T18:30:00+09:00"} {
		if !ValidStaleAfter(ok) {
			t.Errorf("ValidStaleAfter(%q) = false", ok)
		}
	}
	// An offset is what makes a datetime absolute, so a local one is
	// still refused — as are the loose spellings a date never took.
	for _, bad := range []string{"soon", "2026-13-01", "2026-02-30", "2026-1-2",
		"2026-12-31T18:30:00", "2026-12-31 18:30:00Z", "30 days"} {
		if ValidStaleAfter(bad) {
			t.Errorf("ValidStaleAfter(%q) = true", bad)
		}
	}
}

// A moment goes back out in the spelling it arrived in, midnight aside:
// a bare date is the UTC midnight opening it, so the two are one value
// and the tidier spelling is the one a reader gets back.
func TestFormatInstantRoundTrips(t *testing.T) {
	for _, s := range []string{"2026-12-31", "2026-12-31T18:30:00Z"} {
		got, ok := ParseInstant(s)
		if !ok {
			t.Fatalf("ParseInstant(%q) = false", s)
		}
		if back := FormatInstant(got); back != s {
			t.Errorf("FormatInstant(ParseInstant(%q)) = %q", s, back)
		}
	}
	at, ok := ParseInstant("2026-12-31T00:00:00Z")
	if !ok {
		t.Fatal("ParseInstant of a midnight instant = false")
	}
	if got := FormatInstant(at); got != "2026-12-31" {
		t.Errorf("a UTC midnight reads back as %q, want the date alone", got)
	}
	// An offset is resolved rather than kept: what is stored is the
	// instant, and 09:00+09:00 is the midnight that opens the same day.
	at, ok = ParseInstant("2026-12-31T09:00:00+09:00")
	if !ok {
		t.Fatal("ParseInstant of an offset instant = false")
	}
	if got := FormatInstant(at); got != "2026-12-31" {
		t.Errorf("an offset midnight reads back as %q, want the UTC date", got)
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
		t.Error("Attested Computation should be a recommended type (design doc 0036 §3.6)")
	}
}

// A type a write face names but cannot describe is a name an agent picks
// by guessing. The guide is a map, so a tenth type joins Types and lands
// on every surface with an empty sentence after its name unless this
// fails first.
func TestTypesGuideDescribesEveryBuiltin(t *testing.T) {
	g := TypesGuide()
	for _, ty := range Types {
		if guide[ty] == "" {
			t.Errorf("no guide for the recommended type %q", ty)
		}
		if !strings.Contains(g, "- "+string(ty)+": ") {
			t.Errorf("TypesGuide() omits %q:\n%s", ty, g)
		}
	}
	if len(guide) != len(Types) {
		t.Errorf("guide has %d entries for %d recommended types: a retired spelling is still described",
			len(guide), len(Types))
	}
	// The sentences are shown where somebody writes a concept, so they
	// have to say what goes in one. A bare restatement of the name is
	// what the filters already offer (TypesHint).
	for ty, s := range guide {
		if len(s) < len(string(ty)) {
			t.Errorf("the guide for %q says nothing the spelling did not: %q", ty, s)
		}
	}
}

// SPEC §7's third actor form, as ochakai admits it: software and version,
// told apart from the two identity forms by the slash it uses and the
// colon it must not (design doc 0052 §3.2).
func TestValidProducer(t *testing.T) {
	for _, s := range []string{
		"insightflow/1.4.0",
		"claude-code/2026.07",     // no version grammar is imposed
		"ochakai/v0.15.0-3-gabc1", // nor is semver
		"x/y",
	} {
		if !ValidProducer(s) {
			t.Errorf("ValidProducer(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"",
		"insightflow",               // no version
		"insightflow/",              // empty version
		"/1.4.0",                    // empty producer
		"a/b/c",                     // two slashes
		"insight flow/1.4.0",        // whitespace
		"insightflow/1.4.0\n",       // control character
		"process:insightflow/1.4.0", // would read as SPEC §7's identity form
		strings.Repeat("x", MaxProducer) + "/1",
	} {
		if ValidProducer(s) {
			t.Errorf("ValidProducer(%q) = true, want false", s)
		}
	}
}

// Provenance reads as one sentence on every surface, and each clause is
// dropped when there is nothing to say (design docs 0027, 0052 §3.5).
func TestActorString(t *testing.T) {
	for _, tc := range []struct {
		a    Actor
		want string
	}{
		{Actor{Kind: ActorHuman, Name: "tanaka@example.co.jp"}, "human:tanaka@example.co.jp"},
		{Actor{Kind: ActorProcess, Name: "sa@example.iam.gserviceaccount.com", Producer: "insightflow/1.4.0"},
			"process:sa@example.iam.gserviceaccount.com using insightflow/1.4.0"},
		{Actor{Kind: ActorHuman, Name: "tanaka@example.co.jp", Via: "process:app@example.iam.gserviceaccount.com",
			Producer: "insightflow/1.4.0"},
			"human:tanaka@example.co.jp via process:app@example.iam.gserviceaccount.com using insightflow/1.4.0"},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("Actor%+v.String() = %q, want %q", tc.a, got, tc.want)
		}
	}
}

// A tier is derived from the verifications that stand — the rows at or
// after the last meaningful change (design doc 0138). A confirmation of
// content an edit has since replaced keeps its ledger row and loses its
// claim; a later confirmation restores the tier it earns.
func TestTrustOfCountsOnlyStandingVerifications(t *testing.T) {
	edit := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	before, after := edit.Add(-time.Hour), edit.Add(time.Hour)
	human := Verification{By: Actor{Kind: ActorHuman, Name: "t"}, At: before}
	machine := Verification{By: Actor{Kind: ActorProcess, Name: "sa"}, At: after}
	for _, tc := range []struct {
		name string
		vs   []Verification
		want Trust
	}{
		{"no ledger", nil, TrustUnverified},
		{"human before the edit lapses", []Verification{human}, TrustUnverified},
		{"machine after the edit stands", []Verification{human, machine}, TrustMachine},
		{"human at the edit's own instant stands",
			[]Verification{{By: Actor{Kind: ActorHuman, Name: "t"}, At: edit}}, TrustHuman},
		{"human re-verification restores the tier",
			[]Verification{human, {By: Actor{Kind: ActorHuman, Name: "t"}, At: after}}, TrustHuman},
	} {
		if got := TrustOf(tc.vs, edit); got != tc.want {
			t.Errorf("%s: TrustOf = %s, want %s", tc.name, got, tc.want)
		}
	}
}
