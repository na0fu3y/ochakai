package domain

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

// A file is an object in the bundle that is not a concept: the seeds file
// beside a table entry, the dashboard screenshot an insight explains, the
// PDF a policy quotes — and, just as much, a `.md` with no `type` and a
// file nothing links to. Design doc 0046 §3.2 makes the rule one line:
// what goes into a bundle comes out of it.
//
// That replaces the attachment model of design docs 0008 and 0013, where
// a file was a row belonging to an entry, addressed as (entry, name), and
// anything fitting neither shape was dropped on import. A file is now
// addressed the way everything else in a bundle is — by its path — and
// which entry it belongs to is *derived* from the bodies that link to it
// (§3.3), the way links themselves already are (design doc 0024).
//
// What survives from 0008 is the framing: a bundle carries evidence, not
// originals, and a file never stands alone as a result. ochakai still
// never interprets one — no OCR, no PDF text extraction, no captioning.
// Reading a file and writing what it says into a body is the client
// agent's job, like every other interpretation (design doc 0001).
type File struct {
	// Path is the address: the bundle path, extension included.
	Path      string    `json:"path"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedBy Actor     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Name is the file's last path segment, which is how a reader refers to
// it — "weekly.png", not "insights/revenue/weekly.png".
func (f *File) Name() string { return path.Base(f.Path) }

// MaxFileSize bounds one object's bytes.
//
// The per-entry count limit is gone with the attachment model: it bounded
// a stored relationship that no longer exists, since which entry a file
// belongs to is derived from what links to it (design doc 0046 §3.2).
const MaxFileSize = 5 << 20

// DetectMediaType sniffs what data is. The client's declared type is
// never trusted — bytes decide (design doc 0013's rule, kept).
//
// Nothing is refused. The allowlist 0013 drew — png, jpeg, webp, pdf,
// plain text — was a write-time gate, and a write-time gate cannot
// survive design doc 0046 §3.2: a bundle whose files ochakai refuses is a
// bundle that does not round-trip, which is the defect the record exists
// to fix. The danger the allowlist was drawn against — script-carrying
// content served back to a browser — is real, and is answered where it
// happens, in RenderableInline below.
func DetectMediaType(data []byte) string {
	mt, _, _ := strings.Cut(http.DetectContentType(data), ";")
	return strings.TrimSpace(mt)
}

// inlineMediaTypes are the types a browser may render in place: the
// intersection design doc 0013 chose, for the reason it chose it —
// Claude reads them and gemini-embedding-2 embeds them — and none of
// them can carry script.
var inlineMediaTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/webp":      true,
	"application/pdf": true,
	"text/plain":      true,
}

// RenderableInline reports whether a media type may be served for a
// browser to display. Everything else is served as a download and never
// rendered: Content-Disposition: attachment, X-Content-Type-Options:
// nosniff, and a sandbox policy — so an image/svg+xml or a text/html a
// knowledge author put in the bundle is bytes a reader can fetch, and
// never a script a browser runs (design doc 0046 §3.2: accept the file,
// refuse to render it).
func RenderableInline(mediaType string) bool { return inlineMediaTypes[mediaType] }

// Embeddable reports whether a file's bytes can go to the embedder as
// they are (design doc 0020). It is the same list as RenderableInline
// for the same underlying reason — it is what gemini-embedding-2 takes —
// and it is a separate function because the two questions are separate:
// one is about a browser, the other about a model.
func Embeddable(mediaType string) bool { return inlineMediaTypes[mediaType] }

// ValidFilePath reports whether p can address a file in the bundle: the
// path rule ids already follow (design doc 0019 §2), and not one of the
// two names OKF reserves, which are generated (design doc 0046 §§3.7-3.8).
//
// A `.md` path is allowed. A markdown file with no usable frontmatter is
// not a concept — SPEC §11 says a conformant bundle's non-reserved `.md`
// files all carry a type, and a bundle that breaks that rule is one a
// consumer must still accept without dropping anything (§3.2). Which of
// the two a `.md` write becomes is decided by reading it, not by the
// surface it arrived on.
func ValidFilePath(p string) bool {
	if !ValidID(p) {
		return false
	}
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	return base != "index.md" && base != "log.md"
}

// ValidateFile checks what a writer controls about an object.
func ValidateFile(p string, size int) error {
	switch {
	case !ValidFilePath(p):
		return fmt.Errorf("invalid file path %q (path segments separated by \"/\"; segments must not start with \".\", and index.md and log.md are generated)", p)
	case size == 0:
		return fmt.Errorf("file is empty")
	case size > MaxFileSize:
		return fmt.Errorf("file exceeds %d MiB", MaxFileSize>>20)
	}
	return nil
}

// FilesOf returns the files belonging to an entry, derived rather than
// stored (design doc 0046 §3.3). Two rules, in order:
//
//  1. what the entry's body links to — attribution by reference, so any
//     producer's layout works (design doc 0008 §3.4);
//  2. what sits directly in the entry's own "<id>/" namespace, the
//     canonical layout an export writes (design doc 0013 §2.3).
//
// Both were already how an import decided which entry a file belonged
// to. What changes is that the answer is no longer written down: a file
// is at a path, and belonging is a question asked of the bundle as it
// stands.
func FilesOf(id, body string, files []File) []File {
	linked := map[string]bool{}
	for _, target := range FileLinksFromBody(id, body) {
		linked[target] = true
	}
	ns := id + "/"
	var out []File
	for _, f := range files {
		inNamespace := strings.HasPrefix(f.Path, ns) && !strings.Contains(f.Path[len(ns):], "/")
		if linked[f.Path] || inNamespace {
			out = append(out, f)
		}
	}
	return out
}
