// Package restapi serves /api/v1 so users can build their own web UIs
// (the bundled one lives in internal/webui). It is a superset of the MCP
// tools: the same read, write and usage operations plus the bulk export
// endpoint that makes no sense as an agent tool call, and human-facing
// endpoints (the reserved bundle files, the links_to reverse lookup)
// that stay off MCP by
// design (agents get search/get_context instead; design doc 0015 records
// the per-surface policy). The CLI covers this whole surface; the spec is
// committed at api/openapi.yaml.
//
// One object has one address: /api/v1/bundle/{path}, a concept at
// <id>.md (design doc 0046 §3.5). What is not an object of the bundle is
// addressed outside it — a ranking at /api/v1/search, a judgment at
// /api/v1/review, a measurement at /api/v1/usage.
package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
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

// exportBatch is how many concepts the exporter holds at once. Small
// enough that a knowledge base of any size costs the same peak memory;
// large enough that the round trips disappear against the transfer.
const exportBatch = 100

// writeBundleArchive streams a subtree of the bundle as OKF: the tar.gz
// a caller asks for with Accept: application/gzip. The root is the whole
// knowledge base, which is what GET /api/v1/export was until design doc
// 0046 §3.5 folded it here — an archive is the bundle at a path, so it is
// answered at the address of that path rather than at an endpoint named
// after the act of downloading. `prefix` has already been normalized.
//
// Your knowledge is yours.
func writeBundleArchive(w http.ResponseWriter, r *http.Request, svc *service.Service, prefix string) {
	// Every read below comes from one snapshot. Streaming means the id
	// list, the file metadata and the concepts are read at
	// different moments, and a write landing between them produces an
	// archive that disagrees with its own index — an index.md naming a
	// file that is not there, a concept that moved appearing twice or
	// not at all. The archive would look fine.
	// Parsed before the snapshot: a rejected parameter should not
	// have held a pooled connection open, however briefly.
	withFiles, err := queryBool(r.URL.Query(), "files", true)
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
	rows, err := snap.IndexRows(r.Context(), prefix)
	if err != nil {
		writeError(w, err)
		return
	}
	var atts []domain.File
	if withFiles {
		// Metadata only; bytes are pulled one file at a time below.
		if atts, err = snap.FileMeta(r.Context(), prefix); err != nil {
			writeError(w, err)
			return
		}
	}
	// The generated index.md files list the concepts and the files that
	// sit beside them (design doc 0046 §3.7). With ?files=false
	// there are no file bytes to carry, and an index naming files the
	// archive does not contain would describe a bundle nobody received.
	indexes := okf.Indexes(rows, atts) // also sorts rows by id
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
		// The other file OKF reserves, beside the one that names the
		// directory's contents (design doc 0046 §3.8). This is what
		// makes the history portable: an archive that carried the
		// bundle but not what happened to it left the ledger reachable
		// only from the instance that holds it, and a purge (0031) is
		// the only thing that ever removes a row from it.
		//
		// One query per directory, bounded the same way the address is,
		// so a log.md in an archive says what the one at its address
		// says. Against a file's bytes — a round trip to GCS each — the
		// cost of these does not register.
		//
		// An import never reads them back (§2.3): the history of what
		// happened here is this instance's observation, published the
		// way generated and verified are (design doc 0009).
		dir := strings.TrimSuffix(strings.TrimSuffix(path, "index.md"), "/")
		log, err := snap.RevisionsUnder(r.Context(), dir, service.MaxLogLines)
		if err != nil {
			fail("history "+dir, err)
			return
		}
		if err := add(strings.TrimSuffix(path, "index.md")+"log.md", service.RenderLog(dir, log)); err != nil {
			fail("log "+dir, err)
			return
		}
	}
	// Concepts in batches: one batch in memory at a time. The snapshot
	// does hold a pooled connection for the length of the download —
	// the trade a point-in-time backup makes (see ExportSnapshot);
	// batching bounds the memory, not the connection.
	for start := 0; start < len(ids); start += exportBatch {
		end := min(start+exportBatch, len(ids))
		batch, err := snap.ListByIDs(r.Context(), ids[start:end])
		if err != nil {
			fail(fmt.Sprintf("concepts %d-%d", start, end), err)
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
	// Files go where they live: a file's path is its address (design doc
	// 0046 §3.3), so the archive is the layout, and body links that use
	// it keep working byte-for-byte. A path a concept document already
	// took is skipped rather than written twice — one address holds one
	// object, and the concept is the one that answers there.
	for i := range atts {
		a := &atts[i]
		if written[a.Path] {
			continue
		}
		data, err := svc.Store.AttachmentBytes(r.Context(), a.SHA256)
		if err != nil {
			fail("fetch file "+a.Path, err)
			return
		}
		if err := add(a.Path, data); err != nil {
			fail("write file "+a.Path, err)
			return
		}
	}
	if err := tgz.Close(); err != nil {
		fail("close", err)
	}
}

func Handler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	// GET /api/v1/search?q=...&type=...&status=...&tag=...&source=...&prefix=...&limit=...
	//
	// Named for what it returns rather than for what it returns them
	// from. Every other read in this file answers with an object of the
	// bundle at the address it lives at; this one answers with a
	// *ranking*, which is ochakai's own and is nowhere in the bundle
	// (design doc 0046 §3.5, the same posture as 0033). Under the old
	// spelling /api/v1/knowledge it looked like the collection that
	// /api/v1/knowledge/{id} indexed into, and that reading was wrong in
	// both directions: the ranking is not the collection, and the
	// collection is addressed as the bundle.
	//
	// With sort=verified_at, lists by verification age (oldest first)
	// instead of searching — the feed for canary runs.
	// source narrows to the concepts citing one resource — the reverse of
	// sources[].resource (design doc 0037 §2.3) — and composes with a
	// query or with any sort. links_to is the other reverse lookup, the
	// one /api/v1/backlinks/{id} used to be a face of its own for: what
	// points at this concept. Neither needs a query; on their own they list
	// in address order and page with a cursor, which backlinks could not
	// do (design doc 0046 §3.5, 0050). prefix narrows to the concepts addressed
	// under a path, repeatable and OR-ed, for scoping a search to a team's
	// subtree and the shared one at once (design doc 0041).
	// cursor walks a listing past the limit — a listing has a total order
	// to resume from, a search has a ranking window and refuses it
	// (design doc 0050).
	mux.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := rejectUnknownParams(q,
			"limit", "rejected", "type", "status", "tag", "source", "links_to",
			"prefix", "trust", "q", "sort", "cursor", "fm."); err != nil {
			writeError(w, err)
			return
		}
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
			LinksTo:     q.Get("links_to"),
			Prefixes:    q["prefix"],
			Trust:       domain.ToTrusts(q["trust"]),
			Rejected:    rejected,
			Frontmatter: frontmatterFilter(q),
		}
		page, err := svc.SearchOrList(r.Context(), q.Get("q"), q.Get("sort"), q.Get("cursor"), f, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeHits(w, r, svc, page)
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
	// Every other path is an object of the bundle — a concept at
	// <id>.md, a file at its own path — and this is the only address any
	// of them has (design doc 0046 §3.5).
	mux.HandleFunc("GET /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		q := r.URL.Query()
		// An archive is the bundle at a path: everything under it, as
		// OKF, in the layout it lives at (design doc 0046 §3.5). The
		// root is the whole knowledge base, which is what
		// GET /api/v1/export was. Paths inside the archive are not
		// re-rooted — an export of metrics/ imports back to metrics/,
		// because this is a copy of a subtree and not a move of one.
		if wantsArchive(r) {
			// history addresses a different object than the archive
			// (the object's own log.md, not this subtree's), so
			// combining them is a 400 rather than the archive silently
			// winning — the six representations of §4 get a precedence
			// order because they are representations of the same
			// thing; this is not that (design doc 0064 §2 item 2).
			if err := rejectOutOfModeParams(q, "with Accept: application/gzip", "files"); err != nil {
				writeError(w, err)
				return
			}
			// A reserved name is a file the bundle generates, not a
			// directory in it, so there is no subtree at this address to
			// archive — the same refusal the write face makes, for the
			// same reason. Without it the path normalized to a directory
			// nothing lives under and the caller got an empty archive
			// with a 200 on it.
			if refuseReserved(w, path, "so there is no subtree here to archive; ask the directory it sits in") {
				return
			}
			scope, err := service.NormalizePrefix(path)
			if err != nil {
				writeError(w, err)
				return
			}
			// A concept or a file is an object at its own address, not a
			// directory: NormalizePrefix accepts it — dots are legal
			// inside a segment, so "metrics/revenue.md" is a legal prefix
			// — and nothing lives beneath an object's own address, so the
			// same empty-archive-with-a-200 the reserved names got fixed
			// above is still open here for every other object.
			refused, err := refuseObjectAsArchive(w, r, svc, scope)
			if err != nil {
				writeError(w, err)
				return
			}
			if refused {
				return
			}
			writeBundleArchive(w, r, svc, scope)
			return
		}
		// ?history is the log.md of one object, at the object's own
		// address (design doc 0046 §3.5). It is where a concept's
		// history belongs: <id>/log.md put it inside a directory named
		// after the concept — a directory that need not exist — and
		// took the only spelling the *subtree* history had for its JSON,
		// which is why asking a directory or the root for one was a 404.
		//
		// A file has a history too (§3.1), and this addresses it the
		// same way: the ledger is keyed by path, so "what happened to
		// this object" is one question with one answer whichever kind
		// it is.
		if q.Has("history") {
			if err := rejectOutOfModeParams(q, "with ?history", "history", "limit"); err != nil {
				writeError(w, err)
				return
			}
			// Nothing ever happened to a file that is generated on
			// demand, and "nothing ever happened here" (404) reads as a
			// fact about the bundle rather than about the address. The
			// history of what these two files *report* is the log.md
			// beside them.
			if refuseReserved(w, path, "so it has no history of its own; log.md is the history of its directory") {
				return
			}
			writeObjectHistory(w, r, svc, path)
			return
		}
		dir, base := splitReserved(path)
		switch base {
		case "index.md":
			if err := rejectOutOfModeParams(q, "on index.md", "cursor"); err != nil {
				writeError(w, err)
				return
			}
			if wantsJSON(r) {
				res, err := svc.Browse(r.Context(), dir, q.Get("cursor"))
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, res)
				return
			}
			// The document form does not page (design doc 0101 §4), and a
			// cursor it accepted and ignored would be the silence design
			// doc 0064 §2 closed everywhere else: the caller asked for a
			// page and would receive the first one.
			if q.Has("cursor") {
				writeError(w, service.Invalidf(
					"cursor pages the JSON listing; index.md as a document is one level whole, "+
						"so ask for it with Accept: application/json"))
				return
			}
			doc, err := svc.IndexDocument(r.Context(), dir)
			if err != nil {
				writeError(w, err)
				return
			}
			writeMarkdown(w, doc)
		case "log.md":
			if err := rejectOutOfModeParams(q, "on log.md", "limit"); err != nil {
				writeError(w, err)
				return
			}
			limit, err := queryInt(q, "limit")
			if err != nil {
				writeError(w, err)
				return
			}
			if wantsJSON(r) {
				// The same changes the document lists, structured. The
				// two representations of one address say one thing,
				// which is what put the per-object ledger at ?history
				// above rather than here.
				rows, err := svc.LogRows(r.Context(), dir, limit)
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, changeLog{Changes: asChanges(rows)})
				return
			}
			doc, err := svc.LogDocument(r.Context(), dir, limit)
			if err != nil {
				writeError(w, err)
				return
			}
			writeMarkdown(w, doc)
		default:
			if err := rejectOutOfModeParams(q, "on a concept or file's own address"); err != nil {
				writeError(w, err)
				return
			}
			// Every other path is an object of the bundle: a concept at
			// its own path, or a file (design doc 0046 §3.5). A concept
			// is answered by the concept surface's own writer, so one
			// address serves both kinds and neither has a shape of its
			// own here.
			id, isMarkdown := strings.CutSuffix(path, ".md")
			if isMarkdown {
				k, err := svc.Get(r.Context(), id)
				if err == nil {
					w.Header().Set("ETag", etagOf(k))
					// Both file branches below already answer a matched
					// If-None-Match with a 304; a concept computed the
					// same ETag and never compared it (design doc 0064
					// §14.2, issue #470). The spec declared the header
					// for this GET as a whole, not just for a file.
					fresh, err := unmodified(r, k.ContentHash)
					if err != nil {
						writeError(w, err)
						return
					}
					if fresh {
						w.WriteHeader(http.StatusNotModified)
						return
					}
					if wantsDocument(r) {
						writeDocument(w, http.StatusOK, k)
					} else {
						writeView(w, http.StatusOK, k)
					}
					return
				}
				// A markdown path holds a concept or nothing (design
				// doc 0100 §2). The fall-through to the file branches
				// below, which used to stand here, served the typeless
				// `.md` — an object the write face no longer stores.
				writeError(w, err)
				return
			}
			// application/json on a file's own address answers metadata,
			// not bytes: the only way to read a file's sha256 without
			// downloading it (design doc 0064; the table at 0046 §3.5
			// already promised this column).
			if wantsJSON(r) {
				att, err := svc.GetFileMeta(r.Context(), path)
				if err != nil {
					writeError(w, err)
					return
				}
				w.Header().Set("ETag", `"`+att.SHA256+`"`)
				w.Header().Set("Cache-Control", "private, no-cache")
				fresh, err := unmodified(r, att.SHA256)
				if err != nil {
					writeError(w, err)
					return
				}
				if fresh {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				writeJSON(w, http.StatusOK, att)
				return
			}
			att, data, err := svc.GetFile(r.Context(), path)
			if err != nil {
				writeError(w, err)
				return
			}
			w.Header().Set("ETag", `"`+att.SHA256+`"`)
			w.Header().Set("Cache-Control", "private, no-cache")
			fresh, err := unmodified(r, att.SHA256)
			if err != nil {
				writeError(w, err)
				return
			}
			if fresh {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", mediaTypeHeader(att.MediaType))
			serveDefensively(w, att.MediaType, att.Name)
			_, _ = w.Write(data)
		}
	})

	// PUT and DELETE /api/v1/bundle/{path...} — the write face of the one
	// address space (design doc 0046 §3.5), and now the only one: the
	// second spelling of a concept's address, /api/v1/knowledge/{id}, is
	// gone. It read, wrote and deleted exactly what this face does, one
	// suffix away, which meant a client had to know which of two names
	// ochakai preferred for the same object and an id was a bundle path
	// with the `.md` filed off.
	//
	// A PUT is create-or-replace, and states what the object should say.
	// Whether something already held the address is not part of that
	// statement, so there is no POST beside it: existence is expressed by
	// the preconditions (0030), If-None-Match "*" for "only if free" and
	// If-Match for "only if it still says this".
	//
	// The reserved names are the one refusal the address itself makes:
	// they are generated from the bundle rather than stored in it, so a
	// write there is a conflict with what the address is (409). Every
	// other path is an object, and what it becomes is decided by the
	// bytes rather than by the caller — see below.
	//
	// ?dry_run=true asks what the write would do instead of doing it
	// (design doc 0061): the same body, the same preconditions, the same
	// refusals and the same Ochakai-Note headers, answered from the same
	// code the write runs — and nothing stored. The answer is
	// Ochakai-Plan. It is a query parameter and not a header because a
	// header a proxy drops turns a dry run into a write.
	for _, m := range []string{"PUT", "DELETE"} {
		// purge is DELETE's own parameter (api/openapi.yaml declares it
		// there and not on PUT): a PUT that carries it is a caller who
		// meant the two-step delete-then-purge and sent it to the wrong
		// verb, and that deserves the 400 rejectUnknownParams gives every
		// other misdirected parameter — not the silent no-op it was
		// before (issue #409). dry_run stays allowed on both: DELETE's
		// own handling below turns it into a more specific refusal than
		// "unknown query parameter" would.
		allowed := []string{"dry_run"}
		if m == "DELETE" {
			allowed = append(allowed, "purge")
		}
		mux.HandleFunc(m+" /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
			path := r.PathValue("path")
			if err := rejectUnknownParams(r.URL.Query(), allowed...); err != nil {
				writeError(w, err)
				return
			}
			if refuseReserved(w, path, "and a generated file is not one anybody writes") {
				return
			}
			actor := httpauth.Actor(r.Context())
			dry, err := queryBool(r.URL.Query(), "dry_run", false)
			if err != nil {
				writeError(w, err)
				return
			}
			// A markdown path is a concept's address, and a write there
			// is a write to the concept — the same document, the same
			// preconditions, the same answer. Anything else is a file.
			//
			// The address decides, not the bytes. SPEC §3.1 says every
			// non-reserved `.md` in a bundle is a concept document and
			// §11 makes frontmatter with a non-empty `type` the condition
			// of conformance, so a `.md` carrying no type is refused
			// rather than kept as a file at that address — which is what
			// used to happen, and what put a non-conformant `.md` in
			// every export that carried one (design doc 0100).
			id, isMarkdown := strings.CutSuffix(path, ".md")
			if r.Method == http.MethodDelete {
				// A delete has no plan to report — what it would do is
				// what its name says — and ignoring the parameter here
				// would remove the object of a caller who believed they
				// were asking. Refusing is the only safe reading.
				if dry {
					writeError(w, service.Invalidf("dry_run asks what a write would do; a delete has no plan beside it, so it is not accepted here"))
					return
				}
				// ?purge=true hard-deletes a concept that is already
				// soft-deleted, freeing its id (a tombstone still owns
				// the primary key, so a move onto it fails). Two calls,
				// deliberately: the first is reversible, the second is
				// not. Not on MCP — destroying history is a human
				// decision (design docs 0015, 0031). A file has no
				// tombstone to purge, so the parameter is a concept's —
				// and a file removed with it is simply removed, because
				// its one delete is already the irreversible one. The
				// fall-through runs whether or not purge was asked for:
				// answering 404 for an object that is there, because the
				// caller named a parameter that does not apply to it,
				// tells them it is gone when it is not.
				purge, err := queryBool(r.URL.Query(), "purge", false)
				if err != nil {
					writeError(w, err)
					return
				}
				// A path that is not a concept's address names a file,
				// which takes no precondition (design doc 0064 §20.4).
				// The narrow gap that section wrote down — a typeless
				// `.md` reaching DeleteFile through the concept branch
				// with its precondition unread — is closed by the address
				// rule above: nothing but a concept lives at a `.md`
				// (design doc 0100 §6).
				if !isMarkdown {
					if err := refusePreconditions(r); err != nil {
						writeError(w, err)
						return
					}
				}
				switch {
				case isMarkdown && purge:
					// No If-Match here: purge is already the second,
					// irreversible call in a deliberate two-step sequence
					// (soft-delete, then purge), and asking for a fresh
					// ETag on the second step would tax it with the round
					// trip 0030 §3.5 avoids for the common case (design
					// doc 0064).
					err = svc.Purge(r.Context(), id, actor)
				case isMarkdown:
					ifMatch, perr := parseIfMatch(r)
					if perr != nil {
						writeError(w, perr)
						return
					}
					err = svc.Delete(r.Context(), id, actor, ifMatch)
				default:
					// Not a concept's address, so the object here is a
					// file and DeleteFile is the whole of the answer. The
					// fall-through this replaces existed for the typeless
					// `.md`, which no longer exists (design doc 0100).
					err = svc.DeleteFile(r.Context(), path, actor)
				}
				if err != nil {
					writeError(w, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// At a concept's address the body is an OKF document, which
			// is what a concept is (design doc 0043 §3.5). JSON there is
			// the one wrong guess worth naming: there is no typed write
			// surface beside the format, and stored as bytes it would
			// carry no `type` and be kept as a *file* called `x.md` —
			// the silent wrong outcome. Only at a `.md` path: JSON at
			// any other path is a caller storing a JSON file, which is
			// an object of the bundle like any other.
			if mt := r.Header.Get("Content-Type"); isMarkdown && strings.HasPrefix(mt, "application/json") {
				writeErrorBody(w, http.StatusUnsupportedMediaType, domain.CodeInvalid,
					"a concept is written as an OKF document (Content-Type: text/markdown), "+
						"not as JSON: the frontmatter is the metadata and the markdown is the body")
				return
			}
			body, ok := readBody(w, r, maxDocument, fmt.Sprintf("object exceeds %d bytes", maxDocument))
			if !ok {
				return
			}
			if isMarkdown {
				putEntry(w, r, svc, id, body, actor, dry)
				return
			}
			// Everything from here writes a file, which takes no
			// precondition (design doc 0064 §20.4).
			if err := refusePreconditions(r); err != nil {
				writeError(w, err)
				return
			}
			var (
				att              *domain.File
				created, changed bool
			)
			if dry {
				att, created, changed, err = svc.PlanFile(r.Context(), path, body, actor)
			} else {
				att, created, err = svc.PutFile(r.Context(), path, body, actor)
			}
			if err != nil {
				writeError(w, err)
				return
			}
			if dry {
				// Header only, for a file: unlike a concept, a real file
				// write does not compare the bytes it was handed against
				// the bytes that were there, so the fact exists on one of
				// the two paths and a field present on half of them is
				// worse than the header (design doc 0097 §4).
				w.Header().Set(planHeader, domain.PlanOf(created, changed))
				writeJSON(w, http.StatusOK, att)
				return
			}
			// The spec has declared this header on both 200 and 201 since
			// design doc 0064 (matching the concept PUT, which already
			// sets it); the file write never did (§14.3, issue #470).
			w.Header().Set("ETag", `"`+att.SHA256+`"`)
			status := http.StatusOK
			if created {
				status = http.StatusCreated
			}
			writeJSON(w, status, att)
		})
	}

	// The context-pack address is gone (design doc 0108): an agent's
	// read is search → get, with the concept's linked_from rows carrying
	// what the pack's reverse hop used to (design doc 0106).

	// POST /api/v1/review/{id...} {"ruling": ...} — a human's ruling on
	// the concept (design doc 0055 §3.1). It replaced three endpoints:
	// POST /api/v1/verify/{id}, POST /api/v1/reject/{id} and DELETE
	// /api/v1/reject/{id}.
	//
	// The three shared every structural property. Each appends to a
	// ledger and leaves the document alone — the lifecycle status, the
	// updated_at and the ETag all stay put, because ruling on knowledge
	// and publishing it are different acts (0043 §§3.2-3.3). Each is
	// recorded against the caller's identity, each answers with the
	// concept, and each is on the human surfaces and off MCP, because a
	// ruling is what a person does. What differed between them was the
	// value written. That is one operation with a parameter, spelled as
	// three because they arrived at three different times.
	//
	// "withdrawn" is the DELETE, said as what it is: nothing is deleted.
	// Verifications are an append-only ledger and a rejection is the one
	// live ruling (0043 §3.3), so withdrawing it clears that ruling and
	// leaves the history, recorded under the same word.
	//
	// The note belongs to "rejected" alone. Carrying one on a
	// verification would be a new thing to say rather than the same three
	// said once, and the default answer to that is no (docs/surface.md).
	//
	// "verified" is why a ruling face has to exist at all rather than
	// being sugar over PUT: re-affirming a concept that was already
	// verified is the same act as verifying it the first time, and PUT
	// writes nothing when the content is unchanged, which left both
	// review feeds without an exit (design doc 0025 §6).
	//
	// It lives at a top-level path for the reason /usage does — a
	// "/review" suffix after the hierarchical {id...} would be
	// indistinguishable from an ID segment.
	mux.HandleFunc("POST /api/v1/review/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query()); err != nil {
			writeError(w, err)
			return
		}
		var in struct {
			Ruling string `json:"ruling"`
			Note   string `json:"note"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		id, actor := r.PathValue("id"), httpauth.Actor(r.Context())
		var k *domain.Knowledge
		var err error
		switch in.Ruling {
		case "verified":
			if in.Note != "" {
				writeError(w, service.Invalidf(
					`a verification carries no note: "%s" says the concept is right as it stands, and `+
						`anything to say about it belongs in the concept. Use ruling=rejected to record a reason`,
					in.Ruling))
				return
			}
			k, err = svc.Verify(r.Context(), id, actor)
		case "rejected":
			k, err = svc.Reject(r.Context(), id, in.Note, actor)
		case "withdrawn":
			if in.Note != "" {
				writeError(w, service.Invalidf(
					`withdrawing a rejection carries no note: it removes a ruling rather than making one`))
				return
			}
			k, err = svc.WithdrawRejection(r.Context(), id, actor)
		default:
			// The arms above are the vocabulary; domain.Rulings is what
			// every surface quotes it as, so the refusal cannot name a
			// different set than the one the switch accepts.
			writeError(w, service.Invalidf(
				`ruling must be one of %s, not %q`, strings.Join(domain.Rulings, ", "), in.Ruling))
			return
		}
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("ETag", etagOf(k))
		writeView(w, http.StatusOK, k)
	})

	// GET /api/v1/usage/{id...} — how often the concept was actually used
	// (search hits, fetches, outcomes). The measure of the write-back
	// loop: draft promotion evidence, staleness signal. Lives outside
	// /knowledge/ so a "/usage" suffix can never be confused with an ID
	// segment.
	mux.HandleFunc("GET /api/v1/usage/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query()); err != nil {
			writeError(w, err)
			return
		}
		u, err := svc.Usage(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	})
	// POST reports an outcome (worked/failed) against the concept — the
	// write half of the same usage resource, so no new API surface is
	// added (issue #41). Responds with the updated totals.
	mux.HandleFunc("POST /api/v1/usage/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query()); err != nil {
			writeError(w, err)
			return
		}
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

	// GET /api/v1/stats?days=30&prefix=... — the instance's own view of
	// the loop (design doc 0051): what the base is made of, what review
	// did lately, what callers reported, what is waiting, and what they
	// searched for and did not find. Every other measurement here is per
	// concept; this is the only face that answers a question about the base
	// as a whole, which is what running an improvement loop needs.
	//
	// GET /api/v1/queues was a second face over the third of those
	// answers, and is gone (design doc 0049 §3.1): it returned the queue
	// depths this response already carried, under the same key, and the
	// one thing it could do that this could not was scope them to a
	// subtree. So the scope moved here instead, where it now narrows
	// every number keyed by a concept rather than only the three — a team
	// on a shared deployment could count its own queue and could not
	// count its own knowledge.
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query(), "days", "prefix"); err != nil {
			writeError(w, err)
			return
		}
		days, err := queryInt(r.URL.Query(), "days")
		if err != nil {
			writeError(w, err)
			return
		}
		st, err := svc.Stats(r.Context(), days, r.URL.Query()["prefix"])
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})

	// A file has one address, and it is the path it lives at:
	// /api/v1/bundle/{path} reads, writes and deletes it (design doc 0046
	// §§3.3, 3.5). /api/v1/attachments/{id}/{name} is gone with the
	// concept it was named after — a file was something a concept *had*,
	// and the address said so by putting the concept's id in front of a
	// filename. Whether a concept shows a file is now a question its body
	// answers, so the address that asserted it had nothing left to say.

	// POST /api/v1/move {"from": ..., "to": ...} — rename a concept: the
	// id is the address (design doc 0017), so the move carries revisions,
	// usage, and files along and rewrites inbound references (link
	// targets, attrs.model) so nothing breaks (design doc 0021). Its own
	// top-level path for the same reason /usage and /review have one:
	// a suffix after the hierarchical {id...} wildcard could be confused
	// with an ID segment.
	mux.HandleFunc("POST /api/v1/move", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query()); err != nil {
			writeError(w, err)
			return
		}
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

	// POST /api/v1/reembed?limit=N&cursor=... — fill in vectors for
	// concepts and files that have none for the configured model.
	// The response's cursor feeds the next call: a pass whose concepts all
	// fail would otherwise hand back the same window forever. Not an MCP
	// tool: an operator task with an unbounded runtime and no place in an
	// agent's turn (design doc 0015).
	mux.HandleFunc("POST /api/v1/reembed", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectUnknownParams(r.URL.Query(), "limit", "cursor"); err != nil {
			writeError(w, err)
			return
		}
		// The operation reads no body, so one arriving is refused rather
		// than dropped: `{"limit": 10}` in the body would silently run
		// the default-sized pass — the same false green §2 of design doc
		// 0064 closed for unknown query keys.
		var one [1]byte
		if n, _ := r.Body.Read(one[:]); n > 0 {
			writeError(w, service.Invalidf("reembed takes no request body; limit and cursor are query parameters"))
			return
		}
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

	// The log sits inside AnnounceReadOnly and outside the mux: it needs
	// the pattern the mux matched, which ServeMux.Handler answers without
	// serving anything, and it should see the status every layer below it
	// produced.
	logged := requestLog(svc.Log, func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	}, stdlibDefaultsAsJSON(mux))
	return AnnounceReadOnly(svc, logged)
}

// stdlibDefaultsAsJSON rewrites the two responses net/http's ServeMux
// answers by itself — an unmatched path, and a path matched by another
// method — into the same {"error": "..."} envelope every handler in this
// file writes by hand, instead of the plain-text body http.Error puts on
// the wire. Both travel through http.Error, which always sets
// Content-Type to text/plain before the status is written; every
// deliberate response here sets a JSON Content-Type first (writeJSON
// runs before WriteHeader), so a still-text/plain Content-Type at a 404
// or 405 can only be one of these two, never an application 404 like
// store.ErrNotFound.
func stdlibDefaultsAsJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&defaultEnvelopeWriter{ResponseWriter: w}, r)
	})
}

type defaultEnvelopeWriter struct {
	http.ResponseWriter
	rewriting bool
}

func (w *defaultEnvelopeWriter) WriteHeader(status int) {
	if (status != http.StatusNotFound && status != http.StatusMethodNotAllowed) ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.rewriting = true
	msg, code := "not found", domain.CodeNotFound
	if status == http.StatusMethodNotAllowed {
		msg, code = "method not allowed", domain.CodeMethodNotAllowed
	}
	body, _ := json.Marshal(map[string]string{"error": msg, "code": code})
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(body)
}

func (w *defaultEnvelopeWriter) Write(b []byte) (int, error) {
	if w.rewriting {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// AnnounceReadOnly marks every REST response from a read-only deployment
// with Ochakai-Read-Only: true (design doc 0040 §2.3). REST has no
// capability handshake the way MCP does — where read-only shows up as the
// write tools simply not being offered — so a client learns it from a
// header, the same way it learns an update changed nothing from
// Ochakai-Unchanged. The Web UI uses it to stop drawing buttons that
// would only ever 403.
//
// Handler already wraps itself in it; main wraps the auth middleware in
// it too, because the contract says "every /api/v1 response" and the
// 400/403 the middleware writes are /api/v1 responses (design doc 0064).
func AnnounceReadOnly(svc *service.Service, next http.Handler) http.Handler {
	if svc.Config == nil || !svc.Config.ReadOnly {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Ochakai-Read-Only", "true")
		next.ServeHTTP(w, r)
	})
}

// writeHits responds with a hit list, file metadata filled in one
// batch — the REST list surface carries it so UIs can render image
// previews; MCP search results stay lean (design doc 0015).
// The page's cursor rides along when a listing has more behind it
// (design doc 0050 §2.1); a search never has one.
func writeHits(w http.ResponseWriter, r *http.Request, svc *service.Service, page *service.Listing) {
	// A row is a projection now (design doc 0043 §3.5), so the batch fill
	// runs against stand-ins carrying only the ids and the metadata is
	// copied back onto the rows.
	ptrs := make([]*domain.Knowledge, len(page.Hits))
	for i := range page.Hits {
		ptrs[i] = &domain.Knowledge{ID: page.Hits[i].ID}
	}
	if err := svc.FillFiles(r.Context(), ptrs); err != nil {
		writeError(w, err)
		return
	}
	for i := range page.Hits {
		page.Hits[i].Files = ptrs[i].Files
	}
	body := map[string]any{"hits": page.Hits}
	if page.Cursor != "" {
		body["cursor"] = page.Cursor
	}
	writeJSON(w, http.StatusOK, body)
}

// changeLog is the JSON representation of a generated log.md: the same
// changes the document lists, in the same order, without the prose
// (design doc 0046 §3.8). A web UI's history panel reads this rather
// than parsing markdown; it is not a second surface.
type changeLog struct {
	Changes []change `json:"changes"`
}

// change is one line of that history. It names the object by its path,
// because a revision is an event about an object (design doc 0046 §3.1)
// and a file has no concept id — the same reason the ledger is keyed by
// path. Title is the object's as it stands, absent for a file and for a
// concept that has since been purged.
type change struct {
	Path      string       `json:"path"`
	Title     string       `json:"title,omitempty"`
	Change    string       `json:"change"`
	ChangedBy domain.Actor `json:"changed_by"`
	ChangedAt time.Time    `json:"changed_at"`
}

func asChanges(rows []store.LogRow) []change {
	out := make([]change, 0, len(rows))
	for _, r := range rows {
		out = append(out, change{
			Path: r.Path, Title: r.Title, Change: r.Change,
			ChangedBy: r.ChangedBy, ChangedAt: r.ChangedAt,
		})
	}
	return out
}

// writeObjectHistory answers ?history: the ledger rows of the one object
// at this path, as JSON whatever the Accept says. A concept's carry the
// document it was at each change, so a diff between two revisions is a
// text diff — which is what `ochakai revisions --json` and the web UI's
// history panel are for. A file's carry none, because a file has none.
//
// **There is no markdown here** (design doc 0102). SPEC §9 defines one
// markdown history — `log.md`, the reserved file beside the objects —
// and this address used to render a second one from the same rows, which
// left the same `?history&limit=500` answering 400 as JSON and 200 as
// markdown for a concept. The format's own spelling is the one that
// stays.
func writeObjectHistory(w http.ResponseWriter, r *http.Request, svc *service.Service, path string) {
	limit, err := queryInt(r.URL.Query(), "limit")
	if err != nil {
		writeError(w, err)
		return
	}
	// A concept's revisions carry the document it was; a file's carry
	// none, because a file has none, so its rows are the shape a
	// directory's history has. Which of the two this is is read off the
	// path, because the address decides what may live there (design doc
	// 0100 §2).
	if id, isMarkdown := strings.CutSuffix(path, ".md"); isMarkdown {
		revs, err := svc.Revisions(r.Context(), id, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
		return
	}
	rows, err := svc.ObjectHistory(r.Context(), path, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, changeLog{Changes: asChanges(rows)})
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
			writeErrorBody(w, http.StatusRequestEntityTooLarge, domain.CodeTooLarge, tooLarge)
		} else {
			writeErrorBody(w, http.StatusBadRequest, domain.CodeInvalid, "read body: "+err.Error())
		}
		return nil, false
	}
	return data, true
}

// documentMediaType is what an OKF concept document is served and
// accepted as. It is markdown with YAML frontmatter, which is markdown as
// far as any consumer is concerned.
const documentMediaType = "text/markdown; charset=utf-8"

// maxDocument bounds a written document. The body limit (design doc 0081)
// is 4 MiB and the frontmatter is small beside it, so this is the same
// number with room for the envelope rather than a new policy.
const maxDocument = 5 << 20

// frontmatterFilter reads the "fm." query parameters — `?fm.resource=…`,
// `?fm.runtime=sql` — into the filter that asks the frontmatter index
// (design doc 0046 §3.11). The prefix is what keeps the surface
// additive: a key OKF adds later needs no parameter of its own, and none
// of the ones ochakai already names can be shadowed by one.
//
// Which keys are answered is the service's call (checkedFilter): OKF's
// own, less the ones a named filter asks. Reading them all here and
// refusing there keeps the refusal in the one place all three surfaces
// pass through (design doc 0015 §2).
//
// The pairs are AND-ed. A repeated key never reaches here: "the same key
// twice" is a contradiction rather than a question, so there is nothing
// sensible to OR, and rejectUnknownParams answers it with a 400 naming
// the key. This used to keep the last value, which was the same silence
// the rest of the query surface stopped giving (design doc 0064 §20.2).
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

// refuseReserved answers 409 when the path names one of the two files
// OKF reserves per directory, and reports whether it did. because is what
// follows "generated from the bundle, not stored in it," for the face
// that asked — a write has no place to land, an archive has no subtree,
// a history has no events.
//
// Every face that reads the path as something other than "the object at
// it" goes through here, so the reservation is answered once. It used to
// be the write face's own three lines, which is why asking a reserved
// name for an archive returned an empty one with a 200 on it: the
// address was the same address, and only one of its faces knew.
func refuseReserved(w http.ResponseWriter, path, because string) bool {
	_, base := splitReserved(path)
	if !domain.ReservedBundleName(base) {
		return false
	}
	writeErrorBody(w, http.StatusConflict, domain.CodeInvalid,
		fmt.Sprintf("%s is generated from the bundle, not stored in it (design doc 0075 §4.2), %s",
			base, because))
	return true
}

// refuseObjectAsArchive answers 409 when path names one object — a
// concept at its own <id>.md or a file at its own path — rather than a
// directory nothing lives under, and reports whether it did. It goes
// straight to the store rather than through svc.Get: this is a check of
// whether the address is a subtree, not a read of the object living
// there, and must not count as a fetch or fault on missing files.
//
// A prefix that is not an object's own address but still matches
// nothing — a typo one level too deep, as much as a directory that is
// legitimately empty — is deliberately not refused here: the two are
// indistinguishable from the store's side, and treating "matches
// nothing" as an error would refuse every empty subtree along with
// every typo. Only an object's own address is a defect this endpoint
// can tell apart from a real, empty directory (issue #399).
func refuseObjectAsArchive(w http.ResponseWriter, r *http.Request, svc *service.Service, path string) (bool, error) {
	if path == "" {
		return false, nil // the root is the whole bundle, never an object's own address
	}
	isObject := false
	if id, ok := strings.CutSuffix(path, ".md"); ok {
		switch _, err := svc.Store.Get(r.Context(), id); {
		case err == nil:
			isObject = true
		case !errors.Is(err, store.ErrNotFound):
			return false, err
		}
	}
	if !isObject {
		switch _, err := svc.Store.GetFileMeta(r.Context(), path); {
		case err == nil:
			isObject = true
		case !errors.Is(err, store.ErrNotFound):
			return false, err
		}
	}
	if !isObject {
		return false, nil
	}
	writeErrorBody(w, http.StatusConflict, domain.CodeInvalid, fmt.Sprintf(
		"%s is an object, not a directory (design doc 0064 §4); there is no subtree here to archive, ask the directory it sits in", path))
	return true, nil
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

// wantsJSON, wantsDocument and wantsArchive are the whole of content
// negotiation at the bundle address, and design doc 0064 is where their
// precedence is written down as one rule rather than left implicit in
// three independent call sites:
//
//  1. application/gzip, checked once at the top of the GET handler,
//     outranks everything — but only a directory answers it; an object's
//     own address refuses it with 409 (refuseObjectAsArchive) rather
//     than falling through to one of the forms below.
//  2. Below that, each kind of object answers a fixed pair — one
//     representation for an explicit Accept, one default for everything
//     else (including no Accept at all, and an Accept neither predicate
//     recognizes):
//     index.md / log.md   — application/json: structured; default: the
//     generated markdown document.
//     a concept (<id>.md) — text/markdown: the export-form document;
//     default: the JSON View. 0046 §3.5's table said the unmarked
//     default should be the export form; the code has answered the
//     View by default since the address landed — what "every existing
//     client reads" (see wantsDocument below) — and 0064 corrects the
//     table to match the code rather than the other way around, since
//     changing the default now would break every existing REST client
//     silently, at the moment the wire is meant to stop moving.
//     a file               — application/json: its metadata, with no
//     bytes (design doc 0064 — the one way to read a sha256 without
//     downloading it, closing the gap 0046 §3.5's table had already
//     promised); default: the bytes.
//
// acceptsType reports whether the caller named mediaType in Accept.
//
// **Accept here is not a preference ranking.** An address has one default
// representation and at most two media types that select another — the
// archive of the subtree under it, and the object's own alternative form
// (design doc 0064 §4) — so there is nothing to rank. The header is read
// as a *set of names*: split on commas, parameters dropped, the media
// type compared exactly and case-insensitively as RFC 9110 defines it.
// Naming a type selects it; everything else, `*/*` and an unrecognized
// type and no header at all, leaves the default.
//
// **`q` is dropped with the other parameters, `q=0` included.** RFC 9110
// reads `q=0` as "not this one", which ochakai does not implement: a
// caller that wants the default asks for it by *not naming the token*, so
// an exclusion has nothing here to express that omission does not. The
// cost is one honest sentence in the contract — `application/gzip;q=0`
// still gets the archive — rather than a ranking rule a reader would have
// to run in their head to predict a response (design doc 0064 §22).
//
// Exact comparison is the other half of making that rule true. Substring
// matching answered `text/markdown-x` as markdown and was case-sensitive,
// so the written rule and the code agreed only by luck.
func acceptsType(r *http.Request, mediaType string) bool {
	for entry := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
		name, _, _ := strings.Cut(entry, ";")
		if strings.EqualFold(strings.TrimSpace(name), mediaType) {
			return true
		}
	}
	return false
}

// wantsJSON reports whether the caller asked for the structured form of
// a derived file. Only an explicit JSON Accept counts: a browser or a
// curl sends */* and should get the file that lives at the path.
func wantsJSON(r *http.Request) bool {
	return acceptsType(r, "application/json")
}

func writeMarkdown(w http.ResponseWriter, doc []byte) {
	w.Header().Set("Content-Type", documentMediaType)
	_, _ = w.Write(doc)
}

// wantsDocument reports whether the caller asked for the concept as a
// document rather than as JSON. Only an explicit markdown Accept counts:
// a browser sends */* and wants the JSON every existing client reads.
func wantsDocument(r *http.Request) bool {
	return acceptsType(r, "text/markdown")
}

// wantsArchive reports whether the caller asked for a path as an OKF
// bundle rather than as one object. Explicit only, like the other two: a
// browser sending */* is asking to look at something, not to download
// the knowledge base under it.
func wantsArchive(r *http.Request) bool {
	return acceptsType(r, "application/gzip")
}

// onlyIfAbsent reports an If-None-Match "*" precondition on a write:
// write only if the path is free. It is how a caller states that it is
// not replacing anything, which a create-or-replace PUT cannot say any
// other way (design doc 0030), and it is what `--only-if-new` and the
// MCP write path send.
//
// Any other value is a 400 naming it. RFC 9110 gives an entity-tag here
// the meaning "write only if the object is *not* this version", which is
// not something a knowledge store has a use for, and until design doc
// 0064 it was dropped in silence — the write went ahead unconditioned,
// which is the outcome a caller sending a precondition least wants
// (§20.4).
func onlyIfAbsent(r *http.Request) (bool, error) {
	switch v := strings.TrimSpace(r.Header.Get("If-None-Match")); v {
	case "":
		return false, nil
	case "*":
		return true, nil
	default:
		return false, service.Invalidf(
			`If-None-Match on a write takes only "*", for "write only if the path is free"; a version here is not a precondition ochakai implements`)
	}
}

// unmodified reports whether an If-None-Match validator still matches the
// version being served, so the read can answer 304 instead of the body.
//
// "*" is a 400. RFC 9110 reads it on a GET as "only if nothing is here",
// which for an object that exists means a 304 — the opposite of the
// cache validator this header is otherwise used as, and until design doc
// 0064 it was ignored and answered 200 with the whole body (§20.4). The
// caller that wants to know whether something exists asks for it.
// refusePreconditions 400s a write that carries either precondition to an
// address holding a file rather than a concept.
//
// The contract declares If-Match and If-None-Match on the bundle write
// because one address writes both kinds (design doc 0075 §4), but only a
// concept has the version they name: optimistic locking is a concept's
// content hash and its ledger (design doc 0030). A file write has never
// read either header — it overwrote whatever was there, precondition or
// not, which is the outcome a caller sending one least wants. Naming it
// leaves the door open: conditional file writes can be implemented later
// without breaking a caller, because nothing can depend on a request that
// always failed (design doc 0064 §20.4).
func refusePreconditions(r *http.Request) error {
	for _, name := range []string{"If-Match", "If-None-Match"} {
		if strings.TrimSpace(r.Header.Get(name)) == "" {
			continue
		}
		return service.Invalidf(
			"%s conditions a write on a concept's version (design doc 0030); a file is written at its path without one", name)
	}
	return nil
}

func unmodified(r *http.Request, version string) (bool, error) {
	v := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if v == "" {
		return false, nil
	}
	if v == "*" {
		return false, service.Invalidf(
			`If-None-Match "*" is not a precondition ochakai implements on a read; send the version a prior read returned in the ETag header`)
	}
	return strings.Contains(v, version), nil
}

// putEntry writes one document as the concept at id and answers with the
// read of it. Two addresses lead here — the concept surface and the bundle
// path the concept lives at (design doc 0046 §3.5) — and a concept
// written through either is written the same way, with the same
// preconditions and the same answer. An address is not a second surface.
func putEntry(w http.ResponseWriter, r *http.Request, svc *service.Service,
	id string, body []byte, actor domain.Actor, dry bool,
) {
	// Bytes that are not a concept, at an address that holds nothing else:
	// the two ways to fail that are one refusal, because the caller meant
	// one of two things and the answer has to name both. Until design doc
	// 0100 the second of them stored a file called `x.md` in silence, and
	// every export carried a `.md` with no frontmatter — the definition of
	// a bundle that does not conform (SPEC §11.1, §11.2).
	d, notes, err := okf.Parse(body)
	switch {
	case err != nil:
		refuseNonConcept(w, err.Error())
		return
	case strings.TrimSpace(string(d.Type)) == "":
		// validate() would answer `invalid type ""`, which describes the
		// field rather than the choice in front of the caller.
		refuseNonConcept(w, "a concept's frontmatter names a `type` (OKF SPEC §11)")
		return
	}
	// The path is the address; the document carries the metadata — type
	// included, always (no fill-in from the stored concept, design doc 0017
	// §4.5).
	k := &d.Knowledge
	k.ID = id

	// If-Match is an optional optimistic-concurrency precondition: its
	// value is the ETag from a prior read. Absent means last-write-wins
	// (design doc 0030).
	ifMatch, err := parseIfMatch(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ifAbsent, err := onlyIfAbsent(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var (
		out              *domain.Knowledge
		created, changed bool
	)
	switch {
	case dry:
		// If-None-Match "*" is a precondition on a write that is not
		// happening, and the plan already says whether the id is free —
		// which is the whole of what the precondition would have told
		// the caller, without the 412 (design doc 0061).
		out, created, changed, err = svc.Plan(r.Context(), k, actor, ifMatch)
	case ifAbsent:
		out, err = svc.Create(r.Context(), k, actor)
		created, changed = true, true
		if errors.Is(err, store.ErrAlreadyExists) {
			// The If-None-Match "*" precondition failed: the path is
			// occupied, which is what the caller asked to be told rather
			// than have overwritten. RFC 9110 and the spec both say 412
			// here — 409 is writeError's answer for every other
			// ErrAlreadyExists (move onto an occupied id, a file where a
			// concept already sits), none of which came with a
			// precondition to fail (design doc 0064).
			//
			// The code says which 412 this is, and that is the point of
			// having one: a stale If-Match and an occupied path are the
			// same status and different problems — retry after re-reading
			// versus pick another id (design doc 0083).
			writeErrorBody(w, http.StatusPreconditionFailed, domain.CodeAlreadyExists, err.Error())
			return
		}
	default:
		out, created, changed, err = svc.Put(r.Context(), k, actor, ifMatch)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	// Whether a trust family was a claim or this instance's own export
	// form coming home is decided against the stored concept, so the note
	// can only be written once the write has answered: a claim that
	// survived it is one the document made and ochakai did not observe.
	notes = okf.NoteClaim(notes, d.Claimed, out)
	for _, n := range notes {
		w.Header().Add("Ochakai-Note", n)
	}
	// What the write did, in the body as well as in the headers (design
	// doc 0097). The headers stay because header names are inside the
	// freeze (0082 §2); the body is where a reader of `curl | jq` and a
	// generated client find it without knowing to look.
	plan := domain.PlanOf(created, changed)
	// A dry run has nothing to report about a version that was not
	// written: no ETag, because there is no state to precondition on, and
	// no 201, because nothing was created. What it has is the plan.
	if dry {
		w.Header().Set(planHeader, plan)
		if wantsDocument(r) {
			writeDocument(w, http.StatusOK, out)
			return
		}
		writePlannedView(w, http.StatusOK, out, plan)
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
	writePlannedView(w, status, out, plan)
}

// Ochakai-Plan is what ?dry_run=true answers with: the one word the write
// would have been (design doc 0061). It replaces Ochakai-Unchanged on a
// dry run rather than joining it — "unchanged" is one of the three values,
// and two headers saying overlapping things is how a client ends up
// reading only one of them.
//
// The words themselves are domain.Plans: the body says them too now, and
// a vocabulary spelled once in the package that owns it cannot drift
// between the two places it is written (design doc 0097).
const planHeader = "Ochakai-Plan"

// writeView responds with a read of one concept: the canonical document,
// the projection, and what this instance observed (design doc 0043 §3.5).
// refuseNonConcept answers a write of bytes that are not a concept at an
// address only a concept may hold (design doc 0100 §2). It names the
// other way out, because the caller who meant to store a markdown file
// is the one this refusal is new to — and the path they need is one
// character away from the one they used.
func refuseNonConcept(w http.ResponseWriter, why string) {
	writeErrorBody(w, http.StatusBadRequest, domain.CodeInvalid,
		"a `.md` path is a concept's address (OKF SPEC §3.1): "+why+
			" — write these bytes at a path that does not end in `.md` to store them as a file")
}

func writeView(w http.ResponseWriter, status int, k *domain.Knowledge) {
	writePlannedView(w, status, k, "")
}

// writePlannedView is the same answer from a write, carrying the one word
// the write turned out to be (design doc 0097). plan is empty on a read,
// where none of the three words is true.
func writePlannedView(w http.ResponseWriter, status int, k *domain.Knowledge, plan string) {
	v, err := okf.ViewOf(k)
	if err != nil {
		writeError(w, fmt.Errorf("render document: %w", err))
		return
	}
	v.Plan = plan
	writeJSON(w, status, v)
}

// writeDocument renders the concept as the document an export would write
// — server-owned keys included, since that is what a reader of a bundle
// sees and what `ochakai get` hands to an editor.
//
// A render failure after the status is out cannot be reported, so it is
// decided first: the concept came from the store, so failing here means the
// renderer cannot express something the store holds, which is a 500.
func writeDocument(w http.ResponseWriter, status int, k *domain.Knowledge) {
	doc, err := okf.Document(k)
	if err != nil {
		writeError(w, fmt.Errorf("render document: %w", err))
		return
	}
	w.Header().Set("Content-Type", documentMediaType)
	w.WriteHeader(status)
	_, _ = w.Write(doc)
}

// readJSON decodes the request body strictly: an unrecognized key is a 400
// naming it, the same shape rejectUnknownParams gives an unrecognized query
// key, and content trailing the JSON value is a 400 rather than silently
// discarded. Before design doc 0064 §2 neither was checked — a body key a
// server did not yet know was a 200 that dropped it, the same false-green
// shape ?dry_run= had as a query parameter before this doc closed that one.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// An oversized body is not malformed JSON, and calling it that
		// sends the caller looking for a syntax error in a payload that
		// is simply too big — readBody already answers 413 here.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErrorBody(w, http.StatusRequestEntityTooLarge, domain.CodeTooLarge,
				fmt.Sprintf("request body exceeds %d bytes", maxErr.Limit))
			return false
		}
		if field, ok := strings.CutPrefix(err.Error(), `json: unknown field "`); ok {
			writeError(w, service.Invalidf("unknown field %q", strings.TrimSuffix(field, `"`)))
			return false
		}
		writeErrorBody(w, http.StatusBadRequest, domain.CodeInvalid, "invalid JSON: "+err.Error())
		return false
	}
	if dec.More() {
		writeError(w, service.Invalidf("request body must be a single JSON value"))
		return false
	}
	return true
}

// writeError maps a sentinel to the status and the machine-readable code
// that stand for it. The mapping lives here, once, rather than at the
// sixty-odd call sites: what a caller can branch on is the condition,
// and the condition is what the sentinel already names.
func writeError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, domain.CodeInternal
	var inputErr *service.InvalidInputError
	var unsupportedErr *service.UnsupportedError
	switch {
	case errors.Is(err, service.ErrReadOnly):
		// 403, not 404: the route exists and the request was understood.
		// This deployment declines to write (design doc 0040).
		status, code = http.StatusForbidden, domain.CodeReadOnly
	case errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, domain.CodeNotFound
	case errors.Is(err, store.ErrConflict):
		// The If-Match precondition failed: the concept changed since read.
		status, code = http.StatusPreconditionFailed, domain.CodePreconditionFailed
	// The three conditions that share 409 are the reason the code exists:
	// on the wire they were one status and three sentences, and the
	// sentences are the half the compatibility policy lets move.
	case errors.Is(err, store.ErrAlreadyExists):
		status, code = http.StatusConflict, domain.CodeAlreadyExists
	case errors.Is(err, store.ErrNotDeleted):
		status, code = http.StatusConflict, domain.CodeNotDeleted
	case errors.Is(err, store.ErrNoRejection):
		status, code = http.StatusConflict, domain.CodeNoRejection
	case errors.As(err, &inputErr):
		status, code = http.StatusBadRequest, domain.CodeInvalid
	case errors.As(err, &unsupportedErr):
		status, code = http.StatusNotImplemented, domain.CodeUnsupported
	// The same condition arriving from the store rather than the service.
	// Every other path asks the service, which checks for a blob store
	// and says so; the archive reads the store directly, because whether
	// it needs one is a property of what the snapshot contains rather
	// than of the request. Without this the one endpoint that exists to
	// be a backup answered "internal error" for a deployment that has
	// file rows and no bucket — the true reason left in the server's log,
	// in exactly the case an operator most needs it.
	case errors.Is(err, store.ErrNoBlobStore):
		status, code = http.StatusNotImplemented, domain.CodeUnsupported
	}
	if status == http.StatusInternalServerError {
		// Every status above is a message a caller is meant to read; this
		// one is whatever the store or a driver said, which can carry SQL
		// text or a connection string. Logged for an operator instead of
		// put on the wire.
		slog.Default().Error("unhandled REST error", "error", err)
		writeErrorBody(w, status, domain.CodeInternal, "internal error")
		return
	}
	writeErrorBody(w, status, code, err.Error())
}

// writeErrorBody is the one place the error envelope is spelled: a
// sentence for a person, a code for a client (design doc 0082 leaves a
// response-only addition outside the freeze).
func writeErrorBody(w http.ResponseWriter, status int, code, msg string) {
	// The request log wants the condition, and the wire already carries
	// it; handing it over here keeps the log from having to read the body
	// back (requestlog.go).
	if rec, ok := w.(codeRecorder); ok {
		rec.recordCode(code)
	}
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// etagOf renders the concept's version as an ETag: the hash of its
// canonical document, quoted. A client echoes it in If-Match to update
// conditionally (design docs 0030, 0043 §3.4). It moves when the concept's
// content moves and at no other time — verifying, rejecting, or attaching
// a file all leave a held precondition valid, which the updated_at
// version could not do because it was also the row's write timestamp.
func etagOf(k *domain.Knowledge) string {
	return `"` + k.ContentHash + `"`
}

// parseIfMatch reads an optional If-Match precondition. Absent means no
// precondition — last write wins. Any value is taken as a concept ETag,
// quoted or not: it is an opaque token now, so there is no format to
// validate — a value that never matched anything simply fails the
// precondition with a 412, which is what a stale one does too.
//
// "*" is the one value refused, with a 400 naming it. RFC 9110 §13.1.1
// gives it a meaning ochakai does not implement — "only if a
// representation exists", the update-only mirror of the create-only
// If-None-Match "*" — and until design doc 0064 it was read as no
// precondition at all, so a caller sending it to guard against creating
// silently created. That is the shape §2 spent the freeze closing: an
// unimplemented spelling has to be named, not obeyed backwards. Refusing
// it is also what keeps the choice open. A 400 can be given RFC's meaning
// in a later release without breaking a caller, because nobody can be
// relying on a request that never succeeded; freezing either behaviour
// would settle it forever (design doc 0064 §2, §20.1).
func parseIfMatch(r *http.Request) (*string, error) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" {
		return nil, nil
	}
	if v == "*" {
		return nil, service.Invalidf(
			`If-Match "*" is not a precondition ochakai implements; send the version a prior read returned in the ETag header, or no If-Match at all`)
	}
	v = strings.Trim(v, `"`)
	return &v, nil
}

// rejectUnknownParams 400s on the lowest unknown query key, naming it —
// sorted rather than range order, so which key gets named does not
// depend on Go's randomized map iteration. Before design doc 0064 an
// unrecognized key was a silent no-op on every endpoint but one.
//
// fm.* is exempt only for a caller that passes "fm." among allowed: an
// unrecognized fm.* key already 400s downstream, once the service checks
// it against the concept's own vocabulary (checkedFilter), which is why
// frontmatterFilter's keys are validated by content there rather than
// enumerated here — but only on the two operations that read a
// Frontmatter filter at all (search, context). Every other operation has
// no such downstream check, so fm.* left exempt there would be the same
// silent no-op this function exists to close (issue #409).
//
// The literal key "fm." — an empty frontmatter key — is never accepted,
// even where fm.* is exempt: it has "fm." as a prefix of itself, so the
// exemption above would wave it through, and it is also the sentinel a
// caller passes in allowed to grant that exemption, so a plain
// slices.Contains(allowed, key) would wave it through a second way. Both
// would hand it to frontmatterFilter, which drops it silently (name ==
// "") — the third silent no-op issue #409 found, once the first two were
// closed.
//
// This is what makes it safe to add a query parameter after the freeze —
// an old server 400s a caller that sends one early, rather than silently
// ignoring it the way ?dry_run= was ignored by a server that did not
// know it yet.
//
// A key sent more than once is refused the same way unless it is one of
// repeatableParams. The contract declares every other query key with a
// scalar schema, and there is no reading of ?limit=10&limit=50 that
// answers what was asked: the server used to take the first value for
// most keys and the last for fm.*, so the same contradiction got two
// different silent answers on the same request. Naming it is the rule §2
// applied to unknown keys, unknown body keys and out-of-mode keys; a
// repeated scalar is the fourth spelling of the same silence, and the
// last one that can still be closed (design doc 0064 §2, §20.2).
func rejectUnknownParams(q url.Values, allowed ...string) error {
	allowFM := slices.Contains(allowed, "fm.")
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name, isFM := strings.CutPrefix(key, "fm.")
		switch {
		case allowFM && isFM && name != "":
		case key != "fm." && slices.Contains(allowed, key):
		default:
			return service.Invalidf("unknown query parameter %q", key)
		}
		if len(q[key]) > 1 && !slices.Contains(repeatableParams, key) {
			return service.Invalidf(
				"query parameter %q was sent %d times; it takes one value, and the same key twice is a contradiction rather than a question",
				key, len(q[key]))
		}
	}
	return nil
}

// repeatableParams are the query keys the contract declares with an array
// schema, so repeating one is how a caller spells a set: ?type=Metric&type=Policy
// asks for either. Every other key is scalar and repeating it is a 400.
var repeatableParams = []string{"prefix", "status", "tag", "trust", "type"}

// bundleModeParams is history, limit and files: query keys the bundle GET
// handler declares for the address as a whole, but each is read in only
// some of its modes (archive, ?history, index.md, log.md, a concept or
// file's own address). rejectOutOfModeParams is what makes that precise —
// before design doc 0064 §2 item 2, a mode that did not read one of these
// silently ignored it instead of saying so.
//
// `cursor` joined them with the level listing (design doc 0101): it is
// read by one mode of this address, which is the property this list is
// for, and a parameter declared for the address has to be refused by name
// rather than as an unknown key everywhere else.
var bundleModeParams = []string{"history", "limit", "files", "cursor"}

// rejectOutOfModeParams 400s a bundleModeParams key present outside the
// mode that reads it, naming the mode — mode reads as "<key> is not
// meaningful <mode>", so it must fit that sentence (e.g. "with
// Accept: application/gzip", "on log.md"). allowed lists which of the
// three this mode does read; any other unrecognized key falls through to
// rejectUnknownParams's generic message.
func rejectOutOfModeParams(q url.Values, mode string, allowed ...string) error {
	for _, key := range bundleModeParams {
		if !q.Has(key) || slices.Contains(allowed, key) {
			continue
		}
		return service.Invalidf("%s is not meaningful %s", key, mode)
	}
	return rejectUnknownParams(q, allowed...)
}

// queryInt parses an optional numeric query parameter, rejecting a
// malformed value instead of silently treating it as unset (matching the
// MCP surface, where the JSON schema enforces types).
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

// mediaTypeHeader is a stored media type as a Content-Type header. Plain
// text without a charset invites browser guessing, and the sniffer only
// passes UTF-8/UTF-16 text through, so it says which.
func mediaTypeHeader(mediaType string) string {
	if mediaType == "text/plain" {
		return "text/plain; charset=utf-8"
	}
	return mediaType
}

// serveDefensively sets the headers that decide what a browser is
// allowed to do with bytes somebody uploaded (design doc 0046 §3.2).
//
// nosniff on everything: the media type is decided here, by sniffing the
// bytes, and a browser that overrides it would be deciding it again from
// a filename or a guess.
//
// What may render in place is an image, a PDF or plain text
// (domain.InlineServable). Anything else is handed over as a download,
// under a sandbox with no origin, so that a document which happens to
// carry script — an SVG, an HTML file — has nowhere to run even if it
// arrives here. The web UI is served from this same origin, so "nowhere
// to run" is the whole of the protection: an inline SVG would be script
// in the origin holding the reader's IAP session.
//
// This is the delivery half of "receive it, do not render it". The write
// half still refuses those media types (design doc 0013), and this does
// not depend on it: a row written before that rule, or one whose bytes
// sniff differently here than in a browser, is covered either way.
func serveDefensively(w http.ResponseWriter, mediaType, name string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disposition := "inline"
	if !domain.InlineServable(mediaType) {
		disposition = "attachment"
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	// The name is one path segment with no control characters
	// (domain.ValidFileName), so the only character left that could
	// end the quoted string early is the quote itself.
	w.Header().Set("Content-Disposition",
		disposition+`; filename="`+strings.ReplaceAll(name, `"`, `\"`)+`"`)
}
