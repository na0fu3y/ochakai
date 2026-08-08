package domain

import "time"

// Stats is the instance's own view of the improvement loop: what the
// knowledge base is made of now, what moved through it lately, and what
// was asked for and not found (design doc 0051 §3.5).
//
// Every other measurement in ochakai is per entry — how often this one
// was searched, fetched, reported worked. Those are counters, and a loop
// needs a trend: whether the verified share is growing, whether failures
// are being answered, whether the questions arriving have answers. That
// is a question about the instance, not about any entry in it, and this
// is the only shape that can carry it.
//
// Two kinds of number live here, and the field comments say which is
// which: a *state* is how things stand right now (how many entries, how
// many are waiting for review), and a *flow* is what happened inside
// WindowDays (how many were created, verified, reported on, missed).
//
// The queue depths are 0049's, carried here rather than counted again:
// that record kept the place for these metrics as siblings of its own
// answer (§3.1), and a second count of the same queue is a second thing
// to be wrong.
type Stats struct {
	// At is when the server computed this — the answers are not a
	// snapshot of one instant, since each is its own query.
	At time.Time `json:"at"`
	// WindowDays is how far back the flow numbers reach.
	WindowDays int `json:"window_days"`

	Concepts StatsConcepts `json:"concepts"`
	// Queues is what design doc 0049's GET /api/v1/queues returns, under
	// the key it returns it under: how much each review queue is
	// holding, right now. It travels here as a sibling of the rest
	// because 0049 §3.1 kept the place for it, and it is counted by that
	// face's own query — the same three numbers, from one implementation,
	// unscoped by any prefix.
	Queues   QueueCounts   `json:"queues"`
	Review   StatsReview   `json:"review"`
	Outcomes StatsOutcomes `json:"outcomes"`
	Misses   StatsMisses   `json:"misses"`
	// Dropped is what the loop observed and then lost inside the window
	// — the footnote every other number here depends on. Usage is
	// best-effort by decision (design doc 0029 §3.1): it buffers in
	// memory, refuses events past a cap rather than growing without
	// bound, and loses a batch whose write fails instead of keeping a
	// retry queue. That the loss showed only in a log line was the gap:
	// a curator reading how often a concept was fetched could not tell 4
	// from 40. Zero here is what makes the rest trustworthy.
	Dropped StatsDropped `json:"dropped"`
}

// StatsDropped counts the observations that never reached the database
// in the window. Unscoped by prefix, for the reason misses are: a
// dropped batch is a number, and the ids that were in it are gone.
type StatsDropped struct {
	// Events is search hits, fetches and outcome reports together —
	// refused by the buffer, or lost with a failed flush.
	Events int64 `json:"events"`
	// Misses is unanswered questions lost the same two ways.
	Misses int64 `json:"misses"`
}

// StatsConcepts is what the base is made of (state), plus how much of it
// is new (flow). Deleted concepts are in none of it.
type StatsConcepts struct {
	Total int64 `json:"total"`
	// Status and Trust carry every value of their vocabulary, zero
	// included, so a reader never has to tell "none" from "not
	// reported". They are maps rather than fields because the
	// vocabularies have one home (Statuses, Trusts) and a struct here
	// would be a second one.
	Status map[string]int64 `json:"status"`
	Trust  map[string]int64 `json:"trust"`
	// Rejected is how many live entries carry this instance's rejection
	// (design doc 0043 §3.3). They are counted in Status and Trust too —
	// a rejection is a ruling beside the lifecycle value, not one of them.
	Rejected int64 `json:"rejected"`
	// Created is how many of the live entries were created in the window.
	Created int64 `json:"created"`
}

// StatsReview is the human side of the loop as a flow: what review did
// inside the window. How much is waiting for it is Queues, which is a
// state and belongs to design doc 0049.
type StatsReview struct {
	// Verifications is how many verifications were recorded in the
	// window — the ledger's own rows (design doc 0043 §3.2), so
	// re-verifying an entry counts each time, and an entry verified
	// twice counts twice. This is the number that answers "how much
	// moved through review last week", which a queue depth cannot: a
	// queue says how much is left, never how much went through.
	Verifications int64 `json:"verifications"`
}

// StatsOutcomes is what callers reported back in the window — the
// evidence half of the loop (report_outcome).
type StatsOutcomes struct {
	Worked int64 `json:"worked"`
	Failed int64 `json:"failed"`
}

// StatsMisses is what was asked for and not found in the window: the
// questions this knowledge base could not answer, which is the list of
// what to write next (design doc 0051 §2).
type StatsMisses struct {
	// Recording says whether this deployment keeps misses at all. False
	// makes Count and Queries meaningless rather than zero — a public
	// deployment does not keep what strangers typed (§3.4), and any
	// deployment can turn it off.
	Recording bool `json:"recording"`
	// Count is every miss in the window, including the ones whose query
	// did not make the Queries list.
	Count int64 `json:"count"`
	// Queries is the most-asked of them, most first.
	Queries []MissedQuery `json:"queries"`
}

// MissedQuery is one question, as it was asked, and how often it came
// back empty.
type MissedQuery struct {
	Query  string    `json:"query"`
	Count  int64     `json:"count"`
	LastAt time.Time `json:"last_at"`
}
