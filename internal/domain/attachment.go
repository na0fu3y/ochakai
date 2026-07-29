package domain

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Attachment is a file attached to a knowledge entry (design docs 0008,
// 0013). Attachments never stand alone: search and get return the entry —
// body text first, attachment metadata alongside — and the bytes are
// fetched on demand, so an agent's context is never flooded with base64.
// Bytes are content-addressed by SHA-256 and immutable.
type Attachment struct {
	Name      string `json:"name"` // filename, unique within the entry
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	// Path is where the file lives in the bundle — its address, and the
	// whole of what "where it came from" used to mean. A file imported
	// from a foreign layout is at the path it arrived at, and one born
	// here is at "<id>/<name>"; okf_path reported the difference between
	// those two cases, which the path itself states (design doc 0046
	// §3.3). Name is the last segment, kept beside it the way a concept
	// keeps its id beside its path: derived, and what every surface
	// calls the file.
	Path      string    `json:"path"`
	CreatedBy Actor     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// MaxAttachmentSize bounds one file (screenshots, diagrams, seed files —
// not originals).
//
// There is no bound on how many files an entry may carry. There was one,
// of 20, from the days when a file was something an entry *had*: a cap
// on a property of an entry. A file is an object in the bundle now
// (design doc 0046 §§2.1, 3.3), and a cap on how many objects may sit in
// a directory is a cap on what a bundle may contain — which is not
// ochakai's to set, any more than it sets how many entries a directory
// may hold. A bundle whose files come back missing is not a bundle that
// round-trips (§3.2).
const MaxAttachmentSize = 5 << 20

// embeddableMediaTypes is what the file-embedding model takes as input
// (gemini-embedding-2, design doc 0020). Anything else is stored and
// served like everything else and is findable by its name alone — the
// list decides what is worth a call to the embedder, not what a bundle
// may hold.
var embeddableMediaTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/webp":      true,
	"application/pdf": true,
	"text/plain":      true,
}

// Embeddable reports whether a file of this media type can be handed to
// the embedding model. A file that cannot is not a lesser file: it is
// stored, served and exported the same, and found by its name.
func Embeddable(mediaType string) bool {
	mt, _, _ := strings.Cut(mediaType, ";")
	return embeddableMediaTypes[strings.ToLower(strings.TrimSpace(mt))]
}

// InlineServable reports whether bytes of this media type may be handed
// to a browser to render in place. Images (bar SVG), PDFs and plain text
// may; everything else is served as a download instead (design doc 0046
// §3.2).
//
// This is the whole of the protection now. The write side accepts any
// media type — a bundle whose files come back missing is not a bundle
// that round-trips — so "receive it, do not render it" rests entirely on
// this half. Nothing upstream will have refused the bytes on the way in.
//
// SVG is the case worth naming. It is an image by every convention and
// an executable document by specification, so it is served the way an
// executable document has to be: never inline.
func InlineServable(mediaType string) bool {
	mt, _, _ := strings.Cut(mediaType, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	if mt == "image/svg+xml" || mt == "text/html" {
		return false
	}
	return strings.HasPrefix(mt, "image/") || mt == "application/pdf" || mt == "text/plain"
}

// ValidAttachmentName reports whether name can be an attachment filename:
// one path segment (so it embeds in bundle paths and URLs unchanged, with
// the same path-safety-only character rule as ID segments, design doc
// 0019), not markdown — a ".md" attachment could masquerade as a concept
// document in an exported bundle (index.md / log.md fall out of the same
// rule).
func ValidAttachmentName(name string) bool {
	return validSegment(name) && !strings.HasSuffix(strings.ToLower(name), ".md")
}

// DetectAttachmentMediaType is the media type of these bytes, as
// sniffed. The client's declared type is never trusted; bytes decide.
//
// Nothing is refused. It used to be: an allowlist of five types stood
// here, on the reasoning that a knowledge store should hold only what an
// agent could read (design doc 0013). What that reasoning missed is that
// ochakai holds a *bundle*, and a bundle whose files come back missing
// is not the bundle that was handed over (design doc 0046 §3.2). A
// producer's zip of seed data, a `.parquet`, an SVG diagram — refusing
// them does not keep them out of the knowledge base, it keeps them out
// of the copy ochakai hands back.
//
// The error the allowlist prevented is prevented on delivery instead,
// which is where it belongs: what a browser is allowed to do with bytes
// is decided when they are served, by InlineServable and the headers
// beside it. "Receive it, do not render it" is one rule with two halves,
// and this is the half that receives.
//
// The signature still decides what the type *is*, so a file that only
// claims to be an image is stored as what it sniffs as, never as what it
// was called.
func DetectAttachmentMediaType(data []byte) (string, error) {
	mt, _, _ := strings.Cut(http.DetectContentType(data), ";")
	mt = strings.TrimSpace(mt)
	if mt == "" {
		return "application/octet-stream", nil
	}
	return mt, nil
}

// ValidateAttachment checks the parts of an attachment writers control.
func ValidateAttachment(name string, size int) error {
	if !ValidAttachmentName(name) {
		return fmt.Errorf("invalid attachment name %q (one filename segment, not *.md)", name)
	}
	if size == 0 {
		return fmt.Errorf("attachment is empty")
	}
	if size > MaxAttachmentSize {
		return fmt.Errorf("attachment exceeds %d MiB", MaxAttachmentSize>>20)
	}
	return nil
}

// ValidBundlePath reports whether p can address an object in the bundle:
// path segments separated by "/", each one a valid filename, and neither
// of the two names OKF reserves per directory (index.md, log.md — they
// are generated from the bundle rather than stored in it, design doc
// 0046 §§3.7-3.8).
//
// It is ValidID's rule one level down: an id is a path with ".md"
// removed, so what makes a path addressable is what makes an id
// addressable, minus the assumption that every object is a concept.
func ValidBundlePath(p string) bool {
	if p == "" || len(p) > 512 {
		return false
	}
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if !validSegment(s) {
			return false
		}
	}
	return !ReservedBundleName(segs[len(segs)-1])
}

// ReservedBundleName reports whether a filename is one OKF reserves per
// directory: index.md and log.md, which ochakai generates from the
// bundle rather than storing (design doc 0046 §§3.7-3.8). It is the one
// place that decides — a reservation answered differently in two places
// is a path one half accepts and the other refuses.
//
// Exactly those two spellings, as SPEC §3.1 writes them. Folding case
// looks like the safer reading and is not: "Index.md" is a file OKF says
// nothing about, and refusing it would drop it from a bundle that
// carried it (design doc 0046 §3.2) while ValidID went on accepting the
// concept id "Index" — the address would take a concept and refuse a
// file. Two objects whose paths differ only by case do collide when a
// bundle is extracted on a case-insensitive filesystem, but that is true
// of "Revenue.md" beside "revenue.md" as well: it is a property of
// bundles, not of these two names, and the place to notice it is the
// export rather than here.
func ReservedBundleName(name string) bool {
	return name == "index.md" || name == "log.md"
}
