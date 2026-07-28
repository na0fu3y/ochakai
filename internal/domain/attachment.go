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
	// OKFPath is the bundle path a foreign import carried this file at;
	// export writes it back there so body links keep working. Empty for
	// attachments born in ochakai (exported to "<type>/<id>/<name>").
	OKFPath   string    `json:"okf_path,omitempty"`
	CreatedBy Actor     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// MaxAttachmentSize bounds one attachment (screenshots, diagrams, seed
// files — not originals). MaxAttachmentsPerEntry bounds how many an entry
// may carry.
const (
	MaxAttachmentSize      = 5 << 20
	MaxAttachmentsPerEntry = 20
)

// attachmentMediaTypes is the allowlist of what an attachment may be
// (design doc 0013): exactly the intersection of what Claude accepts as
// an upload and what gemini-embedding-2 takes as input — gif, in the
// original image-only list (design doc 0008), is not embeddable and was
// dropped. Never text/html or image/svg+xml: both can carry scripts, and
// serving them from the API would hand every knowledge author an XSS
// vector into web UIs. Note that sniffing decides — CSV/JSON/markup
// without an HTML signature all pass as text/plain and are always served
// as such, never as something executable.
var attachmentMediaTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/webp":      true,
	"application/pdf": true,
	"text/plain":      true,
}

// InlineServable reports whether bytes of this media type may be handed
// to a browser to render in place. Images (bar SVG), PDFs and plain text
// may; everything else is served as a download instead (design doc 0046
// §3.2).
//
// The rule is on the delivery side because that is where it belongs. A
// write-time allowlist decides what a knowledge base may hold, and the
// two questions are not the same one: 0046 §3.2 stops refusing media
// types, on the grounds that a bundle whose files come back missing is
// not a bundle that round-trips — "receive it, do not render it". Even
// while the write side still refuses (design doc 0013), this is the
// check that has to be true: a row written before that allowlist, or a
// sniffer that reads one byte differently than a browser does, is a
// script in the web UI's origin if the only guard is upstream.
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

// DetectAttachmentMediaType sniffs data's media type and checks it against
// the allowlist. The client's declared type is never trusted; bytes decide.
func DetectAttachmentMediaType(data []byte) (string, error) {
	mt, _, _ := strings.Cut(http.DetectContentType(data), ";")
	mt = strings.TrimSpace(mt)
	if !attachmentMediaTypes[mt] {
		return "", fmt.Errorf("unsupported attachment content %q (allowed: png, jpeg, webp, pdf, plain text)", mt)
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
