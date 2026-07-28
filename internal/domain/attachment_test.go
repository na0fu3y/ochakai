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

func TestDetectAttachmentMediaType(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	if mt, err := DetectAttachmentMediaType(png); err != nil || mt != "image/png" {
		t.Errorf("png: got %q, %v", mt, err)
	}
	// gif fell out of the allowlist with the grandfather clause (Claude
	// reads it, gemini-embedding-2 cannot embed it).
	gif := append([]byte("GIF89a"), make([]byte, 16)...)
	if _, err := DetectAttachmentMediaType(gif); err == nil {
		t.Error("gif accepted")
	}
	if mt, err := DetectAttachmentMediaType([]byte("%PDF-1.7 fake pdf body")); err != nil || mt != "application/pdf" {
		t.Errorf("pdf: got %q, %v", mt, err)
	}
	if mt, err := DetectAttachmentMediaType([]byte("seed,urls\nhttps://example.com\n")); err != nil || mt != "text/plain" {
		t.Errorf("text: got %q, %v", mt, err)
	}
	// Markup with an HTML signature sniffs as text/html — refused
	// (script-bearing; design doc 0013).
	if _, err := DetectAttachmentMediaType([]byte(`<!DOCTYPE html><script>alert(1)</script>`)); err == nil {
		t.Error("html accepted")
	}
	// An XML declaration sniffs as text/xml (SVG's usual shape) — refused.
	if _, err := DetectAttachmentMediaType([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`)); err == nil {
		t.Error("xml/svg accepted")
	}
	// SVG without the XML declaration has no HTML signature and passes as
	// text/plain — stored and served inert, never as image/svg+xml.
	if mt, err := DetectAttachmentMediaType([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); err != nil || mt != "text/plain" {
		t.Errorf("bare svg: got %q, %v (want inert text/plain)", mt, err)
	}
	// Binary that is no allowlisted format sniffs application/octet-stream.
	if _, err := DetectAttachmentMediaType([]byte{0x00, 0x01, 0x02, 0x03}); err == nil {
		t.Error("arbitrary binary accepted")
	}
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
