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
// doc 0002) — service accounts to agent:, everyone else to human:.
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
	prefix := "human:"
	if strings.HasSuffix(email, ".gserviceaccount.com") {
		prefix = "agent:"
	}
	return prefix + email, c.auth, nil
}

// Health requests /health with credentials attached: one round trip that
// proves the URL resolves, the server answers, and — behind Cloud Run —
// IAM accepts the caller.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// APIError is a non-2xx response from the server. A 422 from compile
// means the request was understood and refused (outside the supported
// subset) — the CLI maps it to exit code 2.
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
	// (the golden-query canary feed). Query is ignored then, and the
	// returned hits carry no score.
	Sort  string
	Limit int
}

func (c *Client) Search(ctx context.Context, p SearchParams) ([]domain.SearchHit, error) {
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
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	var out struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/knowledge", q, nil, &out)
	return out.Hits, err
}

// ContextResult mirrors the /api/v1/context response: ranked hits plus
// the full entries behind the top ones, expanded one hop through links.
type ContextResult struct {
	Hits    []domain.SearchHit `json:"hits"`
	Entries []domain.Knowledge `json:"entries"`
}

func (c *Client) Context(ctx context.Context, query string, types, statuses, tags []string, limit int, minScore float64) (*ContextResult, error) {
	q := url.Values{}
	q.Set("q", query)
	if minScore > 0 {
		q.Set("min_score", strconv.FormatFloat(minScore, 'f', -1, 64))
	}
	for _, t := range types {
		q.Add("type", t)
	}
	for _, s := range statuses {
		q.Add("status", s)
	}
	for _, t := range tags {
		q.Add("tag", t)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out ContextResult
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/context", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Get(ctx context.Context, typ, id string) (*domain.Knowledge, error) {
	var k domain.Knowledge
	if err := c.doJSON(ctx, http.MethodGet, entryPath(typ, id), nil, nil, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

func (c *Client) Create(ctx context.Context, k *domain.Knowledge) (*domain.Knowledge, error) {
	var created domain.Knowledge
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/knowledge", nil, k, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// Update replaces the entry at k.Type/k.ID (full replacement; the server
// keeps every change as a revision).
func (c *Client) Update(ctx context.Context, k *domain.Knowledge) (*domain.Knowledge, error) {
	var updated domain.Knowledge
	if err := c.doJSON(ctx, http.MethodPut, entryPath(string(k.Type), k.ID), nil, k, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (c *Client) Delete(ctx context.Context, typ, id string) error {
	return c.doJSON(ctx, http.MethodDelete, entryPath(typ, id), nil, nil, nil)
}

// Usage fetches usage totals for one entry (GET /api/v1/usage/{type}/{id}):
// search hits, fetches, compile references, and last-used time.
func (c *Client) Usage(ctx context.Context, typ, id string) (*domain.Usage, error) {
	var u domain.Usage
	if err := c.doJSON(ctx, http.MethodGet, escapedPath("/api/v1/usage/", typ, id), nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) Compile(ctx context.Context, req CompileRequest) (*CompileResult, error) {
	var res CompileResult
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/compile", nil, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Export streams the knowledge base as an OKF tar.gz bundle. The caller
// must close the reader.
func (c *Client) Export(ctx context.Context) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/export", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ImportOssie uploads Apache Ossie semantic model YAML verbatim; the
// server stores each model for compile and derives metric/table
// knowledge entries.
func (c *Client) ImportOssie(ctx context.Context, yamlSrc []byte) (*ImportReport, error) {
	resp, err := c.doRaw(ctx, http.MethodPost, "/api/v1/import/ossie", nil,
		"application/yaml", bytes.NewReader(yamlSrc))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var report ImportReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, err
	}
	return &report, nil
}

// entryPath escapes each ID segment separately: IDs are hierarchical
// ("sales/orders") and their slashes must stay real path separators.
func entryPath(typ, id string) string { return escapedPath("/api/v1/knowledge/", typ, id) }

func escapedPath(base, typ, id string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(url.PathEscape(typ))
	for seg := range strings.SplitSeq(id, "/") {
		b.WriteString("/")
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
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
	return c.doRaw(ctx, method, path, query, contentType, rd)
}

// doRaw is do without the JSON encoding: the body is sent verbatim
// (nil rd and empty contentType for body-less requests).
func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, contentType string, rd io.Reader) (*http.Response, error) {
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
	resp, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
