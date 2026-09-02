// Listings: total orders that page with a keyset cursor, which is what
// makes them different from a search (design doc 0050). The review feeds
// and the queue counts read from here.
package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// After is the position a listing resumes from: the values of the
// ordering columns for the last row handed out, in the order they are
// ordered by, with the id last (design doc 0050 §2.1). A nil element is
// SQL NULL — the never-verified entries at the tail of an ASC NULLS LAST
// order. Nil After is the first page.
//
// The values travel as text and are cast back per column, which is what
// lets the same shape carry a timestamp, a date and a count.
type After struct {
	Keys []*string
	ID   string
}

// orderCol describes one column of a listing's ORDER BY, so the same
// keyset predicate can be built for every feed rather than once per
// feed. cast is the type the cursor's text is compared as.
type orderCol struct {
	expr string
	cast string
	desc bool
	// null marks a column ordered ASC NULLS LAST — the one nullable
	// position any of the feeds has (verification age).
	null bool
}

// utcCursorKey rewrites a one-key cursor's value into RFC 3339, so the
// ::timestamptz cast in the keyset predicate resolves it the same way on
// every deployment.
//
// The cursor carries the position as the reader saw it, and a stale_after
// reads back as a bare date whenever its time of day is midnight
// (domain.FormatInstant). PostgreSQL resolves '2026-12-31'::timestamptz
// in the session's own TimeZone, which would put the resumed page a few
// hours off the page before it — the one place widening the column to an
// instant (design doc 0133) could have let a timezone back in.
//
// A cursor value that is neither spelling is malformed rather than
// mysterious: it did not come from a listing this server rendered.
func utcCursorKey(a *After) (*After, error) {
	if a == nil || len(a.Keys) != 1 || a.Keys[0] == nil {
		return a, nil
	}
	t, ok := domain.ParseInstant(*a.Keys[0])
	if !ok {
		return nil, fmt.Errorf("cursor holds %q, which is not %s", *a.Keys[0], domain.InstantsHint)
	}
	v := t.Format(time.RFC3339)
	return &After{Keys: []*string{&v}, ID: a.ID}, nil
}

// keysetAfter renders "strictly after this position" for an ORDER BY of
// cols followed by idExpr, appending the cursor's values to args. It
// returns "" when there is no position to resume from, so the caller can
// AND it in unconditionally.
//
// The predicate is the lexicographic comparison spelled out rather than
// PostgreSQL's row constructor: a row comparison cannot express one
// column descending beside another ascending, and every feed here mixes
// the two (most-failed first, fewest corroborations first).
func keysetAfter(cols []orderCol, idExpr string, a *After, args *[]any) (string, error) {
	if a == nil {
		return "", nil
	}
	if len(a.Keys) != len(cols) {
		return "", fmt.Errorf("cursor carries %d keys, this listing orders by %d", len(a.Keys), len(cols))
	}
	// Built from the id outwards: the innermost term is "same on every
	// key, later id", and each column wraps it with "strictly after on
	// this key, or equal on it and after by everything below".
	*args = append(*args, a.ID)
	pred := fmt.Sprintf("%s > $%d", idExpr, len(*args))
	for i, col := range slices.Backward(cols) {
		c, v := col, a.Keys[i]
		var strict, eq string
		if v == nil {
			// Nothing sorts after a NULL in ASC NULLS LAST, so the walk
			// can only continue among the other NULLs, by id.
			if !c.null {
				return "", fmt.Errorf("cursor holds no value for %s, which is never null", c.expr)
			}
			strict, eq = "false", c.expr+" IS NULL"
		} else {
			*args = append(*args, *v)
			ref := fmt.Sprintf("$%d::%s", len(*args), c.cast)
			op := ">"
			if c.desc {
				op = "<"
			}
			strict = fmt.Sprintf("%s %s %s", c.expr, op, ref)
			if c.null {
				// A NULL sorts after every value, so it is after this one.
				strict = fmt.Sprintf("(%s OR %s IS NULL)", strict, c.expr)
			}
			eq = fmt.Sprintf("%s = %s", c.expr, ref)
		}
		pred = fmt.Sprintf("(%s OR (%s AND %s))", strict, eq, pred)
	}
	return " AND " + pred, nil
}

// ListByAddress lists the entries a filter matches, by id. It is the
// listing the reverse lookups take: "what cites this resource" (design
// doc 0037 §2.3) and "what links at this entry" are both questions with
// no text to rank by, so they list rather than search. Ordered by id
// because the answer is a set — a stable, readable order beats a
// relevance score nothing computed, and it is a total order, so a cursor
// can resume it (design doc 0050).
//
// Named for the order and not for either filter: which entries it
// returns is the filter's business, and it had a filter's name while
// serving two.
func (s *Store) ListByAddress(ctx context.Context, f Filter, after *After, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	keyset, err := keysetAfter(nil, "id", after, &args)
	if err != nil {
		return nil, err
	}
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeSelect+` FROM object WHERE %s%s
		ORDER BY id LIMIT %d`, where, keyset, limit), args...)
}

// ListByStaleAfter returns the entries whose declared expiry has passed,
// most overdue first — the feed for "what did we say needed re-checking
// by now" (design doc 0037 §2.1).
//
// Only entries that are stale today. A declaration that has not come due
// is not work, and 0025 asked feeds to be queues a reviewer can finish;
// the same reason sort=failed lists only entries with failures.
//
// Unlike the other two feeds, verifying does not empty this one: the date
// is the writer's declaration, not something the server observed, so
// clearing it means editing the entry to re-declare an expiry
// (design doc 0037 §2.2).
func (s *Store) ListByStaleAfter(ctx context.Context, f Filter, after *After, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	after, err := utcCursorKey(after)
	if err != nil {
		return nil, err
	}
	keyset, err := keysetAfter(
		[]orderCol{{expr: "stale_after", cast: "timestamptz"}}, "id", after, &args)
	if err != nil {
		return nil, err
	}
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeSelect+` FROM object WHERE %s
		AND `+pastExpiry("")+`%s
		ORDER BY stale_after ASC, id LIMIT %d`, where, keyset, limit), args...)
}

// ListByVerifiedAt returns filtered entries ordered by verification age,
// oldest first (never-verified entries last). This is the feed for golden
// query canary runs: "which verified queries have gone longest unchecked".
func (s *Store) ListByVerifiedAt(ctx context.Context, f Filter, after *After, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	keyset, err := keysetAfter(
		[]orderCol{{expr: lastVerifiedAt("object"), cast: "timestamptz", null: true}},
		"id", after, &args)
	if err != nil {
		return nil, err
	}
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeSelect+` FROM object WHERE %s%s
		ORDER BY `+lastVerifiedAt("object")+` ASC NULLS LAST, id LIMIT %d`, where, keyset, limit), args...)
}

// usageLateral aggregates a knowledge entry's per-event running totals
// (see usage.go) in one pass, exposing them as u.search_hits … u.failed and
// u.last_used_at. Shared by the usage-carrying list feeds (ListByUsage,
// ListByFailed), which select the same projection but order it differently.
const usageLateral = `
	LEFT JOIN LATERAL (
		SELECT
			COALESCE(sum(count) FILTER (WHERE event = 'search_hit'), 0) AS search_hits,
			COALESCE(sum(count) FILTER (WHERE event = 'fetched'), 0)   AS fetches,
			COALESCE(sum(count) FILTER (WHERE event = 'worked'), 0)    AS worked,
			COALESCE(sum(count) FILTER (WHERE event = 'failed'), 0)    AS failed,
			max(last_at) FILTER (WHERE event = 'failed')                AS last_failed_at,
			max(last_at) AS last_used_at
		FROM knowledge_usage
		WHERE knowledge_id = k.id
	) u ON true`

// recentLateral counts the same two events again over the raw rows,
// inside domain.RecentUsageDays, exposing them as r.fetches and r.failed.
// It is what the two usage feeds order by; the lifetime totals beside it
// stay, and break the ties.
//
// A second pass over a second table, rather than a decaying column. The
// counters in knowledge_usage only ever grow, so a windowed number cannot
// be read off them at all — and a stored decay would need something to
// run on a schedule, which is a moving part this product does not have.
// knowledge_event already holds the rows with their timestamps, indexed
// on (knowledge_id, at), which is exactly this range scan.
//
// The cost is bounded by the same pruning: events older than the
// retention are gone, so the table this walks is a bounded window of a
// curated base's activity rather than its history.
const recentLateral = `
	LEFT JOIN LATERAL (
		SELECT
			count(*) FILTER (WHERE event = 'fetched') AS fetches,
			count(*) FILTER (WHERE event = 'failed')  AS failed
		FROM knowledge_event
		WHERE knowledge_id = k.id AND at >= now() - $%d::interval
	) r ON true`

// recentWindow is the lateral's interval argument, in the spelling
// PostgreSQL reads.
func recentWindow() string { return strconv.Itoa(domain.RecentUsageDays) + " days" }

// ListByUsage returns filtered entries ordered by demand, most-read
// first: fetches descending, then oldest-created (created_at ascending)
// as the tiebreak. This is the draft review feed — the promotion queue at
// the top, and never-read drafts (fetches 0) sinking oldest-first to the
// bottom for inventory. Each hit carries its usage totals so the caller
// renders the signal without a per-entry round trip. Score is 0.
//
// Fetches rather than search_hits, which this ordered by until now. A
// search hit is recorded for every id in every result set: it says the
// ranker put the concept in front of somebody, and ordering a queue by
// it makes the ranker's own output the demand it is supposed to measure
// — whatever surfaces keeps surfacing, and a curator reads that back as
// "people want this". A fetch is the body actually delivered into a
// caller's context, by a pack or by name.
//
// Neither is innocent of the ranker — a context pack is chosen by it too
// — but a pack holds five to twenty concepts against a result set's
// fifty, and what it holds was read rather than listed. The number that
// explains the order is already beside it in every rendering, which is
// the other half of the change: search_hits stays, and stops being the
// one the queue is sorted on.
func (s *Store) ListByUsage(ctx context.Context, f Filter, after *After, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, recentWindow())
	recent := fmt.Sprintf(recentLateral, len(args))
	keyset, err := keysetAfter([]orderCol{
		{expr: "r.fetches", cast: "bigint", desc: true},
		{expr: "u.fetches", cast: "bigint", desc: true},
		{expr: "k.created_at", cast: "timestamptz"},
	}, "k.id", after, &args)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		SELECT `+knowledgeSelectK+`,
			u.search_hits, u.fetches, u.worked, u.failed, u.last_used_at,
			r.fetches, r.failed
		FROM object k`+usageLateral+recent+`
		WHERE %s%s
		ORDER BY r.fetches DESC, u.fetches DESC, k.created_at ASC, k.id LIMIT %d`, where, keyset, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUsageHit)
}

// ListByFailed returns filtered entries whose failure reports are still
// unanswered, worst first — the re-verification feed (design doc 0025).
// It is the evidence-based counterpart to the verified_at feed's
// time-based staleness, so a healthy base yields an empty feed.
//
// "Still unanswered" is the difference between a queue and a ledger. The
// totals in knowledge_usage only ever grow, so ranking by them alone left
// an entry in the feed forever: a reviewer who checked it, found it right
// and re-verified it saw it sitting at the top the next morning, and the
// queue could only lengthen. An entry drops out once it is verified after
// its last failure report (Verify, or a promotion from draft) — or once
// it is rejected, which the default status filter already hides.
// Never-verified entries stay: a failing draft has had no ruling at all.
//
// Ordering stays on the lifetime failure count: presence answers "is this
// unresolved", ranking answers "how bad has this been". Ties break on
// fewest corroborating "worked" reports, then verification age (oldest
// first, never-verified last), then id. Each hit carries its usage totals
// so the reviewer sees the worked/failed evidence inline; score is 0.
func (s *Store) ListByFailed(ctx context.Context, f Filter, after *After, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, recentWindow())
	recent := fmt.Sprintf(recentLateral, len(args))
	keyset, err := keysetAfter([]orderCol{
		{expr: "r.failed", cast: "bigint", desc: true},
		{expr: "u.failed", cast: "bigint", desc: true},
		{expr: "u.worked", cast: "bigint"},
		{expr: lastVerifiedAt("k"), cast: "timestamptz", null: true},
	}, "k.id", after, &args)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		SELECT `+knowledgeSelectK+`,
			u.search_hits, u.fetches, u.worked, u.failed, u.last_used_at,
			r.fetches, r.failed
		FROM object k`+usageLateral+recent+`
		WHERE %s AND `+unansweredFailure("k")+`%[4]s
		ORDER BY r.failed DESC, u.failed DESC, u.worked ASC, %[2]s ASC NULLS LAST, k.id LIMIT %[3]d`,
		where, lastVerifiedAt("k"), limit, keyset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUsageHit)
}

// QueueCounts counts the review queues in one pass: how many entries
// each of ListByUsage's draft feed, ListByFailed, ListByStaleAfter and
// the edited listing would return (design docs 0049, 0138). A count and
// a listing want different queries — measuring the feeds by fetching a
// thousand rows from each is not measuring — but they must not want
// different *predicates*: those are the feeds' own (unansweredFailure,
// pastExpiry, and for edited the trust filter's own standingVerification),
// so a queue's depth cannot come to mean something other than the queue.
//
// The filter is the same one the feeds take, so a count is scoped by
// prefix the way the listings are: a team asks about its own subtree.
func (s *Store) QueueCounts(ctx context.Context, f Filter) (domain.QueueCounts, error) {
	where, args := f.buildWhere("k.")
	q := fmt.Sprintf(`
		SELECT
			count(*) FILTER (WHERE k.status = 'draft'),
			count(*) FILTER (WHERE `+unansweredFailure("k")+`),
			count(*) FILTER (WHERE `+pastExpiry("k.")+`),
			count(*) FILTER (WHERE `+anyVerification("k.")+` AND NOT `+standingVerification("k.", "")+`)
		FROM object k`+usageLateral+`
		WHERE %[1]s`, where)
	var c domain.QueueCounts
	err := s.pool.QueryRow(ctx, q, args...).
		Scan(&c.Drafts, &c.Failed, &c.StaleAfter, &c.Edited)
	return c, err
}

// scanUsageHit reads a knowledge row followed by its usage totals (the
// ListByUsage projection). Score stays 0 — this is a listing, not a search.
func scanUsageHit(row pgx.CollectableRow) (domain.SearchHit, error) {
	var h domain.SearchHit
	var k domain.Knowledge
	var u domain.Usage
	dests, finish := knowledgeDest(&k)
	if err := row.Scan(append(dests,
		&u.SearchHits, &u.Fetches, &u.Worked, &u.Failed, &u.LastUsedAt,
		&u.Recent.Fetches, &u.Recent.Failed)...); err != nil {
		return h, err
	}
	if err := finish(); err != nil {
		return h, err
	}
	u.Recent.Days = domain.RecentUsageDays
	h.Summary = domain.SummaryOf(&k)
	h.Usage = &u
	return h, nil
}
