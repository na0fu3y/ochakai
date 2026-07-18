// Package restapi serves /api/v1 so users can build their own web UIs
// (a sample lives in examples/webui). It is a superset of the MCP tools:
// the same knowledge/search/usage/compile operations plus bulk endpoints
// (export, import/ossie) that make no sense as agent tool calls. The
// spec is committed at api/openapi.yaml.
package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/importer"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

func Handler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	// GET /api/v1/knowledge?q=...&type=...&status=...&tag=...&limit=...
	// With sort=verified_at, lists by verification age (oldest first)
	// instead of searching — the feed for golden query canary runs.
	mux.HandleFunc("GET /api/v1/knowledge", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, err := queryInt(q, "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		f := store.Filter{
			Types:    toTypes(q["type"]),
			Statuses: toStatuses(q["status"]),
			Tags:     q["tag"],
		}
		if sort := q.Get("sort"); sort != "" {
			if sort != "verified_at" && sort != "usage" {
				writeError(w, service.Invalidf("invalid sort (valid: verified_at, usage)"))
				return
			}
			if q.Get("q") != "" {
				writeError(w, service.Invalidf("sort=%s lists entries; it cannot be combined with a search query (q)", sort))
				return
			}
			var hits []domain.SearchHit
			if sort == "usage" {
				hits, err = svc.ListByUsage(r.Context(), f, limit)
			} else {
				hits, err = svc.ListByVerifiedAt(r.Context(), f, limit)
			}
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
			return
		}
		hits, err := svc.Search(r.Context(), q.Get("q"), f, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})

	// GET /api/v1/context?q=...&type=...&status=...&tag=...&limit=...
	// The one-call read before answering a data question: full entries
	// behind the top hits, expanded one hop through links.
	mux.HandleFunc("GET /api/v1/context", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, err := queryInt(q, "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		minScore, err := queryFloat(q, "min_score")
		if err != nil {
			writeError(w, err)
			return
		}
		res, err := svc.Context(r.Context(), q.Get("q"), store.Filter{
			Types:    toTypes(q["type"]),
			Statuses: toStatuses(q["status"]),
			Tags:     q["tag"],
		}, limit, minScore)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("POST /api/v1/knowledge", func(w http.ResponseWriter, r *http.Request) {
		var k domain.Knowledge
		if !readJSON(w, r, &k) {
			return
		}
		created, err := svc.Create(r.Context(), &k, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	})

	// {id...} because IDs are hierarchical ("sales/orders", design doc
	// 0005) — the wildcard captures the remaining path segments.
	mux.HandleFunc("GET /api/v1/knowledge/{type}/{id...}", func(w http.ResponseWriter, r *http.Request) {
		k, err := svc.Get(r.Context(), domain.Type(r.PathValue("type")), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, k)
	})

	mux.HandleFunc("PUT /api/v1/knowledge/{type}/{id...}", func(w http.ResponseWriter, r *http.Request) {
		var k domain.Knowledge
		if !readJSON(w, r, &k) {
			return
		}
		k.Type = domain.Type(r.PathValue("type"))
		k.ID = r.PathValue("id")
		updated, err := svc.Update(r.Context(), &k, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})

	// GET /api/v1/usage/{type}/{id...} — how often the entry was actually
	// used (search hits, fetches, compiles). The measure of the write-back
	// loop: draft promotion evidence, staleness signal. Lives outside
	// /knowledge/ so a "/usage" suffix can never be confused with an ID
	// segment.
	usage := func(w http.ResponseWriter, r *http.Request) {
		u, err := svc.Usage(r.Context(), domain.Type(r.PathValue("type")), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
	mux.HandleFunc("GET /api/v1/usage/{type}/{id...}", usage)
	// Legacy pre-0005 usage path, kept for existing clients. It shadows
	// GET on entries whose two-segment ID ends in "usage" — the canonical
	// path above has no such ambiguity.
	mux.HandleFunc("GET /api/v1/knowledge/{type}/{id}/usage", usage)

	// Attachments (design doc 0008): images attached to an entry, bytes
	// fetched on demand — entry reads carry metadata only. The path is
	// /attachments/{type}/{id segments...}/{name}; the final segment is
	// always the filename (attachment names are single segments), so the
	// wildcard split is unambiguous. Lives outside /knowledge/ for the
	// same reason /usage does: a suffix after a hierarchical {id...}
	// would be unroutable.
	attachmentRef := func(w http.ResponseWriter, r *http.Request) (domain.Type, string, string, bool) {
		id, name, ok := splitAttachmentPath(r.PathValue("path"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment path (want /api/v1/attachments/{type}/{id}/{name})"})
			return "", "", "", false
		}
		return domain.Type(r.PathValue("type")), id, name, true
	}

	mux.HandleFunc("PUT /api/v1/attachments/{type}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		typ, id, name, ok := attachmentRef(w, r)
		if !ok {
			return
		}
		data, ok := readBody(w, r, domain.MaxAttachmentSize,
			fmt.Sprintf("attachment exceeds %d MiB", domain.MaxAttachmentSize>>20))
		if !ok {
			return
		}
		// okf_path preserves the bundle location a foreign import carried
		// this file at, so re-export keeps the original body links working.
		okfPath := r.URL.Query().Get("okf_path")
		if okfPath != "" && (okfPath != path.Clean(okfPath) || okfPath == "." ||
			strings.HasPrefix(okfPath, "/") || strings.HasPrefix(okfPath, "..")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid okf_path (want a clean bundle-relative path)"})
			return
		}
		att, err := svc.Attach(r.Context(), typ, id, name, okfPath, data, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, att)
	})

	mux.HandleFunc("GET /api/v1/attachments/{type}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		typ, id, name, ok := attachmentRef(w, r)
		if !ok {
			return
		}
		att, data, err := svc.Attachment(r.Context(), typ, id, name)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", att.MediaType)
		w.Header().Set("ETag", `"`+att.SHA256+`"`)
		w.Header().Set("Content-Disposition", `inline; filename="`+att.Name+`"`)
		_, _ = w.Write(data)
	})

	mux.HandleFunc("DELETE /api/v1/attachments/{type}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		typ, id, name, ok := attachmentRef(w, r)
		if !ok {
			return
		}
		if err := svc.Detach(r.Context(), typ, id, name, httpauth.Actor(r.Context())); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/v1/knowledge/{type}/{id...}", func(w http.ResponseWriter, r *http.Request) {
		err := svc.Delete(r.Context(), domain.Type(r.PathValue("type")), r.PathValue("id"), httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /api/v1/export — the whole knowledge base as an OKF bundle
	// (tar.gz of markdown + YAML frontmatter, plus attached images as
	// plain files). Your knowledge is yours.
	mux.HandleFunc("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		entries, err := svc.Store.ListAll(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		files, err := okf.Bundle(entries)
		if err != nil {
			writeError(w, err)
			return
		}
		// Attachments go next to their entries: "<type>/<id>/<name>", or
		// the foreign path they were imported at (okf_path) so original
		// body links keep working. A foreign path already taken by a
		// concept document falls back to the canonical layout — identical
		// content at the same path (the same image referenced by two
		// entries) is no conflict.
		atts, err := svc.Store.ListAllAttachments(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		for i := range atts {
			a := &atts[i]
			p := okf.AttachmentPath(a.Type, a.ID, &a.Att)
			if _, taken := files[p]; taken {
				p = string(a.Type) + "/" + a.ID + "/" + a.Att.Name
			}
			files[p] = a.Data
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="ochakai-okf.tar.gz"`)
		if err := okf.WriteTarGz(w, files, time.Now()); err != nil {
			// Headers already sent; nothing to do but log via server.
			return
		}
	})

	// POST /api/v1/import/ossie — import an Apache Ossie semantic model.
	// The body is the YAML verbatim; models are stored for compile and
	// metric/table knowledge entries are derived (design doc 0007 moved
	// this from a DB-direct admin command to the API).
	mux.HandleFunc("POST /api/v1/import/ossie", func(w http.ResponseWriter, r *http.Request) {
		src, ok := readBody(w, r, 4<<20, "semantic model exceeds 4 MiB")
		if !ok {
			return
		}
		report, err := importer.ImportOssie(r.Context(), svc, src, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	})

	mux.HandleFunc("POST /api/v1/compile", func(w http.ResponseWriter, r *http.Request) {
		var req service.CompileRequest
		if !readJSON(w, r, &req) {
			return
		}
		res, err := svc.Compile(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// readBody reads at most limit bytes of the request body. Exceeding the
// limit is a 413 with tooLarge as the message; any other read failure is
// a 400 — a client disconnect must not masquerade as a size complaint.
func readBody(w http.ResponseWriter, r *http.Request, limit int64, tooLarge string) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": tooLarge})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		}
		return nil, false
	}
	return data, true
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var compileErr *compiler.Error
	var inputErr *service.InvalidInputError
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrAlreadyExists):
		status = http.StatusConflict
	case errors.As(err, &compileErr):
		status = http.StatusUnprocessableEntity
	case errors.As(err, &inputErr):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// queryInt and queryFloat parse optional numeric query parameters,
// rejecting malformed values instead of silently treating them as unset
// (matching the MCP surface, where the JSON schema enforces types).
func queryInt(q url.Values, name string) (int, error) {
	s := q.Get(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, service.Invalidf("invalid %s %q (want an integer)", name, s)
	}
	return n, nil
}

func queryFloat(q url.Values, name string) (float64, error) {
	s := q.Get(name)
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, service.Invalidf("invalid %s %q (want a number)", name, s)
	}
	return f, nil
}

// splitAttachmentPath cuts "{id...}/{name}" at the final slash: attachment
// names are single segments, so the last segment is always the filename
// and the rest is the (possibly hierarchical) entry ID.
func splitAttachmentPath(p string) (id, name string, ok bool) {
	i := strings.LastIndex(p, "/")
	if i <= 0 || i == len(p)-1 {
		return "", "", false
	}
	return p[:i], p[i+1:], true
}

func toTypes(ss []string) []domain.Type {
	out := make([]domain.Type, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Type(s))
	}
	return out
}

func toStatuses(ss []string) []domain.Status {
	out := make([]domain.Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Status(s))
	}
	return out
}
