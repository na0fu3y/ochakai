// Package restapi serves /api/v1 so users can build their own web UIs
// (the bundled one lives in internal/webui). It is a superset of the MCP tools:
// the same knowledge/search/usage operations plus the bulk export
// endpoint that makes no sense as an agent tool call, and human-facing
// endpoints (browse, revisions, backlinks) that stay off MCP by design
// (agents get search/get_context instead; design doc 0015 records the
// per-surface policy). The CLI covers this whole surface; the spec is
// committed at api/openapi.yaml.
package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// exportBatch is how many entries the exporter holds at once. Small
// enough that a knowledge base of any size costs the same peak memory;
// large enough that the round trips disappear against the transfer.
const exportBatch = 100

func Handler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	// GET /api/v1/knowledge?q=...&type=...&status=...&tag=...&source=...&prefix=...&limit=...
	// With sort=verified_at, lists by verification age (oldest first)
	// instead of searching — the feed for canary runs.
	// source narrows to the entries citing one resource — the reverse of
	// sources[].resource (design doc 0037 §2.3) — and composes with a
	// query or with any sort. prefix narrows to the entries addressed
	// under a path, repeatable and OR-ed, for scoping a search to a team's
	// subtree and the shared one at once (design doc 0041).
	mux.HandleFunc("GET /api/v1/knowledge", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, err := queryInt(q, "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		rejected, err := queryTriBool(q, "rejected")
		if err != nil {
			writeError(w, err)
			return
		}
		f := store.Filter{
			Types:       domain.ToTypes(q["type"]),
			Statuses:    domain.ToStatuses(q["status"]),
			Tags:        q["tag"],
			Source:      q.Get("source"),
			Prefixes:    q["prefix"],
			Trust:       domain.ToTrusts(q["trust"]),
			Rejected:    rejected,
			Frontmatter: frontmatterFilter(q),
		}
		hits, err := svc.SearchOrList(r.Context(), q.Get("q"), q.Get("sort"), f, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeHits(w, r, svc, hits)
	})

	// GET /api/v1/bundle/{path...} — the two files OKF reserves, at the
	// paths it reserves them at (design doc 0046 §§3.7-3.8).
	//
	//	.../index.md   one level of the tree, as SPEC §8 lists a directory
	//	.../log.md     the changes under that path, as SPEC §9 logs them
	//
	// Both are derived: ochakai already held a tree to walk and a
	// revision ledger, and served them at addresses of its own invention
	// (/api/v1/browse, /api/v1/revisions/{id}) in shapes of its own
	// invention. The format had names for both all along.
	//
	// Accept decides the representation, not the address: markdown is the
	// file, JSON is the same listing with the markdown left out — for the
	// web UI's tree, which should not have to parse a document to draw a
	// sidebar. A representation is not a second surface.
	//
	// PUT and DELETE are refused (405): a derived file is not one anybody
	// writes. The rest of the bundle joins this address in a later change;
	// today a path that is not one of the two reserved names is a 404.
	mux.HandleFunc("GET /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		dir, base := splitReserved(path)
		switch base {
		case "index.md":
			if wantsJSON(r) {
				res, err := svc.Browse(r.Context(), dir)
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, res)
				return
			}
			doc, err := svc.IndexDocument(r.Context(), dir)
			if err != nil {
				writeError(w, err)
				return
			}
			writeMarkdown(w, doc)
		case "log.md":
			limit, err := queryInt(r.URL.Query(), "limit")
			if err != nil {
				writeError(w, err)
				return
			}
			if wantsJSON(r) {
				rows, err := svc.Revisions(r.Context(), dir, limit)
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"revisions": rows})
				return
			}
			doc, err := svc.LogDocument(r.Context(), dir, limit)
			if err != nil {
				writeError(w, err)
				return
			}
			writeMarkdown(w, doc)
		default:
			// Not yet an address for everything in the bundle: the
			// objects and concepts join it in a later change (design
			// doc 0046 §3.5), and until then this path serves the two
			// files the format reserves.
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("no object at %q: the bundle surface serves index.md and log.md", path),
			})
		}
	})

	for _, m := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(m+" /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "index.md and log.md are generated from the bundle, not stored in it (design doc 0046 §§3.7-3.8)",
			})
		})
	}

	// GET /api/v1/context?q=...&type=...&status=...&tag=...&prefix=...&limit=...&budget=...
	// The one-call read before answering a data question: full entries
	// behind the top hits, expanded one hop through links.
	//
	// prefix scopes the search that picks those hits, not the link
	// expansion: a scoped entry citing a glossary term outside the scope
	// still arrives with the term, which is the whole reason this endpoint
	// follows links (design doc 0041 §2.6).
	//
	// budget defaults to 0 (no cap) here, unlike MCP where it is on by
	// default: a REST caller is a program with a pipe, not an agent with a
	// context window, and `ochakai context` does its own rendering-side
	// capping.
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
		budget, err := queryInt(q, "budget")
		if err != nil {
			writeError(w, err)
			return
		}
		res, err := svc.Context(r.Context(), service.ContextRequest{
			Query: q.Get("q"),
			Filter: store.Filter{
				Types:       domain.ToTypes(q["type"]),
				Statuses:    domain.ToStatuses(q["status"]),
				Tags:        q["tag"],
				Prefixes:    q["prefix"],
				Trust:       domain.ToTrusts(q["trust"]),
				Frontmatter: frontmatterFilter(q),
			},
			Limit: limit, MinScore: minScore, Budget: budget,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// {id...} because the id is the entry's full bundle path (design doc
	// 0016) — the wildcard captures every segment.
	mux.HandleFunc("GET /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		k, err := svc.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		// The ETag is the entry's version: a client can echo it in If-Match
		// on a later PUT to update only if nothing changed meanwhile.
		w.Header().Set("ETag", etagOf(k))
		if wantsDocument(r) {
			writeDocument(w, http.StatusOK, k)
			return
		}
		writeView(w, http.StatusOK, k)
	})

	// PUT /api/v1/knowledge/{id...} — the one way to write knowledge. The
	// body is an OKF document, which is what an entry is (design doc 0043
	// §3.5): the path is the address, the frontmatter and body are what it
	// says, and there is no typed JSON write surface beside it to drift
	// from the format.
	//
	// It is create-or-replace. A PUT states what the entry should say, and
	// whether an entry already held the id is not something the writer of
	// a document is saying anything about — so a separate POST would only
	// make the caller answer a question it does not have to ask. The
	// preconditions are where existence is expressed: If-None-Match "*"
	// means "only if the id is free", If-Match a version means "only if it
	// still says this".
	mux.HandleFunc("PUT /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		k, ok := readDocument(w, r)
		if !ok {
			return
		}
		// The path is the address; the document carries the metadata —
		// type included, always (no fill-in from the stored entry, design
		// doc 0017 §4.5).
		k.ID = r.PathValue("id")
		actor := httpauth.Actor(r.Context())

		// If-Match is an optional optimistic-concurrency precondition: its
		// value is the ETag from a prior read. Absent means last-write-wins
		// (design doc 0030).
		ifMatch := parseIfMatch(r)
		var (
			out              *domain.Knowledge
			created, changed bool
			err              error
		)
		if onlyIfAbsent(r) {
			out, err = svc.Create(r.Context(), k, actor)
			created, changed = true, true
		} else {
			out, created, changed, err = svc.Put(r.Context(), k, actor, ifMatch)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		// A payload identical to the stored content wrote nothing (no
		// revision, no version bump); the header lets clients report
		// "unchanged" without an extra read.
		if !changed {
			w.Header().Set("Ochakai-Unchanged", "true")
		}
		w.Header().Set("ETag", etagOf(out))
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		if wantsDocument(r) {
			writeDocument(w, status, out)
			return
		}
		writeView(w, status, out)
	})

	// POST /api/v1/verify/{id...} — record a verification against the
	// entry as it stands. Promoting a draft and re-affirming an entry that
	// was already verified are the same act, and this is the only way to
	// express the second one: PUT carries the stored verified_at over and
	// writes nothing at all when the content is unchanged, which left both
	// review feeds without an exit (design doc 0025 §6). Lives outside
	// /knowledge/ for the same reason /usage does — a "/verify" suffix
	// would be indistinguishable from an ID segment.
	mux.HandleFunc("POST /api/v1/verify/{id...}", func(w http.ResponseWriter, r *http.Request) {
		k, err := svc.Verify(r.Context(), r.PathValue("id"), httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("ETag", etagOf(k))
		writeView(w, http.StatusOK, k)
	})

	// POST /api/v1/reject/{id...} — record that a reviewer turned the
	// entry down, with the reason in the body. A ruling rather than an
	// edit: the document keeps its lifecycle status, so this is not a PUT
	// (design doc 0043 §3.3). DELETE lifts it. Lives outside /knowledge/
	// for the same reason /verify and /usage do — a "/reject" suffix would
	// be indistinguishable from an ID segment.
	mux.HandleFunc("POST /api/v1/reject/{id...}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Note string `json:"note"`
		}
		// A bare POST with no body is a rejection with no reason given.
		if r.ContentLength != 0 && !readJSON(w, r, &body) {
			return
		}
		k, err := svc.Reject(r.Context(), r.PathValue("id"), body.Note, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("ETag", etagOf(k))
		writeView(w, http.StatusOK, k)
	})

	mux.HandleFunc("DELETE /api/v1/reject/{id...}", func(w http.ResponseWriter, r *http.Request) {
		k, err := svc.LiftRejection(r.Context(), r.PathValue("id"), httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("ETag", etagOf(k))
		writeView(w, http.StatusOK, k)
	})

	// GET /api/v1/usage/{id...} — how often the entry was actually used
	// (search hits, fetches, outcomes). The measure of the write-back
	// loop: draft promotion evidence, staleness signal. Lives outside
	// /knowledge/ so a "/usage" suffix can never be confused with an ID
	// segment.
	mux.HandleFunc("GET /api/v1/usage/{id...}", func(w http.ResponseWriter, r *http.Request) {
		u, err := svc.Usage(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	})
	// POST reports an outcome (worked/failed) against the entry — the
	// write half of the same usage resource, so no new API surface is
	// added (issue #41). Responds with the updated totals.
	mux.HandleFunc("POST /api/v1/usage/{id...}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Outcome string `json:"outcome"`
			Note    string `json:"note"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		u, err := svc.ReportOutcome(r.Context(), r.PathValue("id"), in.Outcome, in.Note)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	})

	// GET /api/v1/backlinks/{id...} — live entries whose links point at
	// this entry, most recently updated first. The reverse edge
	// get_context follows when packing companions, exposed so UIs can
	// show "linked from" next to an entry's own links.
	mux.HandleFunc("GET /api/v1/backlinks/{id...}", func(w http.ResponseWriter, r *http.Request) {
		limit, err := queryInt(r.URL.Query(), "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		entries, err := svc.Backlinks(r.Context(), r.PathValue("id"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		// A backlink row names an entry; it does not hand it over. The
		// projection is what a caller needs to decide whether to follow
		// the edge (design doc 0043 §3.5).
		rows := make([]domain.Summary, len(entries))
		for i := range entries {
			rows[i] = domain.SummaryOf(&entries[i])
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": rows})
	})
	// Attachments (design docs 0008, 0013): files attached to an entry
	// (images, PDFs, plain text), bytes fetched on demand — entry reads
	// carry metadata only. The path is
	// /attachments/{id segments...}/{name}; the final segment is always
	// the filename (attachment names are single segments), so the
	// wildcard split is unambiguous. Lives outside /knowledge/ for the
	// same reason /usage does: a suffix after a hierarchical {id...}
	// would be unroutable.
	attachmentRef := func(w http.ResponseWriter, r *http.Request) (string, string, bool) {
		id, name, ok := splitAttachmentPath(r.PathValue("path"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment path (want /api/v1/attachments/{id}/{name})"})
			return "", "", false
		}
		return id, name, true
	}

	mux.HandleFunc("PUT /api/v1/attachments/{path...}", func(w http.ResponseWriter, r *http.Request) {
		id, name, ok := attachmentRef(w, r)
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
		att, err := svc.Attach(r.Context(), id, name, okfPath, data, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, att)
	})

	mux.HandleFunc("GET /api/v1/attachments/{path...}", func(w http.ResponseWriter, r *http.Request) {
		id, name, ok := attachmentRef(w, r)
		if !ok {
			return
		}
		// Conditional GET before touching the blob store: bytes are
		// content-addressed, so the hash is a perfect ETag, and the web
		// UI re-renders the same images on every search — a 304 answered
		// from metadata alone keeps GCS reads and egress off the bill.
		// no-cache means "revalidate every time", which is required
		// because the name→content mapping is mutable (replace-by-name).
		meta, err := svc.AttachmentMeta(r.Context(), id, name)
		if err != nil {
			writeError(w, err)
			return
		}
		etag := `"` + meta.SHA256 + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		att, data, err := svc.Attachment(r.Context(), id, name)
		if err != nil {
			writeError(w, err)
			return
		}
		// Re-stamp from the row the bytes came from, in case the
		// attachment was replaced between the two reads.
		w.Header().Set("ETag", `"`+att.SHA256+`"`)
		ct := att.MediaType
		// Plain text without a charset invites browser guessing; the sniffer
		// only passes UTF-8/UTF-16 text through, so declare it.
		if ct == "text/plain" {
			ct = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		// The bytes are user-uploaded and served inline; the media type is
		// sniffed and allowlisted (design doc 0013 — nothing executable, no
		// text/html, no image/svg+xml), but nosniff keeps a browser from
		// overriding that and interpreting them as anything else.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `inline; filename="`+att.Name+`"`)
		_, _ = w.Write(data)
	})

	mux.HandleFunc("DELETE /api/v1/attachments/{path...}", func(w http.ResponseWriter, r *http.Request) {
		id, name, ok := attachmentRef(w, r)
		if !ok {
			return
		}
		if err := svc.Detach(r.Context(), id, name, httpauth.Actor(r.Context())); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// DELETE soft-deletes; ?purge=true hard-deletes an entry that is
	// already soft-deleted, freeing its id (a tombstone still owns the
	// primary key, so a move onto it fails). Two calls, deliberately: the
	// first is reversible, the second is not. Not on MCP — destroying
	// history is a human decision (design doc 0015).
	mux.HandleFunc("DELETE /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		purge, err := queryBool(r.URL.Query(), "purge", false)
		if err != nil {
			writeError(w, err)
			return
		}
		if purge {
			err = svc.Purge(r.Context(), r.PathValue("id"), httpauth.Actor(r.Context()))
		} else {
			err = svc.Delete(r.Context(), r.PathValue("id"), httpauth.Actor(r.Context()))
		}
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /api/v1/move {"from": ..., "to": ...} — rename an entry: the
	// id is the address (design doc 0017), so the move carries revisions,
	// usage, and attachments along and rewrites inbound references (link
	// targets, attrs.model) so nothing breaks (design doc 0021). Its own
	// top-level path for the same reason /usage and /revisions have one:
	// a suffix after the hierarchical {id...} wildcard could be confused
	// with an ID segment.
	mux.HandleFunc("POST /api/v1/move", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		moved, err := svc.Move(r.Context(), in.From, in.To, httpauth.Actor(r.Context()))
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("ETag", etagOf(moved))
		writeView(w, http.StatusOK, moved)
	})

	// GET /api/v1/export — the whole knowledge base as an OKF bundle
	// (tar.gz of markdown + YAML frontmatter, plus attachment files).
	// Your knowledge is yours.
	//
	// Written straight to the response, a batch of entries and one
	// attachment at a time. Collecting the bundle first would mean holding
	// every entry and every attachment's bytes in memory at once — a
	// periodic CI backup would be the largest allocation the process ever
	// makes, on the instance size Cloud Run runs it at. With
	// ?attachments=false the bytes are skipped entirely: they live in GCS,
	// so a backup can copy them from there far more cheaply than through
	// this endpoint.
	mux.HandleFunc("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		// Every read below comes from one snapshot. Streaming means the id
		// list, the attachment metadata and the entries are read at
		// different moments, and a write landing between them produces an
		// archive that disagrees with its own index — an index.md naming a
		// file that is not there, an entry that moved appearing twice or
		// not at all. The archive would look fine.
		// Parsed before the snapshot: a rejected parameter should not
		// have held a pooled connection open, however briefly.
		withAttachments, err := queryBool(r.URL.Query(), "attachments", true)
		if err != nil {
			writeError(w, err)
			return
		}
		snap, err := svc.Store.BeginExport(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		defer snap.Close(context.WithoutCancel(r.Context()))
		// index.md files need every id, but only the id, title and
		// description — never a body.
		rows, err := snap.IndexRows(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		var atts []store.ExportAttachment
		if withAttachments {
			// Metadata only; bytes are pulled one attachment at a time below.
			if atts, err = snap.AttachmentMeta(r.Context()); err != nil {
				writeError(w, err)
				return
			}
		}
		indexes := okf.Indexes(rows) // also sorts rows by id
		ids := make([]string, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
		}

		// Past this point the status is already sent, so a failure can only
		// truncate the stream. Everything that can fail cheaply — the
		// listings above — has already run; everything below is logged,
		// because a silently short archive is the worst possible outcome
		// for the backup this endpoint exists to serve.
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="ochakai-okf.tar.gz"`)
		tgz := okf.NewTarGzWriter(w, time.Now())
		written := make(map[string]bool, len(ids)+len(indexes))
		fail := func(what string, err error) {
			svc.Log.Error("export truncated after the response began",
				"at", what, "error", err, "files_written", len(written))
		}
		add := func(p string, data []byte) error {
			if err := tgz.Add(p, data); err != nil {
				return err
			}
			written[p] = true
			return nil
		}
		// Sorted so the archive's file order is stable across runs: map
		// iteration order is not an ordering. The bytes still differ run to
		// run — every tar header carries the export's timestamp — so this
		// buys a diffable listing, not a checksum.
		indexPaths := make([]string, 0, len(indexes))
		for path := range indexes {
			indexPaths = append(indexPaths, path)
		}
		sort.Strings(indexPaths)
		for _, path := range indexPaths {
			if err := add(path, indexes[path]); err != nil {
				fail("index "+path, err)
				return
			}
		}
		// Entries in batches: one batch in memory at a time. The snapshot
		// does hold a pooled connection for the length of the download —
		// the trade a point-in-time backup makes (see ExportSnapshot);
		// batching bounds the memory, not the connection.
		for start := 0; start < len(ids); start += exportBatch {
			end := min(start+exportBatch, len(ids))
			batch, err := snap.ListByIDs(r.Context(), ids[start:end])
			if err != nil {
				fail(fmt.Sprintf("entries %d-%d", start, end), err)
				return
			}
			for i := range batch {
				doc, err := okf.Document(&batch[i])
				if err != nil {
					fail("render "+batch[i].ID, err)
					return
				}
				if err := add(batch[i].ID+".md", doc); err != nil {
					fail("write "+batch[i].ID, err)
					return
				}
			}
		}
		// Attachments go next to their entries: "<id>/<name>", or the
		// foreign path they were imported at (okf_path) so original body
		// links keep working. A foreign path already taken by a concept
		// document falls back to the canonical layout — identical content
		// at the same path (the same image referenced by two entries) is
		// no conflict.
		for i := range atts {
			a := &atts[i]
			data, err := svc.Store.AttachmentBytes(r.Context(), a.Att.SHA256)
			if err != nil {
				fail("fetch attachment "+a.ID+"/"+a.Att.Name, err)
				return
			}
			p := okf.AttachmentPath(a.ID, &a.Att)
			if written[p] {
				p = a.ID + "/" + a.Att.Name
			}
			if err := add(p, data); err != nil {
				fail("write attachment "+p, err)
				return
			}
		}
		if err := tgz.Close(); err != nil {
			fail("close", err)
		}
	})

	// POST /api/v1/reembed?limit=N&cursor=... — fill in vectors for
	// entries and attachments that have none for the configured model.
	// The response's cursor feeds the next call: a pass whose entries all
	// fail would otherwise hand back the same window forever. Not an MCP
	// tool: an operator task with an unbounded runtime and no place in an
	// agent's turn (design doc 0015).
	mux.HandleFunc("POST /api/v1/reembed", func(w http.ResponseWriter, r *http.Request) {
		limit, err := queryInt(r.URL.Query(), "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		res, err := svc.Reembed(r.Context(), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	return announceReadOnly(svc, mux)
}

// announceReadOnly marks every REST response from a read-only deployment
// with Ochakai-Read-Only: true (design doc 0040 §2.3). REST has no
// capability handshake the way MCP does — where read-only shows up as the
// write tools simply not being offered — so a client learns it from a
// header, the same way it learns an update changed nothing from
// Ochakai-Unchanged. The Web UI uses it to stop drawing buttons that
// would only ever 403.
func announceReadOnly(svc *service.Service, next http.Handler) http.Handler {
	if svc.Config == nil || !svc.Config.ReadOnly {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Ochakai-Read-Only", "true")
		next.ServeHTTP(w, r)
	})
}

// writeHits responds with a hit list, attachment metadata filled in one
// batch — the REST list surface carries it so UIs can render image
// previews; MCP search results stay lean (design doc 0015).
func writeHits(w http.ResponseWriter, r *http.Request, svc *service.Service, hits []domain.SearchHit) {
	// A row is a projection now (design doc 0043 §3.5), so the batch fill
	// runs against stand-ins carrying only the ids and the metadata is
	// copied back onto the rows.
	ptrs := make([]*domain.Knowledge, len(hits))
	for i := range hits {
		ptrs[i] = &domain.Knowledge{ID: hits[i].ID}
	}
	if err := svc.FillAttachments(r.Context(), ptrs); err != nil {
		writeError(w, err)
		return
	}
	for i := range hits {
		hits[i].Attachments = ptrs[i].Attachments
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
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

// documentMediaType is what an OKF concept document is served and
// accepted as. It is markdown with YAML frontmatter, which is markdown as
// far as any consumer is concerned.
const documentMediaType = "text/markdown; charset=utf-8"

// maxDocument bounds a written document. The body limit (design doc 0001)
// is 4 MiB and the frontmatter is small beside it, so this is the same
// number with room for the envelope rather than a new policy.
const maxDocument = 5 << 20

// readDocument parses the request body as an OKF document. Notes — values
// the parser accepted but read differently than written — travel back in
// a header rather than being swallowed: a reinterpretation is never
// silent (design doc 0036 §3.4), and there is no room for them in a
// response whose body is the entry.
//
// A JSON body is refused with the reason rather than a parse error: a
// client sending one is a client written against the typed write surface
// that 0043 §3.5 removed, and it needs to be told that, not told its
// document has no frontmatter.
func readDocument(w http.ResponseWriter, r *http.Request) (*domain.Knowledge, bool) {
	if mt := r.Header.Get("Content-Type"); strings.HasPrefix(mt, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
			"error": "knowledge is written as an OKF document (Content-Type: text/markdown), " +
				"not as JSON: the frontmatter is the metadata and the markdown is the body",
		})
		return nil, false
	}
	data, ok := readBody(w, r, maxDocument, fmt.Sprintf("document exceeds %d bytes", maxDocument))
	if !ok {
		return nil, false
	}
	d, notes, err := okf.Parse(data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, false
	}
	for _, n := range notes {
		w.Header().Add("Ochakai-Note", n)
	}
	return &d.Knowledge, true
}

// frontmatterFilter reads the "fm." query parameters — `?fm.question=…`,
// `?fm.owner=finance` — into the filter that asks the frontmatter index
// (design doc 0046 §3.11). The prefix is what keeps the surface
// additive: a key the spec adds later needs no parameter of its own, and
// none of the ones ochakai already names can be shadowed by one.
//
// A repeated parameter keeps its last value. The pairs are AND-ed, and
// "the same key twice" is a contradiction rather than a question, so
// there is nothing sensible to OR.
func frontmatterFilter(q url.Values) map[string]string {
	var out map[string]string
	for key, vals := range q {
		name, ok := strings.CutPrefix(key, "fm.")
		if !ok || name == "" || len(vals) == 0 {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = vals[len(vals)-1]
	}
	return out
}

// splitReserved cuts a bundle path into the directory it names and its
// last segment. "index.md" is the root's listing, "a/b/log.md" is the
// history under a/b.
func splitReserved(path string) (dir, base string) {
	path = strings.Trim(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

// wantsJSON reports whether the caller asked for the structured form of
// a derived file. Only an explicit JSON Accept counts: a browser or a
// curl sends */* and should get the file that lives at the path.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeMarkdown(w http.ResponseWriter, doc []byte) {
	w.Header().Set("Content-Type", documentMediaType)
	_, _ = w.Write(doc)
}

// wantsDocument reports whether the caller asked for the entry as a
// document rather than as JSON. Only an explicit markdown Accept counts:
// a browser sends */* and wants the JSON every existing client reads.
func wantsDocument(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/markdown")
}

// onlyIfAbsent reports an If-None-Match "*" precondition: write only if
// the id is free. Any other value is ignored — ochakai has no use for a
// conditional read, and a caller that means "only if absent" says "*".
func onlyIfAbsent(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("If-None-Match")) == "*"
}

// writeView responds with a read of one entry: the canonical document,
// the projection, and what this instance observed (design doc 0043 §3.5).
func writeView(w http.ResponseWriter, status int, k *domain.Knowledge) {
	v, err := okf.ViewOf(k)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "render document: " + err.Error()})
		return
	}
	writeJSON(w, status, v)
}

// writeDocument renders the entry as the document an export would write
// — server-owned keys included, since that is what a reader of a bundle
// sees and what `ochakai get` hands to an editor.
//
// A render failure after the status is out cannot be reported, so it is
// decided first: the entry came from the store, so failing here means the
// renderer cannot express something the store holds, which is a 500.
func writeDocument(w http.ResponseWriter, status int, k *domain.Knowledge) {
	doc, err := okf.Document(k)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "render document: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", documentMediaType)
	w.WriteHeader(status)
	_, _ = w.Write(doc)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		// An oversized body is not malformed JSON, and calling it that
		// sends the caller looking for a syntax error in a payload that
		// is simply too big — readBody already answers 413 here.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge,
				map[string]string{"error": fmt.Sprintf("request body exceeds %d bytes", maxErr.Limit)})
			return false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var inputErr *service.InvalidInputError
	var unsupportedErr *service.UnsupportedError
	switch {
	case errors.Is(err, service.ErrReadOnly):
		// 403, not 404: the route exists and the request was understood.
		// This deployment declines to write (design doc 0040).
		status = http.StatusForbidden
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		// The If-Match precondition failed: the entry changed since read.
		status = http.StatusPreconditionFailed
	case errors.Is(err, store.ErrAlreadyExists), errors.Is(err, store.ErrNotDeleted):
		status = http.StatusConflict
	case errors.As(err, &inputErr):
		status = http.StatusBadRequest
	case errors.As(err, &unsupportedErr):
		status = http.StatusNotImplemented
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// etagOf renders an entry's version as an ETag: its updated_at in
// RFC3339Nano, quoted. A client echoes it in If-Match to update
// conditionally (design doc 0030).
// etagOf renders the entry's version: the hash of its canonical document
// (design doc 0043 §3.4). It moves when the entry's content moves and at
// no other time — verifying, rejecting, or attaching a file all leave a
// held precondition valid, which the updated_at version could not do
// because it was also the row's write timestamp.
func etagOf(k *domain.Knowledge) string {
	return `"` + k.ContentHash + `"`
}

// parseIfMatch reads an optional If-Match precondition. Absent or "*"
// means no version precondition (the update still requires the entry to
// exist). Any other value is taken as an entry ETag, quoted or not: it is
// an opaque token now, so there is no format to validate — a value that
// never matched anything simply fails the precondition with a 412, which
// is what a stale one does too.
func parseIfMatch(r *http.Request) *string {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" || v == "*" {
		return nil
	}
	v = strings.Trim(v, `"`)
	return &v
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

// queryBool parses an optional boolean query parameter. Like queryInt it
// refuses what it cannot read rather than falling back to def: "?purge=1"
// answering 204 for a soft delete would tell the caller an id had been
// freed when it had not, and the two calls of a purge are meant to be
// deliberate (design doc 0031).
func queryBool(q url.Values, name string, def bool) (bool, error) {
	switch q.Get(name) {
	case "":
		return def, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, service.Invalidf("invalid %s %q (want true or false)", name, q.Get(name))
	}
}

// queryBool reads a tri-state flag: absent leaves the question unasked,
// so the caller can tell "not mentioned" from "explicitly false" — which
// is the difference between the rejected filter's default (hide them) and
// an explicit rejected=false (also hide them, but said out loud).
func queryTriBool(q url.Values, name string) (*bool, error) {
	if q.Get(name) == "" {
		return nil, nil
	}
	b, err := queryBool(q, name, false)
	if err != nil {
		return nil, err
	}
	return &b, nil
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
