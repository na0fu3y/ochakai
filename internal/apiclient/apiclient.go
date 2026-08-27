// Package apiclient is the REST client behind ochakai's client-side CLI
// commands (design doc 0004). It is a pure client of /api/v1 — the same
// surface as api/openapi.yaml — and resolves Google ID tokens itself, so
// no proxy process is needed to reach an IAM-protected Cloud Run service.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
)

type Client struct {
	base   string // scheme://host, no trailing slash
	http   *http.Client
	tokens oauth2.TokenSource // nil when this client sends no credentials
	auth   string             // human-readable auth path, for Identity
	// noCreds is why tokens is nil on an https server: the credentials
	// could not be resolved. Nil when they were, and on plain http, where
	// sending none is the intent rather than a failure.
	noCreds  error
	producer string // "<producer>/<version>", sent on every request when set
}

// New builds a client for the ochakai server at baseURL. For https URLs a
// Google ID token source is resolved (design doc 0004 §4); plain http is
// for local development and sends no credentials.
//
// Failing to resolve credentials is not an error here. Whether a
// deployment wants an identity is the server's answer, and the client
// cannot read it off the URL: a public demo (design doc 0066 §3) reads no
// identity and never answers 401, so a caller with no Google account is
// exactly who it is for. Refusing to send the request would deny that
// caller on the client's guess. The reason is kept instead and surfaced
// if the server does answer 401 — the deployments that need credentials
// say so themselves, and only they.
func New(ctx context.Context, baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid server URL %q (want http(s)://host)", baseURL)
	}
	c := &Client{base: strings.TrimRight(baseURL, "/"), http: http.DefaultClient, auth: "plain http, no credentials"}
	if u.Scheme == "https" {
		if c.tokens, c.auth, err = resolveTokenSource(ctx, u.Scheme+"://"+u.Host); err != nil {
			c.noCreds, c.auth = err, "no Google credentials found"
		}
	}
	return c, nil
}

// resolveTokenSource is tokenSource, indirected so tests can drive both
// outcomes without depending on the machine's Google credentials.
var resolveTokenSource = tokenSource

// SetProducer declares the software this client runs as, in SPEC §7's
// "<producer>/<version>" form. It rides on every request and the server
// records it beside the authenticated actor (design doc 0052) — it names
// what wrote, never who, so it neither replaces nor weakens the identity
// the ID token carries. Unset by default: a client that has nothing to
// declare declares nothing.
func (c *Client) SetProducer(p string) { c.producer = p }

// TokenSource exposes the resolved Google ID token source so callers can
// build request paths of their own (e.g. `ochakai ui`'s reverse proxy).
// Nil for plain-http development servers and for an https server this
// machine has no credentials for — send no credentials then, and let the
// server say whether it wanted any.
func (c *Client) TokenSource() oauth2.TokenSource { return c.tokens }

// credentials returns the token to attach, or nil with the reason there
// is none. Having none has two shapes and they are one case to a caller:
// this machine never had credentials, so New resolved no source at all;
// or it advertised some it cannot mint — a gcloud session that wants
// reauthentication, no account selected, a prompt in a shell that cannot
// answer one. Neither is decided here, for New's reason: whether a
// deployment wants an identity is the server's answer, so the request
// goes out bare and a 401 carries this back.
func (c *Client) credentials() (*oauth2.Token, error) {
	if c.tokens == nil {
		return nil, c.noCreds
	}
	tok, err := c.tokens.Token()
	if err != nil {
		return nil, fmt.Errorf("resolve Google ID token: %w", err)
	}
	return tok, nil
}

// Identity resolves, locally, the identity this client would present:
// the ID token's email, prefixed the way the server maps actors (design
// doc 0002) — service accounts to process:, everyone else to human:.
// A client sending no credentials — plain http, or an https server this
// machine has none it can mint for — presents human:anonymous, which is
// what a public deployment resolves every caller to (design doc 0066 §3).
// The server's actor resolution is authoritative; this is the client's
// best-effort view.
func (c *Client) Identity() (actor, auth string, err error) {
	tok, noCreds := c.credentials()
	if tok == nil {
		if noCreds != nil && c.tokens != nil {
			// The path named at construction (gcloud, ADC) is not the
			// one this request will take, so it must not be reported as
			// the one it took: nothing is sent, exactly as when there was
			// never a source, and it reads the same here.
			return "human:anonymous", "no Google credentials found", nil
		}
		return "human:anonymous", c.auth, nil
	}
	email := jwtEmail(tok.AccessToken)
	if email == "" {
		return "", c.auth, fmt.Errorf("ID token carries no email claim")
	}
	prefix := domain.ActorHuman + ":"
	if strings.HasSuffix(email, ".gserviceaccount.com") {
		prefix = domain.ActorProcess + ":"
	}
	return prefix + email, c.auth, nil
}

// Health requests /health with credentials attached: one round trip that
// proves the URL resolves, the server answers, and — behind Cloud Run —
// IAM accepts the caller.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.get(ctx, "/health", nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// ReadOnly reports whether the server refuses changes to knowledge
// (design doc 0040). It reads the Ochakai-Read-Only header, which a
// read-only deployment sets on every /api/v1 response; /health is
// outside that surface, so this is its own request. Servers predating
// the header omit it and are reported writable, which is what they were.
func (c *Client) ReadOnly(ctx context.Context) (bool, error) {
	resp, err := c.get(ctx, "/api/v1/bundle/index.md", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.Header.Get("Ochakai-Read-Only") == "true", nil
}

// APIError is a non-2xx response from the server.
type APIError struct {
	StatusCode int
	Message    string
	// Code is the machine-readable condition the server named beside its
	// sentence (design doc 0083), e.g. "already_exists" or "not_deleted"
	// — the two of the three conditions sharing 409 that a status code
	// cannot tell apart. Empty when the response carried no envelope,
	// which is what a rejection ahead of the container looks like.
	Code string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.StatusCode)
}

// SearchParams are the query parameters of GET /api/v1/search.
type SearchParams struct {
	// Query ranks the answer by relevance. Empty is a request rather
	// than a mistake: with no Sort and neither reverse lookup set, the
	// server lists what the filters match in address order — the plain
	// enumeration, which pages like every other listing.
	Query    string
	Types    []string
	Statuses []string
	Tags     []string
	// Sort switches the endpoint from searching to listing:
	// "verified_at" returns entries by verification age, oldest first
	// (the golden-query canary feed); "usage" returns entries by demand,
	// most-searched first (the draft review feed, hits carry usage);
	// "failed" returns entries callers reported wrong, worst first (the
	// re-verification feed, design doc 0025 §6); "stale_after" returns
	// entries whose declared expiry has passed, most overdue first
	// (design doc 0037 §2.1 — the one feed verifying does not empty).
	// The server rejects a Query combined with Sort; listed hits carry
	// score 0.
	Sort string
	// Source narrows to entries citing this exact resource, the reverse
	// of sources[].resource (design doc 0037 §2.3). Composes with Query
	// and with any Sort.
	Source string
	// LinksTo narrows to entries whose body links at this id — "what
	// points at this entry", which had an endpoint of its own until
	// design doc 0046 §3.5 folded it here. Composes with Query and with
	// any Sort; on its own it lists in address order and pages.
	LinksTo string
	// Prefixes narrow to entries addressed under one of these paths
	// (design doc 0041). Repeatable and OR-ed, matched on segment
	// boundaries. Composes with Query and with any Sort.
	Prefixes []string
	// Trust and Rejected ask about the instance ledgers rather than the
	// document (design doc 0043 §§3.2-3.3), independently of Statuses.
	// Both are tri-state: nil leaves the question unasked. Nil Rejected
	// still hides rejected entries — asking for them is opt-in, which is
	// how an agent checks whether a proposal was already turned down.
	Trust []string
	// FM narrows by an OKF frontmatter key, sent as fm.<key>=<value>.
	// Which keys those are is the server's answer (design doc 0047).
	FM       map[string]string
	Rejected *bool
	Limit    int
	// Cursor resumes a listing where the previous page ended: pass back
	// the Cursor of that page, with the same Sort and filters (design doc
	// 0050 §2.1). The server refuses it beside a Query — a search is
	// bounded by Limit, and a ranking has no page to resume from.
	Cursor string
}

// SearchResult is one page of the query surface. Cursor is set only when
// a listing has more entries behind this page; a search never sets it,
// and no total count comes with either (design doc 0050 §2.3).
type SearchResult struct {
	Hits   []domain.SearchHit `json:"hits"`
	Cursor string             `json:"cursor,omitempty"`
	// Degraded says the server ranked this search without the embeddings
	// it has: lexical only, so a concept that matches by meaning and not
	// by word is missing from Hits (design doc 0114). Only a search sets
	// it; a listing has nothing to embed.
	Degraded bool `json:"degraded,omitempty"`
}

func (c *Client) Search(ctx context.Context, p SearchParams) (*SearchResult, error) {
	q := url.Values{}
	if p.Query != "" {
		q.Set("q", p.Query)
	}
	if p.Sort != "" {
		q.Set("sort", p.Sort)
	}
	for _, t := range p.Types {
		q.Add("type", t)
	}
	for _, s := range p.Statuses {
		q.Add("status", s)
	}
	for _, t := range p.Tags {
		q.Add("tag", t)
	}
	if p.Source != "" {
		q.Set("source", p.Source)
	}
	if p.LinksTo != "" {
		q.Set("links_to", p.LinksTo)
	}
	for _, pre := range p.Prefixes {
		q.Add("prefix", pre)
	}
	for _, t := range p.Trust {
		q.Add("trust", t)
	}
	for k, v := range p.FM {
		q.Set("fm."+k, v)
	}
	if p.Rejected != nil {
		q.Set("rejected", strconv.FormatBool(*p.Rejected))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	var out SearchResult
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/search", q, nil, &out)
	return &out, err
}

// Queues returns how much work each review queue is holding — the three
// feeds counted rather than listed (design doc 0049). Prefixes scopes it
// to a subtree, as it scopes a search.
//
// It reads the stats face, which carries the same three numbers from the
// same query: GET /api/v1/queues was a second address over them and is
// gone (design doc 0049 §3.1). The convenience stays here rather than at
// the endpoint, which is where a convenience belongs — `ochakai queues`
// and the web UI's banner both want the three numbers alone, and neither
// is a reason for the server to offer a second way to ask.
func (c *Client) Queues(ctx context.Context, prefixes []string) (domain.QueueCounts, error) {
	st, err := c.Stats(ctx, 0, prefixes)
	if err != nil {
		return domain.QueueCounts{}, err
	}
	return st.Queues, nil
}

// Browse lists one level of the ID hierarchy: the JSON representation of
// that directory's index.md (design docs 0014, 0016, and 0046 §3.7 for
// where it now lives). The subdirectories and entries directly under
// prefix — "" is the root, the top-level segments.
func (c *Client) Browse(ctx context.Context, prefix, cursor string) (*BrowseResult, error) {
	var out BrowseResult
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if err := c.doJSON(ctx, http.MethodGet, reservedPath(prefix, "index.md"), q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Log fetches the generated log.md for a path: the changes under it,
// newest first, as OKF SPEC §9 writes them (design doc 0046 §3.8).
func (c *Client) Log(ctx context.Context, prefix string, limit int) ([]byte, error) {
	resp, err := c.get(ctx, reservedPath(prefix, "log.md"), limitQuery(limit))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Revisions fetches an entry's change history, newest first: ?history on
// the object's own address, which is where the history of an object
// lives (design doc 0046 §3.5). Each revision carries the entry as it
// stood, so a diff between two is a text diff. Works for soft-deleted
// entries too; limit 0 uses the server default.
func (c *Client) Revisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	var out struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	q := limitQuery(limit)
	if q == nil {
		q = url.Values{}
	}
	q.Set("history", "")
	err := c.doJSON(ctx, http.MethodGet, bundlePath(id+".md"), q, nil, &out)
	return out.Revisions, err
}

// reservedPath addresses one of the two generated files under a bundle
// path. The root's are at /api/v1/bundle/index.md and .../log.md.
func reservedPath(prefix, name string) string {
	if prefix == "" {
		return "/api/v1/bundle/" + name
	}
	return escapedPath("/api/v1/bundle/", strings.TrimSuffix(prefix, "/")+"/"+name)
}

// Backlinks fetches live entries whose links point at the given entry —
// the links_to reverse lookup on the search face, which is where the
// question lives now (design doc 0046 §3.5). limit 0 uses the server
// default. The rows come back in address order rather than by recency,
// which is what a listing that can be resumed costs (design doc 0050).
func (c *Client) Backlinks(ctx context.Context, id string, limit int) ([]domain.Summary, error) {
	page, err := c.Search(ctx, SearchParams{LinksTo: id, Limit: limit})
	if err != nil {
		return nil, err
	}
	rows := make([]domain.Summary, len(page.Hits))
	for i, h := range page.Hits {
		rows[i] = h.Summary
	}
	return rows, nil
}

// limitQuery renders an optional limit as query parameters (nil when
// unset, so the server default applies).
func limitQuery(limit int) url.Values {
	if limit <= 0 {
		return nil
	}
	return url.Values{"limit": {strconv.Itoa(limit)}}
}

func (c *Client) Get(ctx context.Context, id string) (*domain.View, error) {
	var k domain.View
	if err := c.doJSON(ctx, http.MethodGet, entryPath(id), nil, nil, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// Put writes doc — an OKF document — to the entry at id, creating it
// when the id is free (PUT /api/v1/bundle/{path}). One call, because a
// document says what the entry should say and nothing about whether it
// already existed (design doc 0043 §3.5).
//
// A non-empty ifMatch is sent as the If-Match precondition: the entry's
// version, as a prior read returned it (the ETag header, or the JSON
// body's content_hash). The write then lands only if the entry still has
// that version; a stale one is a 412 APIError and nothing is written. ""
// means last write wins. onlyIfAbsent sends If-None-Match "*", which
// makes an occupied id a 412 instead of a replacement.
//
// created reports which way it went; changed=false reports the server
// wrote nothing because the document matched the stored content (the
// Ochakai-Unchanged response header). notes carry values the server
// accepted but read differently than they were written — a
// reinterpretation is never silent (design doc 0036 §3.4).
func (c *Client) Put(ctx context.Context, id string, doc []byte, ifMatch string, onlyIfAbsent bool) (out *domain.View, created, changed bool, notes []string, err error) {
	hdr := http.Header{}
	if ifMatch != "" {
		hdr.Set("If-Match", etagValue(ifMatch))
	}
	if onlyIfAbsent {
		hdr.Set("If-None-Match", "*")
	}
	resp, err := c.doRaw(ctx, http.MethodPut, entryPath(id), nil, documentMediaType, hdr, bytes.NewReader(doc))
	if err != nil {
		return nil, false, false, nil, err
	}
	defer resp.Body.Close()
	var k domain.View
	if err := json.NewDecoder(resp.Body).Decode(&k); err != nil {
		return nil, false, false, nil, err
	}
	return &k, resp.StatusCode == http.StatusCreated,
		resp.Header.Get("Ochakai-Unchanged") != "true", resp.Header.Values("Ochakai-Note"), nil
}

// The three answers a plan can give, as the Ochakai-Plan header and the
// response body both spell them (design docs 0074 §5, 0097). The values are
// domain's: one home for the vocabulary, so the client cannot come to
// disagree with the server about a word they exchange.

const (
	PlanCreated   = domain.PlanCreated
	PlanUpdated   = domain.PlanUpdated
	PlanUnchanged = domain.PlanUnchanged
)

// Plan asks what a Put of doc would do, and writes nothing
// (?dry_run=true). The answer is the server's own — the same validation,
// the same read of what is stored, the same notes — so a caller gating a
// sync on it meets the same verdict when it runs the import for real
// (design doc 0061).
//
// plan is one of PlanCreated, PlanUpdated or PlanUnchanged. A server that
// does not know the parameter would have written the document, so an
// answer with no plan in it is an error rather than a clean bill.
func (c *Client) Plan(ctx context.Context, id string, doc []byte) (out *domain.View, plan string, notes []string, err error) {
	q := url.Values{"dry_run": []string{"true"}}
	resp, err := c.doRaw(ctx, http.MethodPut, entryPath(id), q, documentMediaType, nil, bytes.NewReader(doc))
	if err != nil {
		return nil, "", nil, err
	}
	defer resp.Body.Close()
	var k domain.View
	if err := json.NewDecoder(resp.Body).Decode(&k); err != nil {
		return nil, "", nil, err
	}
	plan, err = planOf(resp)
	if err != nil {
		return nil, "", nil, err
	}
	return &k, plan, resp.Header.Values("Ochakai-Note"), nil
}

// PlanBundleFile is Plan for an object of the bundle that is not a
// concept: the same refusals a PutBundleFile would meet, and nothing
// written.
func (c *Client) PlanBundleFile(ctx context.Context, path string, data []byte) (plan string, err error) {
	q := url.Values{"dry_run": []string{"true"}}
	resp, err := c.doRaw(ctx, http.MethodPut, bundlePath(path), q,
		"application/octet-stream", nil, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return planOf(resp)
}

// planOf reads the Ochakai-Plan header, and refuses an answer that has
// none: a server old enough not to know dry_run ignored the parameter and
// wrote, so silence here means the opposite of what it looks like.
func planOf(resp *http.Response) (string, error) {
	switch p := resp.Header.Get("Ochakai-Plan"); p {
	case PlanCreated, PlanUpdated, PlanUnchanged:
		return p, nil
	case "":
		return "", fmt.Errorf("the server answered a dry run with no Ochakai-Plan header: it does not support dry_run, and may have written the document (ochakai 0.16.1 and earlier)")
	default:
		return "", fmt.Errorf("the server answered a dry run with an unknown plan %q", p)
	}
}

// documentMediaType is what an OKF concept document travels as.
const documentMediaType = "text/markdown; charset=utf-8"

// etagValue renders a version as an If-Match value: quoted, the way HTTP
// spells an ETag, unless the caller already passed one (or "*").
func etagValue(v string) string {
	if v == "*" || strings.HasPrefix(v, `"`) {
		return v
	}
	return `"` + v + `"`
}

// Delete removes the object at a bundle path (DELETE
// /api/v1/bundle/{path}): a concept at "<id>.md" — soft-deleted, history
// retained — or a file at its own path. One verb for one address space
// (design doc 0075 §4), so the caller passes the address rather than
// choosing between two methods.
//
// A non-empty ifMatch is sent as If-Match, the same precondition Put
// takes: the delete lands only if the concept still has that version,
// and a stale one fails with a conflict instead of deleting someone
// else's edit (design doc 0064). "" means unconditional, as before.
func (c *Client) Delete(ctx context.Context, path, ifMatch string) error {
	hdr := http.Header{}
	if ifMatch != "" {
		hdr.Set("If-Match", etagValue(ifMatch))
	}
	resp, err := c.doRaw(ctx, http.MethodDelete, bundlePath(path), nil, "", hdr, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// review records one human ruling on the entry (POST
// /api/v1/review/{id}) — the one face that used to be three (design doc
// 0055 §3.1). None of the three rulings edits the entry: the document,
// its lifecycle status and its ETag all stay put, because ruling on
// knowledge and publishing it are different acts (design doc 0043
// §§3.2-3.3).
func (c *Client) review(ctx context.Context, id, ruling, note string) (*domain.View, error) {
	var k domain.View
	body := struct {
		Ruling string `json:"ruling"`
		Note   string `json:"note,omitempty"`
	}{Ruling: ruling, Note: note}
	if err := c.doJSON(ctx, http.MethodPost, escapedPath("/api/v1/review/", id), nil, body, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// Verify appends a verification against the entry as it stands. The first
// confirmation and the tenth re-check are the same call — the re-check is
// what takes an entry out of the review feeds (design doc 0025 §6), and
// Update cannot do it.
func (c *Client) Verify(ctx context.Context, id string) (*domain.View, error) {
	return c.review(ctx, id, "verified", "")
}

// Reject records that the entry was turned down, with an optional reason.
// The entry drops out of searches that did not ask for rejected ones
// (design doc 0043 §3.3).
func (c *Client) Reject(ctx context.Context, id, note string) (*domain.View, error) {
	return c.review(ctx, id, "rejected", note)
}

// WithdrawRejection withdraws a rejection. A 404 when the entry carries none:
// lifting nothing is a mistake worth reporting. Nothing is deleted — the
// verifications stay, and the withdrawal is its own row in the history.
func (c *Client) WithdrawRejection(ctx context.Context, id string) (*domain.View, error) {
	return c.review(ctx, id, "withdrawn", "")
}

// Purge hard-deletes an entry that is already soft-deleted, freeing its
// id (DELETE ...?purge=true). A live entry is a 409: delete it first.
func (c *Client) Purge(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, entryPath(id), url.Values{"purge": {"true"}}, nil, nil)
}

// Move renames the entry at id to newID (POST /api/v1/move). The server
// carries revisions, usage, and files along and rewrites inbound
// references so nothing breaks (design doc 0021).
func (c *Client) Move(ctx context.Context, id, newID string) (*domain.View, error) {
	in := struct {
		From string `json:"from"`
		To   string `json:"to"`
	}{id, newID}
	var moved domain.View
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/move", nil, in, &moved); err != nil {
		return nil, err
	}
	return &moved, nil
}

// File fetches one file's bytes and media type by its bundle path
// (GET /api/v1/bundle/{path}). Metadata travels with the entry that
// shows it (Get → Knowledge.Files), each carrying its path.
func (c *Client) File(ctx context.Context, path string) (data []byte, mediaType string, err error) {
	resp, err := c.get(ctx, bundlePath(path), nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func bundlePath(p string) string {
	return escapedPath("/api/v1/bundle/", p)
}

// Usage fetches usage totals for one entry (GET /api/v1/usage/{id}):
// search hits, fetches, outcome reports, and last-used time.
func (c *Client) Usage(ctx context.Context, id string) (*domain.Usage, error) {
	var u domain.Usage
	if err := c.doJSON(ctx, http.MethodGet, escapedPath("/api/v1/usage/", id), nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ReportOutcome reports whether acting on an entry worked or failed
// (POST /api/v1/usage/{id}) and returns the updated totals.
func (c *Client) ReportOutcome(ctx context.Context, id, outcome, note string) (*domain.Usage, error) {
	body := map[string]string{"outcome": outcome}
	if note != "" {
		body["note"] = note
	}
	var u domain.Usage
	if err := c.doJSON(ctx, http.MethodPost, escapedPath("/api/v1/usage/", id), nil, body, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Stats fetches the instance's view of the improvement loop
// (GET /api/v1/stats): what the base is made of, what review did in the
// last days days, what callers reported, and what they searched for and
// did not find. days 0 leaves the server's default (30), and prefixes
// scopes every number keyed by an entry to those subtrees — the misses
// excepted, which belong to no subtree (design doc 0051 §3.7).
func (c *Client) Stats(ctx context.Context, days int, prefixes []string) (*domain.Stats, error) {
	q := url.Values{}
	if days != 0 {
		q.Set("days", strconv.Itoa(days))
	}
	for _, p := range prefixes {
		q.Add("prefix", p)
	}
	var st domain.Stats
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/stats", q, nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Export streams the knowledge base as an OKF tar.gz bundle. The caller
// must close the reader.
//
// It is a read of the bundle root asking for the archive representation
// (design doc 0046 §3.5): an archive is the bundle at a path, so it is
// answered at the address of that path rather than at an endpoint named
// after downloading.
// A non-empty prefix takes the archive of that subtree instead of the
// whole base — the same address a read of it uses, asked for as an
// archive (design doc 0127).
func (c *Client) Export(ctx context.Context, files bool, prefix string) (io.ReadCloser, error) {
	var q url.Values
	if !files {
		q = url.Values{"files": {"false"}}
	}
	path := "/api/v1/bundle/" + strings.TrimSuffix(strings.TrimPrefix(prefix, "/"), "/")
	resp, err := c.doAccept(ctx, http.MethodGet, path, q, nil, "application/gzip")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ReembedResult mirrors the /api/v1/reembed response.
type ReembedResult struct {
	Concepts int    `json:"concepts"`
	Files    int    `json:"files"`
	Failed   int    `json:"failed"`
	Missing  int    `json:"missing"`
	Cursor   string `json:"cursor,omitempty"`
}

// Reembed fills in vectors for entries and files that have none
// for the configured model (POST /api/v1/reembed). limit 0 uses the
// server default. One pass is bounded: Missing reports what is still
// left and Cursor where to resume, and the caller repeats (see
// cmdReembed) rather than asking for a pass that cannot finish inside a
// request timeout. Pass the previous Cursor back to advance; passing ""
// starts from the beginning.
func (c *Client) Reembed(ctx context.Context, cursor string, limit int) (*ReembedResult, error) {
	q := limitQuery(limit)
	if cursor != "" {
		if q == nil {
			q = url.Values{}
		}
		q.Set("cursor", cursor)
	}
	var out ReembedResult
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/reembed", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// entryPath is where a concept lives in the bundle: its id plus `.md`,
// which is its only address (design doc 0046 §3.5). Each ID segment is
// escaped separately — the id is a path ("metrics/revenue") and its
// slashes must stay real path separators.
func entryPath(id string) string { return escapedPath("/api/v1/bundle/", id) + ".md" }

func escapedPath(base, id string) string {
	var b strings.Builder
	b.WriteString(base)
	for i, seg := range strings.Split(id, "/") {
		if i > 0 {
			b.WriteString("/")
		}
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

// get is a body-less read that takes whatever representation the path
// serves by default. The reads that want JSON specifically go through
// doJSON, which asks for it (design doc 0046 §§3.7-3.8).
func (c *Client) get(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	return c.doRaw(ctx, http.MethodGet, path, query, "", nil, nil)
}

// doAccept is do with an Accept header, for the paths whose default
// representation is a file rather than JSON (design doc 0046 §§3.7-3.8).
func (c *Client) doAccept(ctx context.Context, method, path string, query url.Values, body any, accept string) (*http.Response, error) {
	var rd io.Reader
	contentType := ""
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(buf)
		contentType = "application/json"
	}
	return c.doRaw(ctx, method, path, query, contentType, http.Header{"Accept": []string{accept}}, rd)
}

// doRaw is do without the JSON encoding: the body is sent verbatim
// (nil rd and empty contentType for body-less requests); extra headers,
// if any, are copied onto the request.
func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, contentType string, extra http.Header, rd io.Reader) (*http.Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	maps.Copy(req.Header, extra)
	if c.producer != "" {
		req.Header.Set(httpauth.ProducerHeader, c.producer)
	}
	tok, noCreds := c.credentials()
	if tok != nil {
		tok.SetAuthHeader(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var msg struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if json.Unmarshal(data, &msg) == nil {
			apiErr.Message, apiErr.Code = msg.Error, msg.Code
		}
		// This client sent nothing because it had nothing, and this
		// server — unlike a public one, which never answers 401 (design
		// doc 0066 §3) — wanted an identity. Say why the request went out
		// bare: Cloud Run rejects ahead of the container, so the body is
		// Google's HTML and APIError alone would say only "Unauthorized".
		if resp.StatusCode == http.StatusUnauthorized && noCreds != nil {
			return nil, fmt.Errorf("server answered %w: %w", apiErr, noCreds)
		}
		return nil, apiErr
	}
	return resp, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	// Asked for as JSON, not merely decoded as JSON: the reserved paths
	// serve a file unless a caller says otherwise (design doc 0046
	// §§3.7-3.8), and every other endpoint ignores the header.
	resp, err := c.doAccept(ctx, method, path, query, body, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// PutBundleFile writes one object of the bundle at its path
// (PUT /api/v1/bundle/{path}, design doc 0046 §3.5). It is how an import
// keeps what a bundle carried besides its concepts — a file no entry
// references, or a markdown file with no type, which the server stores
// as a file rather than reading as a concept — and how `ochakai put`
// writes a single file, at the one address a file has (design doc 0075
// §1).
//
// The stored file comes back: the media type is sniffed from the bytes
// rather than taken from the caller, so what the object became is the
// server's answer and not the request's.
func (c *Client) PutBundleFile(ctx context.Context, path string, data []byte) (*domain.File, error) {
	resp, err := c.doRaw(ctx, http.MethodPut, bundlePath(path), nil,
		"application/octet-stream", nil, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var f domain.File
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// AccessBody is the access policy document at /api/v1/access (design
// doc 0109): one key, so a rule can grow a field without the body
// changing shape.
type AccessBody struct {
	Rules []domain.AccessRule `json:"rules"`
	// Version is what a conditional replacement echoes in If-Match. It
	// travels in the document so that the policy a client read back is
	// the whole of what it needs to write the policy conditionally;
	// the server ignores it on the way in (design doc 0120).
	Version string `json:"version,omitempty"`
}

// Policy reads the whole access policy and its version. An
// administrator's read: 403 for anyone else, because the rules name who
// may see what.
func (c *Client) Policy(ctx context.Context) ([]domain.AccessRule, string, error) {
	var b AccessBody
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/access", nil, nil, &b); err != nil {
		return nil, "", err
	}
	return b.Rules, b.Version, nil
}

// SetPolicy replaces the whole access policy and returns it as stored,
// with its new version.
//
// A non-empty ifMatch is sent as the If-Match precondition — the version
// a prior read returned — and the replacement lands only while the
// policy still has it; a stale one is a 412 APIError and nothing is
// written. "" means last write wins, as it does for a concept.
func (c *Client) SetPolicy(ctx context.Context, rules []domain.AccessRule, ifMatch string) ([]domain.AccessRule, string, error) {
	if rules == nil {
		// An explicit empty policy — the way a deployment turns the
		// boundary off again — must travel as [] and not as null: the
		// server reads the key, and null would be a document that says
		// nothing rather than one that says "no rules".
		rules = []domain.AccessRule{}
	}
	hdr := http.Header{"Accept": []string{"application/json"}}
	if ifMatch != "" {
		hdr.Set("If-Match", etagValue(ifMatch))
	}
	buf, err := json.Marshal(AccessBody{Rules: rules})
	if err != nil {
		return nil, "", err
	}
	resp, err := c.doRaw(ctx, http.MethodPut, "/api/v1/access", nil, "application/json", hdr, bytes.NewReader(buf))
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	var out AccessBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", err
	}
	return out.Rules, out.Version, nil
}
