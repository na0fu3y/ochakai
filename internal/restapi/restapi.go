// Package restapi serves /api/v1 so users can build their own web UIs
// (a sample lives in examples/webui). It mirrors the MCP tools; the spec
// is committed at api/openapi.yaml.
package restapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		limit, _ := strconv.Atoi(q.Get("limit"))
		f := store.Filter{
			Types:    toTypes(q["type"]),
			Statuses: toStatuses(q["status"]),
			Tags:     q["tag"],
		}
		if sort := q.Get("sort"); sort != "" {
			if sort != "verified_at" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sort (valid: verified_at)"})
				return
			}
			entries, err := svc.ListByVerifiedAt(r.Context(), f, limit)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"hits": entries})
			return
		}
		hits, err := svc.Search(r.Context(), q.Get("q"), f, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
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

	mux.HandleFunc("DELETE /api/v1/knowledge/{type}/{id...}", func(w http.ResponseWriter, r *http.Request) {
		err := svc.Delete(r.Context(), domain.Type(r.PathValue("type")), r.PathValue("id"), httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /api/v1/export — the whole knowledge base as an OKF bundle
	// (tar.gz of markdown + YAML frontmatter). Your knowledge is yours.
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
		src, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
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
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrAlreadyExists):
		status = http.StatusConflict
	case errors.As(err, &compileErr):
		status = http.StatusUnprocessableEntity
	case strings.Contains(err.Error(), "invalid"):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
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
