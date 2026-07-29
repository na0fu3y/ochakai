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
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/domain"
)

type Client struct {
	base   string // scheme://host, no trailing slash
	http   *http.Client
	tokens oauth2.TokenSource // nil for plain-http development servers
	auth   string             // human-readable auth path, for Identity
}

// New builds a client for the ochakai server at baseURL. For https URLs a
// Google ID token source is resolved (design doc 0004 §4); plain http is
// for local development and sends no credentials.
func New(ctx context.Context, baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid server URL %q (want http(s)://host)", baseURL)
	}
	c := &Client{base: strings.TrimRight(baseURL, "/"), http: http.DefaultClient, auth: "plain http, no credentials"}
	if u.Scheme == "https" {
		if c.tokens, c.auth, err = tokenSource(ctx, u.Scheme+"://"+u.Host); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// TokenSource exposes the resolved Google ID token source so callers can
// build request paths of their own (e.g. `ochakai ui`'s reverse proxy).
// Nil for plain-http development servers — send no credentials then.
func (c *Client) TokenSource() oauth2.TokenSource { return c.tokens }

// Identity resolves, locally, the identity this client would present:
// the ID token's email, prefixed the way the server maps actors (design
// doc 0002) — service accounts to process:, everyone else to human:.
// Plain-http development servers see human:anonymous. The server's actor
// resolution is authoritative; this is the client's best-effort view.
func (c *Client) Identity() (actor, auth string, err error) {
	if c.tokens == nil {
		return "human:anonymous", c.auth, nil
	}
	tok, err := c.tokens.Token()
	if err != nil {
		return "", c.auth, fmt.Errorf("resolve Google ID token: %w", err)
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
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.StatusCode)
}

// SearchParams are the query parameters of GET /api/v1/knowledge.
type SearchParams struct {
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
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/knowledge", q, nil, &out)
	return &out, err
}

// Queues returns how much work each review queue is holding — the three
// feeds counted rather than listed (design doc 0049). Prefixes scopes it
// to a subtree, as it scopes a search.
func (c *Client) Queues(ctx context.Context, prefixes []string) (domain.QueueCounts, error) {
	q := url.Values{}
	for _, p := range prefixes {
		q.Add("prefix", p)
	}
	var out struct {
		Queues domain.QueueCounts `json:"queues"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/queues", q, nil, &out)
	return out.Queues, err
}

// ContextResult mirrors the /api/v1/context response: the ranking plus
// the full entries behind the top hits, expanded one hop through links.
// Hits carries the ranking only — the knowledge travels once, in Entries
// (design doc 0033) — so a hit with no matching entry is a pointer to
// fetch with Get. Outline lists entries the server's budget dropped; it
// stays empty unless the caller passes a budget.
type ContextResult struct {
	Hits      []domain.ContextRank    `json:"hits"`
	Entries   []domain.View           `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
}

// ContextParams are the query parameters of GET /api/v1/context. The
// filters mean what they mean on SearchParams; Prefixes scopes the search
// that picks the hits and not the link expansion, so an entry in scope
// still arrives with the companions it cites (design doc 0041 §2.6).
type ContextParams struct {
	Query    string
	Types    []string
	Statuses []string
	Tags     []string
	Prefixes []string
	// Trust narrows by who confirmed the entry, as on
	// SearchParams. Rejected has no counterpart here: a context pack never
	// carries knowledge somebody turned down.
	Trust []string
	// FM narrows by an OKF frontmatter key, sent as fm.<key>=<value>.
	// Which keys those are is the server's answer (design doc 0047).
	FM       map[string]string
	Limit    int
	MinScore float64
	// Budget, when positive, caps the response bytes server-side: entries
	// that do not fit come back as Outline instead of being dropped
	// silently. 0 asks for everything, which is what the rendered CLI
	// output wants — it caps at render time, where it can say what it
	// left out.
	Budget int
}

func (c *Client) Context(ctx context.Context, p ContextParams) (*ContextResult, error) {
	q := url.Values{}
	q.Set("q", p.Query)
	if p.MinScore > 0 {
		q.Set("min_score", strconv.FormatFloat(p.MinScore, 'f', -1, 64))
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
	for _, pre := range p.Prefixes {
		q.Add("prefix", pre)
	}
	for _, t := range p.Trust {
		q.Add("trust", t)
	}
	for k, v := range p.FM {
		q.Set("fm."+k, v)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Budget > 0 {
		q.Set("budget", strconv.Itoa(p.Budget))
	}
	var out ContextResult
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/context", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Browse lists one level of the ID hierarchy: the JSON representation of
// that directory's index.md (design docs 0014, 0016, and 0046 §3.7 for
// where it now lives). The subdirectories and entries directly under
// prefix — "" is the root, the top-level segments.
func (c *Client) Browse(ctx context.Context, prefix string) (*BrowseResult, error) {
	var out BrowseResult
	if err := c.doJSON(ctx, http.MethodGet, reservedPath(prefix, "index.md"), nil, nil, &out); err != nil {
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

// Revisions fetches an entry's change history, newest first — the JSON
// representation of its directory's log.md, narrowed to the entry
// (design doc 0046 §3.8). Works for soft-deleted entries too; limit 0
// uses the server default.
func (c *Client) Revisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	var out struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	err := c.doJSON(ctx, http.MethodGet, reservedPath(id, "log.md"), limitQuery(limit), nil, &out)
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

// Backlinks fetches live entries whose links point at the given entry,
// most recently updated first (GET /api/v1/backlinks/{id}).
// limit 0 uses the server default.
func (c *Client) Backlinks(ctx context.Context, id string, limit int) ([]domain.Summary, error) {
	var out struct {
		Entries []domain.Summary `json:"entries"`
	}
	err := c.doJSON(ctx, http.MethodGet, escapedPath("/api/v1/backlinks/", id), limitQuery(limit), nil, &out)
	return out.Entries, err
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
// when the id is free (PUT /api/v1/knowledge/{id}). One call, because a
// document says what the entry should say and nothing about whether it
// already existed (design doc 0043 §3.5).
//
// A non-empty ifMatch is sent as the If-Match precondition: the entry's
// version, as a prior read returned it (the ETag header, or the JSON
// body's content_hash). The write then lands only if the entry still has
// that version; a stale one is a 412 APIError and nothing is written. ""
// means last write wins. onlyIfAbsent sends If-None-Match "*", which
// makes an occupied id a 409 instead of a replacement.
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

func (c *Client) Delete(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, entryPath(id), nil, nil, nil)
}

// Verify appends a verification against the entry as it stands (POST
// /api/v1/verify/{id}). The first confirmation and the tenth re-check are
// the same call — the re-check is what takes an entry out of the review
// feeds (design doc 0025 §6), and Update cannot do it. It does not touch
// the document, so the entry's status and ETag do not move.
func (c *Client) Verify(ctx context.Context, id string) (*domain.View, error) {
	var k domain.View
	if err := c.doJSON(ctx, http.MethodPost, escapedPath("/api/v1/verify/", id), nil, nil, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// Reject records that the entry was turned down, with an optional reason
// (POST /api/v1/reject/{id}). Like Verify it is a ruling, not an edit:
// the document keeps its lifecycle status, and the entry drops out of
// searches that did not ask for rejected ones (design doc 0043 §3.3).
func (c *Client) Reject(ctx context.Context, id, note string) (*domain.View, error) {
	var k domain.View
	body := struct {
		Note string `json:"note"`
	}{Note: note}
	if err := c.doJSON(ctx, http.MethodPost, escapedPath("/api/v1/reject/", id), nil, body, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// LiftRejection withdraws a ruling (DELETE /api/v1/reject/{id}). A 404
// when the entry carries no rejection.
func (c *Client) LiftRejection(ctx context.Context, id string) (*domain.View, error) {
	var k domain.View
	if err := c.doJSON(ctx, http.MethodDelete, escapedPath("/api/v1/reject/", id), nil, nil, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// Purge hard-deletes an entry that is already soft-deleted, freeing its
// id (DELETE ...?purge=true). A live entry is a 409: delete it first.
func (c *Client) Purge(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, entryPath(id), url.Values{"purge": {"true"}}, nil, nil)
}

// Move renames the entry at id to newID (POST /api/v1/move). The server
// carries revisions, usage, and attachments along and rewrites inbound
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

// Attach uploads data as an attachment of the entry (PUT
// /api/v1/attachments/{id}/{name}), replacing any attachment of
// the same name. The file lands at <id>/<name>; one that lives elsewhere
// in the bundle is written with PutBundleFile, at the path it lives at.
func (c *Client) Attach(ctx context.Context, id, name string, data []byte) (*domain.Attachment, error) {
	resp, err := c.doRaw(ctx, http.MethodPut, attachmentPath(id, name), nil,
		"application/octet-stream", nil, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var att domain.Attachment
	if err := json.NewDecoder(resp.Body).Decode(&att); err != nil {
		return nil, err
	}
	return &att, nil
}

// Attachment fetches one attachment's bytes and media type (GET
// /api/v1/attachments/{id}/{name}). Full metadata travels with
// the entry (Get → Knowledge.Attachments).
func (c *Client) Attachment(ctx context.Context, id, name string) (data []byte, mediaType string, err error) {
	resp, err := c.get(ctx, attachmentPath(id, name), nil)
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

// Detach removes an attachment (DELETE /api/v1/attachments/{id}/{name}).
func (c *Client) Detach(ctx context.Context, id, name string) error {
	return c.doJSON(ctx, http.MethodDelete, attachmentPath(id, name), nil, nil, nil)
}

func attachmentPath(id, name string) string {
	return escapedPath("/api/v1/attachments/", id) + "/" + url.PathEscape(name)
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
// did not find. days 0 leaves the server's default (30).
func (c *Client) Stats(ctx context.Context, days int) (*domain.Stats, error) {
	var q url.Values
	if days != 0 {
		q = url.Values{"days": {strconv.Itoa(days)}}
	}
	var st domain.Stats
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/stats", q, nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Export streams the knowledge base as an OKF tar.gz bundle. The caller
// must close the reader.
func (c *Client) Export(ctx context.Context, attachments bool) (io.ReadCloser, error) {
	var q url.Values
	if !attachments {
		q = url.Values{"attachments": {"false"}}
	}
	resp, err := c.get(ctx, "/api/v1/export", q)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ReembedResult mirrors the /api/v1/reembed response.
type ReembedResult struct {
	Embedded    int    `json:"embedded"`
	Attachments int    `json:"attachments"`
	Failed      int    `json:"failed"`
	Missing     int    `json:"missing"`
	Cursor      string `json:"cursor,omitempty"`
}

// Reembed fills in vectors for entries and attachments that have none
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

// entryPath escapes each ID segment separately: the id is a path
// ("metric/revenue") and its slashes must stay real path separators.
func entryPath(id string) string { return escapedPath("/api/v1/knowledge/", id) }

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
	for name, values := range extra {
		req.Header[name] = values
	}
	if c.tokens != nil {
		tok, err := c.tokens.Token()
		if err != nil {
			return nil, fmt.Errorf("resolve Google ID token: %w", err)
		}
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
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if json.Unmarshal(data, &msg) == nil {
			apiErr.Message = msg.Error
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
// keeps what a bundle carried besides its concepts: a file no entry
// references, or a markdown file with no type, which the server stores
// as a file rather than reading as a concept.
func (c *Client) PutBundleFile(ctx context.Context, path string, data []byte) error {
	resp, err := c.doRaw(ctx, http.MethodPut, "/api/v1/bundle/"+path, nil,
		"application/octet-stream", nil, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
