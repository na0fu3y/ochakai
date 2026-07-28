package domain

import (
	"strings"
	"testing"
)

func TestValidAttachmentName(t *testing.T) {
	for name, want := range map[string]bool{
		"weekly.png":             true,
		"er-diagram.webp":        true,
		"UPPER_case.01.gif":      true,
		"weekly":                 true, // extension-less is fine; bytes decide the type
		"":                       false,
		"a/b.png":                false, // one segment only
		".hidden.png":            false, // leading dot (hidden-path rule)
		"notes.md":               false, // could masquerade as a concept document
		"INDEX.MD":               false,
		strings.Repeat("a", 200): false,
	} {
		if got := ValidAttachmentName(name); got != want {
			t.Errorf("ValidAttachmentName(%q) = %v, want %v", name, got, want)
		}
	}
}

// Bytes decide what a file is, and nothing is refused for being what it
// is (design doc 0046 §3.2): a bundle whose files come back missing is
// not the bundle that was handed over. What a browser may do with them
// is decided on the way out, by InlineServable.
func TestDetectAttachmentMediaType(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	mustSniff(t, png, "image/png")
	mustSniff(t, append([]byte("GIF89a"), make([]byte, 16)...), "image/gif")
	mustSniff(t, []byte("%PDF-1.7 fake pdf body"), "application/pdf")
	mustSniff(t, []byte("seed,urls\nhttps://example.com\n"), "text/plain")
	// The two that carry script are stored as what they are, and never
	// served inline — the delivery rule is what stands between them and
	// the web UI's origin.
	for _, tc := range []struct{ data, mediaType string }{
		{`<!DOCTYPE html><script>alert(1)</script>`, "text/html"},
		{`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`, "text/xml"},
	} {
		mt := mustSniff(t, []byte(tc.data), tc.mediaType)
		if InlineServable(mt) {
			t.Errorf("%s is served inline", mt)
		}
	}
	// An archive, a font, anything at all: stored as what it sniffs as.
	mustSniff(t, append([]byte("PK\x03\x04"), make([]byte, 16)...), "application/zip")
	mustSniff(t, []byte{0x00, 0x01, 0x02, 0x03}, "application/octet-stream")
}

func mustSniff(t *testing.T, data []byte, want string) string {
	t.Helper()
	mt, err := DetectAttachmentMediaType(data)
	if err != nil {
		t.Errorf("sniffing %q: %v", want, err)
	}
	if mt != want {
		t.Errorf("sniffed %q, want %q", mt, want)
	}
	return mt
}

func TestValidateAttachment(t *testing.T) {
	if err := ValidateAttachment("ok.png", 100); err != nil {
		t.Errorf("valid attachment refused: %v", err)
	}
	if err := ValidateAttachment("ok.png", 0); err == nil {
		t.Error("empty attachment accepted")
	}
	if err := ValidateAttachment("ok.png", MaxAttachmentSize+1); err == nil {
		t.Error("oversized attachment accepted")
	}
	if err := ValidateAttachment("bad.md", 100); err == nil {
		t.Error(".md attachment accepted")
	}
}

// What a browser may render in place, and what it may only be handed
// (design doc 0046 §3.2). The write path still refuses most of these
// media types (design doc 0013); the delivery rule must not depend on
// that, because a row written before the rule — or bytes that sniff
// differently in a browser than they did here — reaches this function
// either way.
func TestInlineServable(t *testing.T) {
	for _, tc := range []struct {
		mediaType string
		inline    bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/webp", true},
		{"image/gif", true}, // not writable, but nothing executable about it
		{"application/pdf", true},
		{"text/plain", true},
		{"text/plain; charset=utf-8", true},
		{"TEXT/PLAIN", true},
		// The two that carry script, and the reason this rule exists: an
		// SVG is an image by every convention and a document by spec.
		{"image/svg+xml", false},
		{"IMAGE/SVG+XML", false},
		{"image/svg+xml; charset=utf-8", false},
		{"text/html", false},
		{"application/xhtml+xml", false},
		{"application/zip", false},
		{"application/octet-stream", false},
		{"", false},
	} {
		if got := InlineServable(tc.mediaType); got != tc.inline {
			t.Errorf("InlineServable(%q) = %v, want %v", tc.mediaType, got, tc.inline)
		}
	}
}
