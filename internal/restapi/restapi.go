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
	// list, the attachment metadata and the concepts are read at
	// different moments, and a write landing between them produces an
	// archive that disagrees with its own index — an index.md naming a
	// file that is not there, a concept that moved appearing twice or
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
	rows, err := snap.IndexRows(r.Context(), prefix)
	if err != nil {
		writeError(w, err)
		return
	}
	var atts []domain.Attachment
	if withAttachments {
		// Metadata only; bytes are pulled one file at a time below.
		if atts, err = snap.FileMeta(r.Context(), prefix); err != nil {
			writeError(w, err)
			return
		}
	}
	// The generated index.md files list the concepts and the files that
	// sit beside them (design doc 0046 §3.7). With ?attachments=false
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
		// An archive is the bundle at a path: everything under it, as
		// OKF, in the layout it lives at (design doc 0046 §3.5). The
		// root is the whole knowledge base, which is what
		// GET /api/v1/export was. Paths inside the archive are not
		// re-rooted — an export of metrics/ imports back to metrics/,
		// because this is a copy of a subtree and not a move of one.
		if wantsArchive(r) {
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
		if r.URL.Query().Has("history") {
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
					if wantsDocument(r) {
						writeDocument(w, http.StatusOK, k)
					} else {
						writeView(w, http.StatusOK, k)
					}
					return
				}
				// A markdown path with no concept at it may still be a
				// file: a document that carries no type is not a concept
				// and is kept as a file (design doc 0046 §3.2).
				if !errors.Is(err, store.ErrNotFound) {
					writeError(w, err)
					return
				}
			}
			att, data, err := svc.GetFile(r.Context(), path)
			if err != nil {
				writeError(w, missingObject(err, isMarkdown))
				return
			}
			w.Header().Set("ETag", `"`+att.SHA256+`"`)
			w.Header().Set("Cache-Control", "private, no-cache")
			if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, att.SHA256) {
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
		mux.HandleFunc(m+" /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
			path := r.PathValue("path")
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
			// A .md that carries no type is not a concept (SPEC §11
			// requires the key), and it is not an error either: it is a
			// file that happens to be markdown, and a bundle that carried
			// one gets it back (design doc 0046 §3.2).
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
				err = store.ErrNotFound
				switch {
				case isMarkdown && purge:
					err = svc.Purge(r.Context(), id, actor)
				case isMarkdown:
					err = svc.Delete(r.Context(), id, actor)
				}
				if errors.Is(err, store.ErrNotFound) {
					err = missingObject(svc.DeleteFile(r.Context(), path, actor), isMarkdown)
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
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
					"error": "a concept is written as an OKF document (Content-Type: text/markdown), " +
						"not as JSON: the frontmatter is the metadata and the markdown is the body",
				})
				return
			}
			body, ok := readBody(w, r, maxDocument, fmt.Sprintf("object exceeds %d bytes", maxDocument))
			if !ok {
				return
			}
			if isMarkdown && okf.CarriesType(body) {
				putEntry(w, r, svc, id, body, actor, dry)
				return
			}
			var (
				att              *domain.Attachment
				created, changed bool
			)
			if dry {
				att, created, changed, err = svc.PlanFile(r.Context(), path, body)
			} else {
				att, err = svc.PutFile(r.Context(), path, body, actor)
			}
			if err != nil {
				writeError(w, err)
				return
			}
			if dry {
				w.Header().Set(planHeader, planOf(created, changed))
			}
			writeJSON(w, http.StatusOK, att)
		})
	}

	// GET /api/v1/context?q=...&type=...&status=...&tag=...&prefix=...&limit=...&budget=...
	// The one-call read before answering a data question: full concepts
	// behind the top hits, expanded one hop through links.
	//
	// prefix scopes the search that picks those hits, not the link
	// expansion: a scoped concept citing a glossary term outside the scope
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
			Limit: limit, Budget: budget,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

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
	// usage, and attachments along and rewrites inbound references (link
	// targets, attrs.model) so nothing breaks (design doc 0021). Its own
	// top-level path for the same reason /usage and /review have one:
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

	// POST /api/v1/reembed?limit=N&cursor=... — fill in vectors for
	// concepts and attachments that have none for the configured model.
	// The response's cursor feeds the next call: a pass whose concepts all
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

	return announceReadOnly(svc, stdlibDefaultsAsJSON(mux))
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
	msg := "not found"
	if status == http.StatusMethodNotAllowed {
		msg = "method not allowed"
	}
	body, _ := json.Marshal(map[string]string{"error": msg})
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
	if err := svc.FillAttachments(r.Context(), ptrs); err != nil {
		writeError(w, err)
		return
	}
	for i := range page.Hits {
		page.Hits[i].Attachments = ptrs[i].Attachments
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

// writeObjectHistory answers ?history: the log.md of the one object at
// this path, as the document (SPEC §9) or — with an explicit JSON
// Accept — its ledger rows, each carrying the concept as it stood after
// the change. The documents are the reason this representation exists:
// a diff between two revisions is a text diff, which is what
// `ochakai revisions --json` and the web UI's history panel are for.
//
// A file's revisions carry no document, because a file has none. Its
// history is the record that it was written and removed, which is the
// whole of what happened.
func writeObjectHistory(w http.ResponseWriter, r *http.Request, svc *service.Service, path string) {
	limit, err := queryInt(r.URL.Query(), "limit")
	if err != nil {
		writeError(w, err)
		return
	}
	// A concept's revisions carry the document it was; asked as JSON,
	// that is the point of the representation — a diff between two
	// revisions is a text diff. A file's carry none, because a file has
	// none, so its rows are the shape a directory's history has.
	//
	// Which of the two this is cannot be read off the path: a markdown
	// document that carries no type is a *file* (design doc 0046 §3.2),
	// and it lives at a ".md" path like any other. So the concept ledger
	// is asked first and its ErrNotFound falls through to the object's —
	// keyed by path, which is the one key both kinds have. Routing on the
	// suffix alone answered 404 as JSON at an address whose markdown
	// listed the history fine, which is the one thing two representations
	// of one address may not do.
	if id, isMarkdown := strings.CutSuffix(path, ".md"); isMarkdown && wantsJSON(r) {
		revs, err := svc.Revisions(r.Context(), id, limit)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
			return
		}
		if !errors.Is(err, store.ErrNotFound) {
			writeError(w, err)
			return
		}
	}
	rows, err := svc.ObjectHistory(r.Context(), path, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, changeLog{Changes: asChanges(rows)})
		return
	}
	writeMarkdown(w, service.RenderObjectLog(path, rows))
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
	writeJSON(w, http.StatusConflict, map[string]string{
		"error": fmt.Sprintf("%s is generated from the bundle, not stored in it (design doc 0046 §§3.7-3.8), %s",
			base, because),
	})
	return true
}

// refuseObjectAsArchive answers 409 when path names one object — a
// concept at its own <id>.md or a file at its own path — rather than a
// directory nothing lives under, and reports whether it did. It goes
// straight to the store rather than through svc.Get: this is a check of
// whether the address is a subtree, not a read of the object living
// there, and must not count as a fetch or fault on missing attachments.
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
	writeJSON(w, http.StatusConflict, map[string]string{
		"error": fmt.Sprintf(
			"%s is an object, not a directory (design doc 0046 §3.5); there is no subtree here to archive, ask the directory it sits in", path),
	})
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

// wantsDocument reports whether the caller asked for the concept as a
// document rather than as JSON. Only an explicit markdown Accept counts:
// a browser sends */* and wants the JSON every existing client reads.
func wantsDocument(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/markdown")
}

// wantsArchive reports whether the caller asked for a path as an OKF
// bundle rather than as one object. Explicit only, like the other two: a
// browser sending */* is asking to look at something, not to download
// the knowledge base under it.
func wantsArchive(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/gzip")
}

// onlyIfAbsent reports an If-None-Match "*" precondition: write only if
// the id is free. Any other value is ignored — ochakai has no use for a
// conditional read, and a caller that means "only if absent" says "*".
func onlyIfAbsent(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("If-None-Match")) == "*"
}

// putEntry writes one document as the concept at id and answers with the
// read of it. Two addresses lead here — the concept surface and the bundle
// path the concept lives at (design doc 0046 §3.5) — and a concept
// written through either is written the same way, with the same
// preconditions and the same answer. An address is not a second surface.
func putEntry(w http.ResponseWriter, r *http.Request, svc *service.Service,
	id string, body []byte, actor domain.Actor, dry bool,
) {
	d, notes, err := okf.Parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		out, created, changed, err = svc.Plan(r.Context(), k, actor, parseIfMatch(r))
	case onlyIfAbsent(r):
		out, err = svc.Create(r.Context(), k, actor)
		created, changed = true, true
	default:
		out, created, changed, err = svc.Put(r.Context(), k, actor, parseIfMatch(r))
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
	// A dry run has nothing to report about a version that was not
	// written: no ETag, because there is no state to precondition on, and
	// no 201, because nothing was created. What it has is the plan.
	if dry {
		w.Header().Set(planHeader, planOf(created, changed))
		if wantsDocument(r) {
			writeDocument(w, http.StatusOK, out)
			return
		}
		writeView(w, http.StatusOK, out)
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
}

// Ochakai-Plan is what ?dry_run=true answers with: the one word the write
// would have been (design doc 0061). It replaces Ochakai-Unchanged on a
// dry run rather than joining it — "unchanged" is one of the three values,
// and two headers saying overlapping things is how a client ends up
// reading only one of them.
const planHeader = "Ochakai-Plan"

const (
	planCreated   = "created"
	planUpdated   = "updated"
	planUnchanged = "unchanged"
)

func planOf(created, changed bool) string {
	switch {
	case created:
		return planCreated
	case !changed:
		return planUnchanged
	default:
		return planUpdated
	}
}

// writeView responds with a read of one concept: the canonical document,
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
		// The If-Match precondition failed: the concept changed since read.
		status = http.StatusPreconditionFailed
	case errors.Is(err, store.ErrAlreadyExists), errors.Is(err, store.ErrNotDeleted):
		status = http.StatusConflict
	case errors.As(err, &inputErr):
		status = http.StatusBadRequest
	case errors.As(err, &unsupportedErr):
		status = http.StatusNotImplemented
	}
	if status == http.StatusInternalServerError {
		// Every status above is a message a caller is meant to read; this
		// one is whatever the store or a driver said, which can carry SQL
		// text or a connection string. Logged for an operator instead of
		// put on the wire.
		slog.Default().Error("unhandled REST error", "error", err)
		writeJSON(w, status, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// missingObject reports what a read or delete that fell through to the
// file half should say about a markdown path.
//
// A `.md` path is a concept's address first (design doc 0046 §3.5); the
// file lookup behind it is §3.2's accommodation for a markdown document
// that carries no type. When this instance holds no files at all — no
// GCS (design doc 0013) — that accommodation is simply unavailable, and
// answering 501 would reply to a question about a concept with news
// about the deployment. There is no object at the path: 404.
//
// It matters because the bundle path is now a concept's only address:
// without this, every 404 for a soft-deleted or never-written concept
// became a 501 on an instance without GCS, which is most of them.
// A path that is not markdown keeps the 501 — there, "this instance
// cannot hold files" is exactly the answer to what was asked.
func missingObject(err error, markdown bool) error {
	var unsupported *service.UnsupportedError
	if markdown && errors.As(err, &unsupported) {
		return store.ErrNotFound
	}
	return err
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

// parseIfMatch reads an optional If-Match precondition. Absent or "*"
// means no version precondition (the update still requires the concept to
// exist). Any other value is taken as a concept ETag, quoted or not: it is
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
	// (domain.ValidAttachmentName), so the only character left that could
	// end the quoted string early is the quote itself.
	w.Header().Set("Content-Disposition",
		disposition+`; filename="`+strings.ReplaceAll(name, `"`, `\"`)+`"`)
}
