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
}

// New builds a client for the ochakai server at baseURL. For https URLs a
// Google ID token source is resolved (design doc 0004 §4); plain http is
// for local development and sends no credentials.
func New(ctx context.Context, baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid server URL %q (want http(s)://host)", baseURL)
	}
	c := &Client{base: strings.TrimRight(baseURL, "/"), http: http.DefaultClient}
	if u.Scheme == "https" {
		if c.tokens, err = tokenSource(ctx, u.Scheme+"://"+u.Host); err != nil {
			return nil, err
		}
	}
	return c, nil
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

func (c *Client) Search(ctx context.Context, query string, types, statuses, tags []string, limit int) ([]domain.SearchHit, error) {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
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
	var out struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/knowledge", q, nil, &out)
	return out.Hits, err
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

func entryPath(typ, id string) string {
	return "/api/v1/knowledge/" + url.PathEscape(typ) + "/" + url.PathEscape(id)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(buf)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
