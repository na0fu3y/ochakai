// Package domain defines the knowledge model shared by the store, MCP
// server, and REST API. See design docs 0074 and 0075.
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

// Type is the kind of a knowledge entry. Any single-line string is a valid
// type (design doc 0005): the built-in types below are recommendations,
// never a closed set — users' own document types are first-class. The
// values are the OKF type vocabulary verbatim, so import and export are
// identity on the type key (design doc 0023); the list is a vocabulary,
// not a registry of server behavior (design doc 0028 §3).
type Type string

const (
	TypeMetrics      Type = "Metric"               // semantic metric definition, synonyms
	TypeComputations Type = "Attested Computation" // sanctioned computation and the means to check a run of it (OKF SPEC §10, design doc 0036 §3.6)
	TypeSkills       Type = "Skill"                // the procedure an executor.resource points at (OKF SPEC §10.2)
	TypeInsights     Type = "Insight"              // how to read a metric: baselines, caveats
	TypePolicies     Type = "Policy"               // the rule that decides a number; what a sources[].resource cites
	TypeTerms        Type = "Glossary Term"        // glossary term
	TypeDatasets     Type = "BigQuery Dataset"     // dataset: a container grouping tables
	TypeTables       Type = "BigQuery Table"       // table catalog entry
	TypeReferences   Type = "Reference"            // mirror of external material (enums, licenses, schema docs)
)

// Types lists the recommended (built-in) knowledge types, in display order:
// definitions, then the things you run, then interpretation and the rules
// behind it, then catalog assets (containers before their contents), then
// mirrors of outside material.
//
// No server behavior hangs off any of them — the list is a vocabulary, not
// a registry (design doc 0028 §3). Attested Computation is the clearest
// case: ochakai records the contract (runtime, parameters, executor,
// attester) as envelope fields and never runs it. Holding a field is not
// acting on it — executing the computation and checking the receipt belong
// to the consumer (OKF SPEC §10.5), which is the same line design doc 0028
// drew when compile_sql went away.
//
// What earns a place is SPEC §4.1's one requirement of a producer — that
// the spelling be descriptive and self-explanatory — read against what OKF
// itself spells out (design doc 0038). "Semantic Model" and "Golden Query"
// left on the first count: the one carried its meaning in a foreign spec
// rather than in its spelling, and the other is what an Attested
// Computation with a runtime already is. Skill and Policy joined on the
// second — they are what an entry's own executor.resource and
// sources[].resource point at, so leaving them out left both ends of an
// edge ochakai already stores without a name.
//
// "Playbook" and "API Endpoint" also joined with 0038, on SPEC's example
// list alone, and left with design doc 0063: a recommended type is taught
// on every MCP call (design doc 0015 §3.1), and neither spelling was ever
// written by ochakai's own examples or by OKF's own reference bundles —
// SPEC naming a type in prose is not the same evidence as somebody having
// used it.
//
// The retired spellings stay first-class as free types; only the
// recommendation is gone.
var Types = []Type{TypeMetrics, TypeComputations, TypeSkills, TypeInsights,
	TypePolicies, TypeTerms, TypeDatasets, TypeTables, TypeReferences}

// TypesHint renders the recommended vocabulary for help and error text,
// so no surface keeps its own copy of the list to fall behind.
func TypesHint() string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// guide says what a writer puts in each recommended type, in the words
// docs/architecture.md's table uses (design doc 0071 §2). The comments on
// the constants above are notes for whoever reads this file — spec
// references and why a spelling is in the vocabulary; these are the
// sentences somebody choosing a type is shown.
//
// The filters get the bare spellings (TypesHint): a caller narrowing a
// search already knows what they are after. It is the write faces that
// were handing over nine names and no way to tell them apart, and that
// had grown their own scattered advice for four of the nine.
var guide = map[Type]string{
	TypeMetrics: "what a number means and what it is called — its definition and synonyms, " +
		"one concept per metric (id metrics/<name>), not the SQL",
	TypeComputations: "a computation others run instead of improvising — the code in a " +
		"# Computation fence, the contract in runtime (required), parameters, executor and " +
		"attester; a confirmed question-and-SQL pair is one of these, with a top-level " +
		"question key for the question it answers",
	TypeSkills: "the procedure an executor.resource points at — how to actually run a " +
		"computation in a given runtime",
	TypeInsights: "how to read a metric — baselines, seasonality, the artifacts that fake a " +
		"move, when a number is worth escalating",
	TypePolicies: "the rule that decides a number, such as revenue recognition or cost " +
		"allocation — what a sources[].resource cites",
	TypeTerms:    "one term of the shared vocabulary, defined once",
	TypeDatasets: "a dataset's catalog entry — the container its tables sit in; resource is its canonical URI",
	TypeTables: "a table's catalog entry — where the data comes from, column notes, known " +
		"problems; resource is its canonical URI, and the conventional sections are " +
		"# Schema and # Common query patterns",
	TypeReferences: "a copy of material that lives outside — enum definitions, licenses, " +
		"schema docs; resource is the original",
}

// TypesGuide renders the recommended vocabulary with what each type
// holds, one per line, for the faces where somebody is choosing a type to
// write rather than one to filter on. It has the same single home as
// TypesHint (design doc 0071 §6), for the same reason: the sentences were
// going to be copied into the MCP write tool, the CLI's put help and the
// manual otherwise, and a copy is what falls behind.
func TypesGuide() string {
	lines := make([]string, len(Types))
	for i, t := range Types {
		lines[i] = wrapItem("- "+string(t)+": "+guide[t], guideWidth)
	}
	return strings.Join(lines, "\n")
}

// guideWidth is the column the guide wraps at: the CLI's help is
// hand-wrapped narrower than this, and a terminal is where the widest
// line shows first.
const guideWidth = 76

// wrapItem folds one bullet at width, indenting what carries over so the
// item reads as one item. It splits on spaces alone — the sentences are
// English, and a word longer than the width keeps its line rather than
// being cut mid-token. Width counts runes: the sentences carry em dashes,
// which are three bytes and one column.
func wrapItem(s string, width int) string {
	var b strings.Builder
	col := 0
	for i, word := range strings.Fields(s) {
		n := utf8.RuneCountInString(word)
		switch {
		case i == 0:
			b.WriteString(word)
			col = n
		case col+1+n > width:
			b.WriteString("\n  " + word)
			col = 2 + n
		default:
			b.WriteString(" " + word)
			col += 1 + n
		}
	}
	return b.String()
}

// TypesQuoted renders the recommended vocabulary as shell words, wrapping
// the types that contain a space in q so the shell sees one word per type.
// Completion scripts interpolate this rather than spelling the list out,
// so the vocabulary has one home (design doc 0038 §4.4); q differs per
// shell because each already uses the other quote around the whole list.
func TypesQuoted(q string) string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
		if strings.Contains(names[i], " ") {
			names[i] = q + names[i] + q
		}
	}
	return strings.Join(names, " ")
}

// BuiltinType reports whether t is one of the recommended types, matched
// case-insensitively (EqualType), the same way search filters match
// (FoldType).
func BuiltinType(t Type) bool {
	for _, v := range Types {
		if EqualType(t, v) {
			return true
		}
	}
	return false
}

// ValidType reports whether t can be a knowledge type: one non-empty
// line, within 128 bytes. Types are the OKF vocabulary verbatim (design
// doc 0071) — "BigQuery Table", not a slug — and OKF registers no
// taxonomy, so the spelling is the meaning.
//
// "/" used to be banned here so a type never read as an address. That was
// ochakai's own rule with no SPEC behind it, and SPEC §4.1 ("Type values
// are not registered centrally… consumers MUST tolerate unknown types
// gracefully") and §11 ("Unknown type values" is not grounds to reject a
// bundle) are against it: `type: acme/Table` is legal OKF, and ochakai
// answered 400 to it and demoted it to a loose bundle file on import.
// A type is never a path — it is spoken in filters, frontmatter and tool
// arguments — so nothing needed the ban (design doc 0064).
//
// The length cap stays. It is ordinary defensive practice rather than
// anything SPEC derives, and no realistic bundle trips it.
func ValidType(t Type) bool {
	s := strings.TrimSpace(string(t))
	if s == "" || len(s) > 128 {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || (r < 0x20) || r == 0x7f
	}) < 0
}

// EqualType reports whether two types name the same kind. Types are
// matched case-insensitively (design doc 0023 §3.3) so a filter written
// "bigquery table" finds entries stored as "BigQuery Table"; the stored
// spelling is always the one the writer used.
func EqualType(a, b Type) bool {
	return FoldType(a) == FoldType(b)
}

// FoldType returns the comparison key for t: NFC-normalized, trimmed, and
// lowercased. It is the matching half of design doc 0023 §3.3 ("照合は
// 大小文字非依存、保存は原表記") in the form storage needs — a value to
// compare against a folded column, rather than the pairwise EqualType.
// Free types fold too: §3.3 widens the tolerance to every entry point and
// every comparison, not just the recommended eight.
//
// The store folds the column with SQL lower(), which agrees with this for
// ASCII and for the ordinary cased letters types are written in. The two
// can disagree on exotic Unicode case pairs (final sigma, dotted I under a
// Turkish collation); a type that hinges on those is beyond what either
// side promises.
func FoldType(t Type) string {
	return strings.ToLower(strings.TrimSpace(Normalize(string(t))))
}

// Status is an entry's lifecycle value, spelled as OKF SPEC §5.4 spells
// it: draft is "not yet reviewed, possibly incomplete", stable is "ready
// for consumption", deprecated is "kept for links and history, no longer
// current".
//
// It says nothing about whether anyone confirmed the entry. Under SPEC
// §5.3 that is what the verified key says, and ochakai keeps it in the
// verification ledger — the lifecycle value and the trust signal are
// independent, and a stable entry may be unverified just as a draft may
// be verified (design doc 0043 §3.2). "Was never accepted" is likewise
// not a lifecycle value but this instance's ruling, in the rejection
// ledger (§3.3).
type Status string

const (
	StatusDraft      Status = "draft"
	StatusStable     Status = "stable"
	StatusDeprecated Status = "deprecated"
)

// Statuses lists all statuses in lifecycle order — the single source for
// every user-facing enumeration (CLI help, completions, docs guards).
var Statuses = []Status{StatusDraft, StatusStable, StatusDeprecated}

func ValidStatus(s Status) bool {
	return slices.Contains(Statuses, s)
}

// StatusesHint renders the lifecycle vocabulary for help and error text,
// so no surface keeps a copy of the list to fall behind. The write path's
// invalid-status error did, and named `verified` and `rejected` for years
// after they stopped being statuses (design doc 0043 §§3.2-3.3) — telling
// a caller to retry with a value that fails the same way.
func StatusesHint() string {
	names := make([]string, len(Statuses))
	for i, s := range Statuses {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// Trust is the tier a consumer derives from an entry's verification
// ledger, in OKF's own vocabulary (SPEC §5.3). It is not a lifecycle
// value and not a field a writer can set: status says whether the entry
// may be consumed, trust says who has confirmed it, and the two are
// independent — a draft can be human-reviewed and a stable entry
// unverified (design docs 0043 §3.2, 0046 §3.10).
type Trust string

const (
	TrustUnverified = Trust("unverified")        // nobody has confirmed it
	TrustMachine    = Trust("machine-confirmed") // confirmed, but by no person
	TrustHuman      = Trust("human-reviewed")    // a person vouched for it
)

// Trusts lists the tiers lowest to highest, which is the order SPEC §5.3
// introduces them in and the order a filter's help text reads best in.
var Trusts = []Trust{TrustUnverified, TrustMachine, TrustHuman}

// ValidTrust reports whether t names a tier.
func ValidTrust(t Trust) bool { return slices.Contains(Trusts, t) }

// TrustsHint renders the vocabulary for help and error text, so no
// surface keeps a copy of the list to fall behind.
func TrustsHint() string {
	names := make([]string, len(Trusts))
	for i, t := range Trusts {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// TrustOf derives the tier from a verification ledger. SPEC §5.3 reads it
// from the human: prefix alone: any person makes it human-reviewed, any
// other confirmation machine-confirmed, none unverified.
func TrustOf(vs []Verification) Trust {
	tier := TrustUnverified
	for i := range vs {
		if vs[i].By.Kind == ActorHuman {
			return TrustHuman
		}
		tier = TrustMachine
	}
	return tier
}

// Lifecycle is the entry's status as a reader sees it: what it says, or
// OKF's default when it says nothing (SPEC §5.4). ochakai does not write
// a status its writer left out (design doc 0046 §3.9) — absence is what
// the document says, and applying the default is the projection's job.
func (k *Knowledge) Lifecycle() Status { return LifecycleOf(k.Status) }

// LifecycleOf is the same projection over a bare status value, for the
// readers that count statuses without materializing the entries behind
// them (the instance stats, design doc 0051 §3.5). The default lives
// here once: a second copy is how a tally starts disagreeing with the
// entries it is a tally of.
func LifecycleOf(s Status) Status {
	if s == "" {
		return StatusStable
	}
	return s
}

// StatusFromOKF maps a frontmatter status onto ochakai's vocabulary,
// which since design doc 0043 §3.2 is OKF's vocabulary — so the mapping
// is the identity on every value the spec defines, and the whole of what
// remains here is what to do with the two cases the spec leaves open.
//
// An unrecognized value is a draft, never a rejection of the document
// (SPEC §11 forbids rejecting a concept over an unknown field value). ok
// reports whether the value was recognized, so an importer can mention
// what it did rather than silently reinterpreting.
//
// An absent status stays unset ("") rather than becoming a value. OKF
// reads absence as stable (§5.4), and that default is applied where the
// value is read (LifecycleOf, and the write path's k.Lifecycle() — design
// doc 0046 §3.9) rather than stamped in here: the parser reports what the
// document says, and this one says nothing.
//
// Whether the document carried a verified key is deliberately not an
// input. Until 0043 it was: with no way to say "stable but unconfirmed",
// a foreign bundle's stable entry had to be demoted to draft or it would
// have claimed a confirmation nobody made. The verification ledger can
// hold that state, so the lifecycle value now arrives unaltered and the
// trust signal is recorded beside it (§3.2).
func StatusFromOKF(raw string) (s Status, ok bool) {
	if raw == "" {
		return "", true
	}
	if st := Status(raw); ValidStatus(st) {
		return st, true
	}
	return StatusDraft, false
}

// DateLayout is the date form OKF fixes for its absolute dates — stale_after
// (SPEC §5.5), sources[].last_modified and usage_window (SPEC §5.1). An
// absolute day, not a relative TTL, so staleness stays a plain date
// comparison with no reference to when the entry was read — and with no
// timezone to argue about.
const DateLayout = "2006-01-02"

// StaleAfterLayout is the older name for DateLayout, kept because the date
// rule was written for stale_after first.
const StaleAfterLayout = DateLayout

// ValidDate reports whether s can be one of OKF's absolute dates: unset,
// or a YYYY-MM-DD date that exists. Every date field in the envelope is
// held to this one rule.
func ValidDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(DateLayout, s)
	return err == nil
}

// ValidStaleAfter reports whether s can be a stale_after.
func ValidStaleAfter(s string) bool { return ValidDate(s) }

// Usage event kinds recorded per knowledge entry (design doc 0069).
// The first two are recorded passively by reads; worked/failed are
// deliberate outcome reports (report_outcome) closing the write-back loop.
//
// Retired: "compiled", written by compile_sql until 0.13.0 (design doc
// 0028). Existing rows are kept — history stays history — but nothing
// writes or aggregates them any more.
const (
	EventSearchHit = "search_hit" // appeared in search results
	EventFetched   = "fetched"    // fetched individually via get
	EventWorked    = "worked"     // caller reports the entry led to a correct result
	EventFailed    = "failed"     // caller reports the entry led to a wrong or unusable result
)

// ListSorts are the listing modes: the values of ?sort that replace
// searching with a feed. verified_at and failed rank by what the server
// observed and are emptied by verifying (design doc 0025); usage ranks by
// demand; stale_after ranks by the expiry a writer declared and is
// emptied by editing, not verifying (design doc 0037 §2.2). The single
// source for every surface's validation and help text.
var ListSorts = []string{"verified_at", "usage", "failed", "stale_after"}

func ValidListSort(s string) bool { return slices.Contains(ListSorts, s) }

// Outcomes lists the reportable outcome kinds — the single source for
// every user-facing enumeration (tool schema, CLI help, completions).
var Outcomes = []string{EventWorked, EventFailed}

func ValidOutcome(o string) bool {
	for _, v := range Outcomes {
		if o == v {
			return true
		}
	}
	return false
}

// Rulings are the values POST /api/v1/review/{id} accepts in its body
// (design doc 0055 §3.2): append a verification, stand up a rejection,
// take a live rejection back. The single source for the surfaces'
// validation and their refusal messages.
//
// Distinct from the Ruling type below, which is what an entry's ledger
// *says* after the fact rather than what a caller asks for: "deprecated"
// is a lifecycle value nobody posts here, and "withdrawn" leaves no
// ruling behind at all.
var Rulings = []string{"verified", "rejected", "withdrawn"}

// Changes are the values a revision's Change carries — what the product
// calls each kind of write in an entry's history. They are the verb form
// of what happened; a ruling posted as "withdrawn" is recorded here as
// "withdraw", the way "verified" is recorded as "verify".
var Changes = []string{
	"create", "update", "move", "delete",
	"verify", "reject", "withdraw",
	"add_file", "remove_file",
}

// QueueCounts is how much work each review queue is holding — the three
// listing feeds that a curator is meant to empty, counted rather than
// listed (design doc 0049).
//
// A queue is keyed by the sort that lists it, so the count and the way to
// see what it counts share one word (design doc 0059). Two of the three
// once had names of their own — reported_wrong and past_expiry — and a
// curator who wanted to work the queue `stats` had just named had to know
// that it was spelled `failed` on the way to seeing it:
//
//   - failed — sort=failed: entries whose failure reports are still
//     unanswered. `failed` is already the outcome a caller reports
//     (Outcomes), so the queue, the feed and the report are one word for
//     one thing.
//   - stale_after — sort=stale_after: entries past the expiry their own
//     author declared. `stale_after` is OKF's own frontmatter key, which
//     is where the date being counted came from.
//   - drafts — sort=usage with status=draft: what agents proposed,
//     waiting to be published or turned down. This one keeps a name of
//     its own, because it is not a sort: no single listing mode names it
//     (design doc 0059 §2.2). Verifying does not empty it either —
//     confirming and publishing are different acts (design doc 0043
//     §3.2), so a draft leaves by an edit or a rejection.
//
// The verification-age feed (sort=verified_at) is deliberately absent:
// it ranks every verified entry rather than holding the ones that need
// something, so a count of it is the size of the knowledge base and
// never reaches zero. A number nobody can drive down is not a queue.
type QueueCounts struct {
	Drafts     int64 `json:"drafts"`
	Failed     int64 `json:"failed"`
	StaleAfter int64 `json:"stale_after"`
}

// Total is how much work is waiting across all three queues. An entry
// can sit in more than one, so this counts places in queues rather than
// distinct entries — which is what a reviewer works through anyway.
func (q QueueCounts) Total() int64 { return q.Drafts + q.Failed + q.StaleAfter }

// Usage aggregates how often a knowledge entry was actually used, and
// how often users reported it worked or failed.
type Usage struct {
	SearchHits int64      `json:"search_hits"`
	Fetches    int64      `json:"fetches"`
	Worked     int64      `json:"worked"`
	Failed     int64      `json:"failed"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// Recent is the same activity inside a fixed recent window, and it is
	// what the usage and failed feeds are ordered by.
	Recent RecentUsage `json:"recent"`
}

// RecentUsageDays is the window the feeds rank inside: how long a read or
// a failure report goes on counting as *demand* rather than as history.
//
// Ninety days, and it is a decision rather than a tuning knob. The
// counters beside it are lifetime and never decay, so without a window a
// concept that was hot in a knowledge base's first month outranks
// everything for as long as the deployment lives, and a queue meant to
// say "look at this next" answers "look at what you already looked at".
//
// It sits well inside the raw events' retention, and has to: the count is
// taken over knowledge_event, so a window longer than the retention would
// silently mean "as much as we still have" while reading like a period of
// time. TestTheRecentWindowFitsInsideRetention holds the two together.
const RecentUsageDays = 90

// RecentUsage is what happened to a concept lately: the two counts the
// feeds rank on, and the window they were counted in.
//
// The window travels with the numbers rather than being left to the
// manual. A count whose period is implicit is a count a reader cannot
// check, and this one changes meaning entirely between "three reads" and
// "three reads this quarter".
//
// Two counts and not four. search_hits is not here because it is not
// demand — the ranker records one for every id in every result set, so
// windowing it would only make the ranker's recent output the recent
// demand. `worked` is not here because nothing ranks on it.
type RecentUsage struct {
	Days    int   `json:"days"`
	Fetches int64 `json:"fetches"`
	Failed  int64 `json:"failed"`
}

// Actor identifies who created or verified a knowledge entry.
//
// Via names the caller that acted on this actor's behalf: an application
// with its own service-account identity, forwarding the identity of the
// person using it (design doc 0027). It is never a replacement for the
// actor — an entry written by tanaka through InsightFlow and one tanaka
// wrote with their own credentials must stay distinguishable, or the
// delegation becomes indistinguishable from a forgery. Empty for the
// ordinary case, where the caller acted as itself.
//
// Producer names the software that made this write, in SPEC §7's
// "<producer>/<version>" form, and is the one field here the caller
// declares about itself rather than something authentication observed
// (design doc 0052). It sits beside Kind/Name for the same reason Via
// does: a self-declared name put in place of an authenticated one would
// make "who wrote this" answerable by anyone, while one recorded next to
// it is always attributable to a caller Google verified. Empty when the
// caller named no producer.
type Actor struct {
	Kind     string `json:"kind"` // "human" | "process"
	Name     string `json:"name"`
	Via      string `json:"via,omitempty"`      // the delegating caller's identity, "" when there is none
	Producer string `json:"producer,omitempty"` // "<producer>/<version>", self-declared, "" when unnamed
}

// String renders an actor the way every surface shows provenance:
// "human:tanaka@example.co.jp", or "human:tanaka@example.co.jp via
// process:insightflow@example.iam.gserviceaccount.com" when the write came
// through a delegating application, with " using insightflow/1.4.0"
// appended when the caller named the software it was running.
func (a Actor) String() string {
	s := a.Kind + ":" + a.Name
	if a.Via != "" {
		s += " via " + a.Via
	}
	if a.Producer != "" {
		s += " using " + a.Producer
	}
	return s
}

// The actor kinds, spelled as OKF SPEC §7 spells them. ochakai wrote
// "agent:" until 0.15 (design doc 0043 §3.8): the spelling was ochakai's
// own, and the distinction it drew — an LLM agent rather than any other
// automation — is one nothing in the system ever branched on, while SPEC
// §5.3 lumps everything that is not human: into one machine-confirmed
// tier. A distinction that costs conformance and buys nothing is not one
// worth keeping.
//
// SPEC §7's third form, "<producer>/<version>", is not a kind: it names
// software where the other two name an identity, and an ochakai write
// always has an authenticated identity behind it. It is recorded in
// Actor.Producer instead, beside the actor rather than in its place
// (design doc 0052).
const (
	ActorHuman   = "human"
	ActorProcess = "process"
)

// MaxProducer bounds the producer string so a provenance column cannot be
// filled with arbitrary bulk — the same reason the delegation header is
// bounded (design doc 0027 §5.1).
const MaxProducer = 128

// ValidProducer reports whether s is SPEC §7's "<producer>/<version>"
// form: exactly one slash, both halves non-empty, no whitespace, and
// short enough to be a name rather than a payload.
//
// A colon is refused because SPEC §7 tells its three actor forms apart by
// exactly that character — "human:x" and "process:x" against
// "<producer>/<version>". A producer holding one would read as an
// identity to anything applying the convention, which is the single
// confusion this field must never create.
//
// The version half is not parsed further. SPEC §7 says nothing about how
// a version is spelled, and a store that insisted on semver would refuse
// "claude-code/2026.07" or a git sha — provenance records what the writer
// called itself, and inventing a grammar for it would only make honest
// writers unrecordable (design doc 0052 §3.2).
func ValidProducer(s string) bool {
	if s == "" || len(s) > MaxProducer || !utf8.ValidString(s) {
		return false
	}
	name, version, ok := strings.Cut(s, "/")
	if !ok || name == "" || version == "" || strings.Contains(version, "/") {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool {
		return r == ':' || unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// Verification is one recorded confirmation that an entry was checked —
// one element of OKF's verified list (SPEC §5.2), and under §5.3 the only
// thing that makes an entry anything but unverified.
//
// The ledger is append-only and plural. ochakai held a single verifier
// until 0.15, which made re-verification an overwrite: checking an entry
// again replaced the record of the last check, so "when was this last
// looked at" was answerable and "how often has this been confirmed, and
// by whom" was not (design doc 0043 §3.2). Verifying is now an append,
// and the feed that ranks by verification age reads the newest row.
//
// A verification is this instance's observation, never something a
// document asserts: import reads only whether a verified key was present,
// not who or when (design docs 0009, 0036 §2.2).
type Verification struct {
	By Actor     `json:"by"`
	At time.Time `json:"at"`
}

// Rejection is this instance's ruling that an entry was not accepted —
// the memory that stops an agent re-proposing knowledge a human already
// turned down (design doc 0081 §4).
//
// It is a ledger rather than a status because it is not a lifecycle
// value: OKF's three statuses say how ready a concept is, and "we said
// no" is a judgment about the concept, not a stage of it. As a status it
// also could not round-trip — export had to fold it onto deprecated,
// which claims the opposite thing ("was correct, no longer current")
// about the entry (design doc 0043 §3.3).
//
// Note is the reason, and belongs to the rejecter. It is distinct from
// the entry's own status_note, which belongs to whoever wrote the entry.
type Rejection struct {
	By   Actor     `json:"by"`
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// ValidActorKind reports whether kind is one ochakai records.
func ValidActorKind(kind string) bool {
	return kind == ActorHuman || kind == ActorProcess
}

// Link is an edge to another knowledge entry, derived from a markdown
// link in the body — never authored as a field (design doc 0024). Links
// are untyped: OKF SPEC §5 leaves the kind of relationship to the
// surrounding prose, so all ochakai keeps is where the link points and
// what it was called.
type Link struct {
	Target string `json:"target"`         // the target entry's id (its bundle path)
	Text   string `json:"text,omitempty"` // the anchor text; empty when the link gave none
}

// DisplayText returns how the link should read: its anchor text when the
// body gave one, else the target's last segment — the same "filename is
// the name" fallback titles use (design doc 0022). No surface calls this
// yet; it exists so a derived link is never rendered as nothing (see the
// fuzz invariant in fuzz_test.go).
func (l Link) DisplayText() string {
	if l.Text != "" {
		return l.Text
	}
	return DisplayTitle("", l.Target)
}

// UsageWindow is the date range that frames usage_count values (OKF SPEC
// §5.1): "5000 uses" means nothing without the window it was counted over.
// ochakai records the window and never computes one.
type UsageWindow struct {
	From string `json:"from,omitempty"` // YYYY-MM-DD
	To   string `json:"to,omitempty"`
}

func (w *UsageWindow) Valid() bool {
	return w == nil || (ValidDate(w.From) && ValidDate(w.To))
}

func (w *UsageWindow) sameAs(o *UsageWindow) bool {
	if w == nil || o == nil {
		return w == o
	}
	return *w == *o
}

// Extra carries the producer-defined keys of a structured OKF object —
// the ones inside a source, a parameter, an executor or an attester
// (SPEC §4.1). They are kept, not dropped (design doc 0043 §3.6).
//
// 0036 §3.5 dropped them with a note, reasoning that the point of
// promoting these families to typed fields was that four surfaces could
// describe one shape, and an open map defeats that. The premise went
// when the document became the stored form (0043 §3.1): storage keeps
// whatever the writer wrote, a schema is only needed by the projections
// that read it, and dropping a key on the way in is a loss no later
// release can undo. A round trip that silently narrows its input is not
// a round trip.
//
// In a document these ride inline, beside the keys the spec defines,
// exactly where the producer put them. In ochakai's own JSON they are
// nested under "extra": that JSON is ochakai's shape, not OKF's, and
// nesting keeps a producer key from ever colliding with a key a later
// SPEC adds.
type Extra map[string]any

func (e Extra) sameAs(o Extra) bool {
	if len(e) == 0 && len(o) == 0 {
		return true
	}
	ja, errA := json.Marshal(e)
	jb, errB := json.Marshal(o)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// Source is one piece of external material a concept derives from (OKF
// SPEC §5.1). ochakai records sources; it never fetches Resource, checks
// that it resolves, or scores the credibility signals — SPEC §5.1 is
// explicit that raw signals are for the consumer to weigh.
//
// The spec's six keys are modeled; anything else a producer wrote is
// kept in Extra (SPEC §4.1, design doc 0043 §3.6).
type Source struct {
	Resource string `json:"resource"` // REQUIRED within an entry: the artifact this came from
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"` // an actor (SPEC §7), the source's, not ochakai's
	// UsageCount is a pointer because absent and zero say different things:
	// no count at all versus a source exercised zero times over the window.
	UsageCount   *int         `json:"usage_count,omitempty"`
	LastModified string       `json:"last_modified,omitempty"` // YYYY-MM-DD
	UsageWindow  *UsageWindow `json:"usage_window,omitempty"`  // overrides the entry's window for this source
	Extra        Extra        `json:"extra,omitempty"`
}

// Valid reports whether s is a usable source. A source that names no
// artifact is not a citation, which is why SPEC §5.1 makes resource
// required inside an entry.
func (s Source) Valid() bool {
	return s.Resource != "" && ValidDate(s.LastModified) && s.UsageWindow.Valid() &&
		(s.UsageCount == nil || *s.UsageCount >= 0)
}

func (s Source) sameAs(o Source) bool {
	if s.Resource != o.Resource || s.ID != o.ID || s.Title != o.Title ||
		s.Author != o.Author || s.LastModified != o.LastModified {
		return false
	}
	if !s.UsageWindow.sameAs(o.UsageWindow) || !s.Extra.sameAs(o.Extra) {
		return false
	}
	if s.UsageCount == nil || o.UsageCount == nil {
		return s.UsageCount == o.UsageCount
	}
	return *s.UsageCount == *o.UsageCount
}

// Parameter is one declared input of an Attested Computation (OKF SPEC
// §10.2). An agent supplies parameter values; it must not author or edit
// the computation itself — which is a rule for the consumer running it,
// not something ochakai enforces.
//
// Required is a plain bool because the spec gives absent and false the
// same meaning. UsageCount is a pointer for the opposite reason.
type Parameter struct {
	Name     string `json:"name"` // REQUIRED within an entry
	Type     string `json:"type"` // REQUIRED within an entry
	Required bool   `json:"required,omitempty"`
	Extra    Extra  `json:"extra,omitempty"`
}

func (p Parameter) Valid() bool { return p.Name != "" && p.Type != "" }

func (p Parameter) sameAs(o Parameter) bool {
	return p.Name == o.Name && p.Type == o.Type && p.Required == o.Required &&
		p.Extra.sameAs(o.Extra)
}

// Executor says how an Attested Computation is run and what a run must
// hand back as evidence (OKF SPEC §10.2). ochakai stores it and never
// runs it: executing the computation and checking the receipt belong to
// the consumer (SPEC §10.5, design docs 0081, 0036 §5).
type Executor struct {
	Resource string   `json:"resource"`          // run instructions or code — what the key is for
	Receipt  []string `json:"receipt,omitempty"` // optional: the fields a run must return
	Extra    Extra    `json:"extra,omitempty"`
}

// Valid holds an executor to what SPEC §10.2 actually says. Only runtime
// is marked REQUIRED there; of executor the spec says "`resource` names
// run instructions or code" and "`receipt` declares the fields a run must
// return", neither with a requirement word. A resource is still the whole
// content of the key — an executor naming nothing to run says nothing —
// but a receipt is an optional field, and SPEC §11 forbids rejecting a
// concept for missing one. ochakai demanded it until 0079, citing a §10.2
// requirement §10.2 does not state.
func (e *Executor) Valid() bool { return e == nil || e.Resource != "" }

// Attester is the deterministic, LLM-free checker that a run of an
// Attested Computation was performed correctly (OKF SPEC §10.2). As with
// Executor, ochakai records the path and never executes it.
type Attester struct {
	Resource string `json:"resource"` // REQUIRED within attester
	Extra    Extra  `json:"extra,omitempty"`
}

func (a *Attester) Valid() bool { return a == nil || a.Resource != "" }

// Knowledge is the common envelope for all knowledge types. Every key OKF
// v0.2 defines is a field here; Attrs carries only producer-defined
// extension keys (SPEC §4.1, design doc 0036 §2). Prose lives in Body
// (markdown). The envelope maps 1:1 to an OKF document (YAML frontmatter
// + markdown).
type Knowledge struct {
	Type        Type         `json:"type"`
	ID          string       `json:"id"`              // full bundle path, the sole key (design doc 0017)
	Title       string       `json:"title,omitempty"` // display-name override; empty means the id's last segment is the name (design doc 0022)
	Description string       `json:"description,omitempty"`
	Resource    string       `json:"resource,omitempty"` // canonical URI of the underlying asset (OKF recommended key)
	Tags        []string     `json:"tags,omitempty"`
	Sources     []Source     `json:"sources,omitempty"`      // the material this derives from (OKF SPEC §5.1)
	UsageWindow *UsageWindow `json:"usage_window,omitempty"` // the range the sources' usage_counts were counted over
	Status      Status       `json:"status"`
	StatusNote  string       `json:"status_note,omitempty"` // free-form reason for the current status (why rejected/deprecated)
	StaleAfter  string       `json:"stale_after,omitempty"` // YYYY-MM-DD; the entry is stale on and after this day (OKF SPEC §5.5, design doc 0036 §3.5)
	// The Attested Computation contract (OKF SPEC §10.2). These live in the
	// envelope for every type — Go structs do not change shape per type, and
	// ochakai enforces no per-type schema (design doc 0036 §5). What varies
	// by type is validation of Runtime alone (design doc 0036 §3.4) and
	// which sections the Web UI shows.
	//
	// ochakai records this contract and never acts on it: it does not run
	// Executor, check a receipt, run Attester, or fetch Computation. That
	// is the consumer's procedure (SPEC §10.5, design docs 0081, 0036 §5).
	Runtime     string      `json:"runtime,omitempty"`     // required by SPEC when the type is Attested Computation
	Parameters  []Parameter `json:"parameters,omitempty"`  // inputs an agent may fill in
	Computation string      `json:"computation,omitempty"` // path to the computation; empty means the body's "# Computation" fence
	Executor    *Executor   `json:"executor,omitempty"`
	Attester    *Attester   `json:"attester,omitempty"`
	CreatedBy   Actor       `json:"created_by"`
	UpdatedBy   Actor       `json:"updated_by"` // who last changed the content — OKF's generated.by (design doc 0036 §3.3)
	// Verifications and Rejection are this instance's ledgers, not fields
	// of the document: read-only on every surface, never accepted from a
	// payload, and written only by verify and reject (design doc 0043
	// §§3.2-3.3). Oldest verification first.
	Verifications []Verification `json:"verifications,omitempty"`
	Rejection     *Rejection     `json:"rejection,omitempty"`
	Links         []Link         `json:"links,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Body          string         `json:"body,omitempty"`
	// Files is read-only metadata (no bytes), populated on single-
	// entry reads. Files are managed through their own endpoints,
	// never through create/update payloads.
	Files []File `json:"files,omitempty"`
	// LinkedFrom is read-only like Files and populated the same way, on
	// single-entry reads: the rows okf.ViewOf carries into
	// View.LinkedFrom. Never accepted from a payload — links are derived
	// from bodies (design doc 0024), and this is the same edge read the
	// other way round.
	LinkedFrom []Backlink `json:"linked_from,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// ContentChangedAt is when what the entry says last changed, which is
	// what OKF's generated.at means (SPEC §5.2). It parted ways with
	// UpdatedAt when the document became the writer's own bytes:
	// reformatting an entry writes the row and moves its version without
	// changing a word of what it says (design doc 0046 §3.4).
	ContentChangedAt time.Time `json:"content_changed_at"`
	// Doc is the document as it was received: the writer's own bytes,
	// normalized for line endings and stripped of the keys this instance
	// owns, and nothing else (design doc 0046 §2.2). It is what the store
	// holds and what ContentHash covers.
	//
	// Empty in two cases, both of which fall back to the canonical
	// rendering: a row read for a listing, which does not select the
	// column because the index columns beside it already answer the
	// query, and an entry composed in memory rather than parsed from a
	// document.
	Doc string `json:"-"`
	// Trust is the tier SPEC §5.3 derives from Verifications, carried on
	// the wire so a caller that only reads a listing gets the same answer
	// as one that reads the ledger (design doc 0046 §3.10). Derived on
	// read; never accepted from a payload.
	Trust Trust `json:"trust,omitempty"`
	// ContentHash is the entry's version: the SHA-256 of the document as
	// stored (design docs 0043 §3.4, 0046 §3.4), which is what the ETag
	// carries and what If-Match is checked against. It covers the
	// content alone, so a verification, a rejection or a file beside the
	// entry leaves it where it was — only an edit moves it.
	ContentHash string `json:"content_hash,omitempty"`
}

// EnvelopeKeys names every frontmatter key the envelope owns, in the
// spelling a bundle uses. Attrs carries producer extension keys (SPEC
// §4.1) and nothing else, so a payload naming one of these inside attrs is
// rejected rather than stored where nothing will read it (design doc 0036
// §3.5). Server-owned provenance (generated, verified, created_by,
// rejected_*) is not here: those keys are ignored on the way in, and the
// bundle writer keeps its own longer list.
var EnvelopeKeys = []string{
	"type", "id", "title", "description", "resource", "tags",
	"sources", "usage_window",
	"status", "status_note", "stale_after",
	"runtime", "parameters", "computation", "executor", "attester",
}

// OchakaiEnvelopeKeys names the two envelope keys that are ochakai's own
// rather than OKF's: SPEC v0.2 defines no top-level `id` (a concept's id
// is its path, §2 — `sources[].id` is a different key) and no
// `status_note`. Both are read into columns and written back into
// bundles, so they stay envelope keys — but the frontmatter filter's
// vocabulary is OKF's (design doc 0074 §4.2), and when ochakai is the
// producer its own extension keys are refused the same way anybody
// else's are (design doc 0064).
var OchakaiEnvelopeKeys = []string{"id", "status_note"}

// FilterOwnedKeys names, for each OKF key ochakai reads through a column
// of its own, the filter that owns the question — the one a frontmatter
// filter must not answer a second time (design doc 0047 §2.1). The column
// and the jsonb path do not say the same thing (a document that writes no
// status reads as stable to `status=`, since that is OKF's default, and as
// nothing to `fm.status`), and a caller cannot see which one it got.
var FilterOwnedKeys = map[string]string{
	"type":        "type=",
	"status":      "status=",
	"tags":        "tag=",
	"sources":     "source=URI",
	"stale_after": "sort=stale_after",
}

// AskableFrontmatterKeys returns the frontmatter keys a filter may name,
// sorted: every key OKF defines, less the ones a filter of their own
// already asks (design doc 0047 §2). A producer's extension key is not
// here — it is stored and handed back exactly as written (SPEC §4.1), but
// the query vocabulary is OKF's. That rule reads ochakai's own two
// envelope keys out too (OchakaiEnvelopeKeys): `fm.id` asks what the
// address already answers, and an extension key ochakai may ask but
// nobody else may would make the refusal's own reason false.
//
// It is derived from EnvelopeKeys rather than written out, which is what
// makes the query surface additive: the day OKF adds a key and that list
// learns its spelling, the key is askable — no column and no migration,
// because the whole frontmatter is already indexed (design doc 0046
// §3.11). Every surface describes itself from this, so no help text can
// promise a key the server refuses.
func AskableFrontmatterKeys() []string {
	out := make([]string, 0, len(EnvelopeKeys))
	for _, k := range EnvelopeKeys {
		if FilterOwnedKeys[k] == "" && !slices.Contains(OchakaiEnvelopeKeys, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Verified reports whether anyone has confirmed this entry — SPEC §5.3's
// signal, which is the presence of a verification and not a status.
func (k *Knowledge) Verified() bool { return len(k.Verifications) > 0 }

// LastVerified returns the newest verification, or nil when there is
// none. The ledger is stored oldest-first.
func (k *Knowledge) LastVerified() *Verification {
	if len(k.Verifications) == 0 {
		return nil
	}
	return &k.Verifications[len(k.Verifications)-1]
}

// Ruling names the decision a human recorded about an entry, or "" when
// nobody has ruled on it. Two of the three now live in ledgers and one is
// still a lifecycle value (design doc 0043 §§3.2-3.3), so the guards ask
// the entry rather than its status — which is where the question always
// belonged, since what it means is "did somebody decide something about
// this", not "what stage is it at".
//
// A rejection outranks a verification when an entry carries both: Verify
// clears a rejection but Reject keeps the verifications, so the pair
// means somebody vouched for this and somebody later ruled against it,
// and the later ruling is the one in force.
type Ruling string

const (
	RulingRejected   Ruling = "rejected"
	RulingVerified   Ruling = "verified"
	RulingDeprecated Ruling = "deprecated"
)

func (k *Knowledge) Ruling() Ruling {
	switch {
	case k.Rejection != nil:
		return RulingRejected
	case k.Verified():
		return RulingVerified
	case k.Status == StatusDeprecated:
		return RulingDeprecated
	}
	return ""
}

// Curated reports whether a human has ruled on this entry. Surfaces with
// no If-Match channel must not replace one in place (design doc 0015
// §3.1).
func (k *Knowledge) Curated() bool { return k.Ruling() != "" }

// URI returns the canonical reference, e.g. "ochakai://metrics/revenue" —
// the scheme plus the entry's id (its bundle path, design doc 0017).
func (k *Knowledge) URI() string { return fmt.Sprintf("ochakai://%s", k.ID) }

// DisplayTitle returns the entry's display name: the title when one is
// set, else the id's final segment — with title optional (design doc
// 0074 §1), the filename usually is the name.
func (k *Knowledge) DisplayTitle() string { return DisplayTitle(k.Title, k.ID) }

// DisplayTitle is the package-level form for projections that carry
// title and id without a full Knowledge (browse entries, and every wire
// projection since design doc 0064 stopped resolving the title on the
// wire — deriving it is the reader's job, as OKF SPEC §4.1 says).
func DisplayTitle(title, id string) string {
	if title != "" {
		return title
	}
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// Normalize returns s in Unicode NFC. IDs, link targets, file
// names, and search queries are compared byte-wise against stored text,
// and macOS filesystems hand paths back NFD-decomposed — the same
// visible path must land on the same entry, so every boundary that
// accepts one normalizes first (design doc 0022). Bodies and titles are
// content, not keys, and are kept as written.
func Normalize(s string) string { return norm.NFC.String(s) }

// MissKey is what two askings of the same question have to share to be
// counted as one gap (migration 0037). It is a grouping key and never a
// stored answer: what a person typed is kept as they typed it, and this
// only decides which typings land in the same row of the list.
//
// Folded, in this order: compatibility normalization, so ＡＢＣ and ABC
// and ｶﾀｶﾅ and カタカナ are one word; case, so a sentence and its
// capitalization are one question; inner whitespace, so a double space
// is not a second gap; and punctuation at either end, so "売上とは?" and
// "売上とは" are the same asking. Punctuation *inside* stays — "q3" and
// "q/3" are not obviously the same thing, and a key that folds too much
// merges gaps that wanted separate answers.
func MissKey(s string) string {
	folded := strings.ToLower(width.Fold.String(norm.NFKC.String(s)))
	return strings.Trim(strings.Join(strings.Fields(folded), " "), missKeyEdge)
}

// missKeyEdge is the punctuation dropped from either end of a miss key.
// Latin and Japanese spellings of the same marks, because a question
// mark is a question mark.
const missKeyEdge = `?？!！.。,、:：;；'"“”「」『』()（）[]【】 `

// SameContent reports whether o carries the same authored content as k:
// the fields a writer controls (title, description, resource, tags,
// sources, usage_window, status, status_note, stale_after, the Attested
// Computation contract, links, attrs, body). Server-managed provenance and
// timestamps (created_*, updated_by, verified_*, rejected_*, updated_at)
// and the file list are not content. Attrs are compared as canonical
// JSON, so the same value decoded from YAML (int) and from JSONB (float64)
// compares equal; values JSON cannot encode compare as different.
//
// Status is compared as a reader reads it, so a document that names
// stable and one that names nothing are the same claim (SPEC §5.4) —
// which is what lets a silent document round-trip without the index
// column it derives disagreeing with it.
//
// The list comparisons are order-sensitive on purpose: reordering an
// entry's citations or an Attested Computation's parameters is a change to
// what it says, and a write that reorders them must not be swallowed as a
// no-op.
func (k *Knowledge) SameContent(o *Knowledge) bool {
	return k.Type == o.Type && k.ID == o.ID &&
		k.Title == o.Title && k.Description == o.Description &&
		k.Resource == o.Resource &&
		k.Lifecycle() == o.Lifecycle() && k.StatusNote == o.StatusNote &&
		k.StaleAfter == o.StaleAfter &&
		k.Runtime == o.Runtime && k.Computation == o.Computation &&
		k.Body == o.Body &&
		slices.Equal(k.Tags, o.Tags) &&
		slices.EqualFunc(k.Sources, o.Sources, Source.sameAs) &&
		k.UsageWindow.sameAs(o.UsageWindow) &&
		slices.EqualFunc(k.Parameters, o.Parameters, Parameter.sameAs) &&
		executorsEqual(k.Executor, o.Executor) &&
		attestersEqual(k.Attester, o.Attester) &&
		slices.Equal(k.Links, o.Links) &&
		attrsEqual(k.Attrs, o.Attrs)
}

func executorsEqual(a, b *Executor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Resource == b.Resource && slices.Equal(a.Receipt, b.Receipt) &&
		a.Extra.sameAs(b.Extra)
}

func attestersEqual(a, b *Attester) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Resource == b.Resource && a.Extra.sameAs(b.Extra)
}

func attrsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// Revision is one entry in an entry's change history: who changed it,
// how, when, and the full snapshot as of that change. The audit trail
// behind "every change kept as a revision".
type Revision struct {
	Rev       int       `json:"rev"`
	Change    string    `json:"change"` // one of Changes
	ChangedBy Actor     `json:"changed_by"`
	ChangedAt time.Time `json:"changed_at"`
	// Document is the entry as it stood, as an OKF document (design doc
	// 0043 §3.9). It replaced a JSON snapshot of the Go struct: a record
	// whose shape depends on the reader knowing every past release is the
	// distortion, not the fidelity, and OKF is an anchor that outlasts
	// them. It also means a diff between two revisions reads.
	Document string `json:"document"`
}

// validSegment reports whether s can be one ID path segment. OKF
// prescribes no character set — a concept ID is a file path, nothing
// more — so only path safety constrains it (design doc 0019): non-empty
// valid UTF-8, at most 128 bytes, no control characters, and not
// starting with "." (which rules out ".", "..", and hidden files, the
// paths bundle import skips as never-knowledge).
func validSegment(s string) bool {
	if s == "" || len(s) > 128 || strings.HasPrefix(s, ".") || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' {
			return false
		}
	}
	return true
}

// ValidID reports whether id is a valid knowledge ID: path segments
// separated by "/", following OKF's "concept ID = file path" rule (the
// bundle path is "<id>.md", design doc 0017). The final segment must not
// be "index" or "log" — those filenames belong to OKF's reserved
// per-directory index.md (generated navigation) and log.md (history).
func ValidID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	segs := strings.Split(id, "/")
	for _, s := range segs {
		if !validSegment(s) {
			return false
		}
	}
	last := segs[len(segs)-1]
	return last != "index" && last != "log"
}

// ValidIDPrefix reports whether prefix can lead a knowledge ID: empty
// (the root) or path segments separated by "/". Unlike ValidID, a final
// "index" segment is fine — it only names a directory here, and the
// index.md reservation is about a document's own filename.
func ValidIDPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(prefix) > 512 {
		return false
	}
	for _, s := range strings.Split(prefix, "/") {
		if !validSegment(s) {
			return false
		}
	}
	return true
}

// ToTypes converts transport-layer type filters (query parameters, tool
// inputs) into domain types — shared by the REST and MCP surfaces so the
// conversion is written once.
func ToTypes(ss []string) []Type {
	out := make([]Type, 0, len(ss))
	for _, s := range ss {
		out = append(out, Type(s))
	}
	return out
}

// ToStatuses is ToTypes for status filters.
func ToTrusts(ts []string) []Trust {
	out := make([]Trust, 0, len(ts))
	for _, t := range ts {
		out = append(out, Trust(t))
	}
	return out
}

// ToStatuses converts wire strings to statuses.
func ToStatuses(ss []string) []Status {
	out := make([]Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, Status(s))
	}
	return out
}

// Summary is the read-only projection of an entry: what a listing row
// carries, and what a single read shows beside the document (design doc
// 0043 §3.5).
//
// It holds nothing the document holds. The entry's content — its
// citations, its computation contract, its producer keys, its body — is
// in the document, once; a projection that repeated any of it would be a
// second copy to keep in step, which is exactly what document-first
// exists to remove. What is here is either an address (id), a signal a
// caller ranks or filters on, or a ledger's answer reduced to a bool.
//
// Verified and Rejected are those answers. A row does not carry the
// ledgers themselves: "has anyone confirmed this" is what a reader of a
// list wants, and who and when is a question about one entry.
type Summary struct {
	ID   string `json:"id"`
	Type Type   `json:"type"`
	// Title is the concept's own, and absent when the document declared
	// none: OKF SPEC §4.1 gives title no default — it is RECOMMENDED, and
	// "If omitted, consumers MAY derive a title from the filename". A
	// resolved title asserted a value the document never declared. Status
	// is resolved (Lifecycle below) because SPEC §5.4 does give it a
	// normative default, so filling that one in reports what the spec says
	// the document means (design doc 0064).
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      Status   `json:"status"`
	StaleAfter  string   `json:"stale_after,omitempty"`
	// Trust is OKF's tier (SPEC §5.3), derived from the verification
	// ledger; a boolean is not a distinction the spec draws (design doc
	// 0046 §3.10).
	Trust Trust `json:"trust"`
	// VerifiedAt is the newest verification's time, absent when there is
	// none. A row carries it because it is what the verification-age feed
	// ranks by — a reviewer reading that feed is reading these times.
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	Rejected    bool       `json:"rejected"`
	Links       []Link     `json:"links,omitempty"`
	ContentHash string     `json:"content_hash"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// SummaryOf projects an entry. Title travels raw — absent when the
// document declared none — so the projection never claims a title the
// writer did not write (design doc 0064). A reader that needs a display
// name calls DisplayTitle, the way every listing row already did.
func SummaryOf(k *Knowledge) Summary {
	s := Summary{
		ID: k.ID, Type: k.Type, Title: k.Title, Description: k.Description,
		Resource: k.Resource, Tags: k.Tags, Status: k.Lifecycle(), StaleAfter: k.StaleAfter,
		Trust: TrustOf(k.Verifications), Rejected: k.Rejection != nil, Links: k.Links,
		ContentHash: k.ContentHash, CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
	}
	if v := k.LastVerified(); v != nil {
		at := v.At
		s.VerifiedAt = &at
	}
	return s
}

// URI is the entry's canonical address, as on Knowledge: every shape that
// names an entry should name it the same way.
func (s *Summary) URI() string { return fmt.Sprintf("ochakai://%s", s.ID) }

// Generated is OKF's generated family (SPEC §5.2): who the content
// stands by, and when it last changed.
type Generated struct {
	By Actor     `json:"by"`
	At time.Time `json:"at"`
}

// Observed is what this instance saw happen to an entry, as against what
// the entry says. The split is design doc 0009's: a bundle carries
// knowledge, and provenance is an observation this instance made — so it
// travels beside the document rather than inside it, and import never
// reads it back.
type Observed struct {
	CreatedBy Actor          `json:"created_by"`
	Generated Generated      `json:"generated"`
	Verified  []Verification `json:"verified,omitempty"`
	Rejection *Rejection     `json:"rejection,omitempty"`
}

func ObservedOf(k *Knowledge) Observed {
	return Observed{
		CreatedBy: k.CreatedBy,
		// generated.at is OKF SPEC §5.2's "last meaningful change", which
		// is ContentChangedAt — the same instant the exported document's
		// generated.at carries (internal/okf). UpdatedAt is the row's
		// version and moves for a write that only reformats the document,
		// so the JSON used to report a different instant from the
		// document rendered beside it (design doc 0064).
		Generated: Generated{By: k.UpdatedBy, At: k.ContentChangedAt},
		Verified:  k.Verifications,
		Rejection: k.Rejection,
	}
}

// View is a read of one entry: the document it is, the projection to read
// it by, and what this instance observed about it (design doc 0043 §3.5).
//
// Document is the stored document — the writer's own bytes (design doc
// 0046 §2.2), without the keys this instance owns, which Observed carries
// instead. It is what the editing surfaces read and write back, so it has
// to be the thing that was written: a rendering here would mean every
// edit through the web UI or `ochakai get` silently reformatted the entry.
type View struct {
	ID       string   `json:"id"`
	Document string   `json:"document"`
	Summary  Summary  `json:"summary"`
	Observed Observed `json:"observed"`
	Files    []File   `json:"files,omitempty"`
	// LinkedFrom is the one thing about an entry its own document cannot
	// say: what links at it (design doc 0106). The insight that explains
	// a metric points at the metric, so a read of the metric alone would
	// never surface it. Rows, not documents — each is a pointer the
	// caller can spend a fetch on — and only on single-entry reads, like
	// Files. The complete, pageable reverse lookup is search's links_to.
	LinkedFrom []Backlink `json:"linked_from,omitempty"`
	// Plan is what the write did, on the response to a write: created,
	// updated, or unchanged. Absent on a read, which did none of them.
	//
	// It is one of the three words below rather than a boolean, because
	// "nothing happened" is one of three answers and not the negation of
	// the other two.
	Plan string `json:"plan,omitempty"`
}

// The three things a write can turn out to have been (design doc 0074
// §5, carried into the body by 0097).
// They live here rather than in the surface that first needed them
// because every surface says them now — a header on REST, a field in the
// body on REST and MCP, a line on the CLI — and a vocabulary with one
// home is a vocabulary that cannot drift between them.
const (
	PlanCreated   = "created"
	PlanUpdated   = "updated"
	PlanUnchanged = "unchanged"
)

// Plans is the closed set, for the schemas and the surface count.
var Plans = []string{PlanCreated, PlanUpdated, PlanUnchanged}

// PlanOf names what a write did from what the store answered: whether
// the id was free, and whether the bytes differed from what was there.
func PlanOf(created, changed bool) string {
	switch {
	case created:
		return PlanCreated
	case !changed:
		return PlanUnchanged
	default:
		return PlanUpdated
	}
}

// URI is the entry's canonical address, as on Knowledge and Summary.
func (v *View) URI() string { return fmt.Sprintf("ochakai://%s", v.ID) }

// LastVerified returns the newest verification this instance recorded,
// or nil when there is none. The ledger is stored oldest-first.
func (o *Observed) LastVerified() *Verification {
	if len(o.Verified) == 0 {
		return nil
	}
	return &o.Verified[len(o.Verified)-1]
}

// SearchHit is one search result with its ranking score. Usage is
// populated only by the sort=usage listing (the draft review feed), where
// the promotion signal is the point; it stays nil for search results and
// the verified_at feed so their wire shape is unchanged.
//
// It carries the projection, not the entry. Search used to return whole
// entries on the reasoning that there they are the answer rather than a
// duplicate of one (design doc 0033) — true while an entry was a bag of
// fields, and false once it is a document: a result list that shipped ten
// documents to answer "which of these should I read" spends the caller's
// context on nine it will not. The document is one fetch away by id, and
// get_context is the call that returns entries.
type SearchHit struct {
	Summary
	Score float64 `json:"score"`
	// Snippet is the passage of the body where the query's own words
	// land, cut to a readable window. It answers "why did this match?",
	// which title and description cannot: they say what somebody wrote
	// about the concept as a whole, and a caller deciding which of ten
	// hits to open was paying a fetch per row to find out.
	//
	// Absent for two reasons, both meaning "there is no passage to
	// show": the words landed in the name or the description rather than
	// the body, where the row already shows the match; or the concept
	// was found by meaning rather than by text, where pointing at the
	// opening of the body would answer the question with something that
	// is not the reason.
	Snippet string `json:"snippet,omitempty"`
	Usage   *Usage `json:"usage,omitempty"`
	// Files is metadata only, and only on the REST list surface,
	// so a UI can draw a thumbnail without a round trip per row (design
	// doc 0015). It is not part of Summary: a row is a projection of the
	// entry, and a file is an object beside it.
	Files []File `json:"files,omitempty"`
}

// Backlink is one row of a read's linked_from: an entry whose body links
// at the one being read. The fields are what a caller needs to decide
// whether to spend a fetch on it, and the description carries the weight
// — an entry with an empty description is nearly invisible here, which
// is one more reason curation pays. No score: the answer is a set, not a
// ranking (the LinksTo filter's own rule), and rows arrive in address
// order.
type Backlink struct {
	ID   string `json:"id"`
	Type Type   `json:"type"`
	// Title is the concept's own, absent when the document declared none
	// (design doc 0064; OKF SPEC §4.1).
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Status      Status `json:"status"`
	Trust       Trust  `json:"trust"`
}

// BacklinkOf projects an entry down to a backlink row.
func BacklinkOf(k *Knowledge) Backlink {
	return Backlink{
		ID: k.ID, Type: k.Type, Title: k.Title, Description: k.Description,
		Status: k.Lifecycle(), Trust: TrustOf(k.Verifications),
	}
}

// ConceptPath is the bundle path a concept lives at: its id with ".md"
// on the end (OKF SPEC §2, design doc 0017). The path is what an object
// is keyed by (design doc 0046 §3.1) and the id is the address a concept
// is called by, so every place that has one and needs the other goes
// through here rather than concatenating a suffix of its own.
func ConceptPath(id string) string { return id + ".md" }

// ConceptID is the inverse: the id addressed by a bundle path, and ""
// when the path is not a markdown file and so names no concept.
func ConceptID(path string) (string, bool) {
	return strings.CutSuffix(path, ".md")
}
