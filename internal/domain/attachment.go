package domain

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Attachment is an image attached to a knowledge entry (design doc 0008).
// Images never stand alone: search and get return the entry — body text
// first, attachment metadata alongside — and the bytes are fetched on
// demand, so an agent's context is never flooded with base64. Bytes are
// content-addressed by SHA-256 and immutable; Data is only populated when
// the attachment itself is fetched.
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

// MaxAttachmentSize bounds one attachment (screenshots, diagrams — not
// originals). MaxAttachmentsPerEntry bounds how many an entry may carry.
const (
	MaxAttachmentSize      = 5 << 20
	MaxAttachmentsPerEntry = 20
)

// attachmentMediaTypes is the allowlist of what an attachment may be.
// Images only, and no SVG: SVG can carry scripts, and serving it from the
// API would hand every knowledge author an XSS vector into web UIs.
var attachmentMediaTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// ValidAttachmentName reports whether name can be an attachment filename:
// one path segment (so it embeds in bundle paths and URLs unchanged), not
// markdown — a ".md" attachment could masquerade as a concept document in
// an exported bundle (index.md / log.md fall out of the same rule).
func ValidAttachmentName(name string) bool {
	return segmentRe.MatchString(name) && !strings.HasSuffix(strings.ToLower(name), ".md")
}

// DetectAttachmentMediaType sniffs data's media type and checks it against
// the allowlist. The client's declared type is never trusted; bytes decide.
func DetectAttachmentMediaType(data []byte) (string, error) {
	mt, _, _ := strings.Cut(http.DetectContentType(data), ";")
	mt = strings.TrimSpace(mt)
	if !attachmentMediaTypes[mt] {
		return "", fmt.Errorf("unsupported attachment content %q (allowed: png, jpeg, gif, webp)", mt)
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
