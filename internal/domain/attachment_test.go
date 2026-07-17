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
	gif := append([]byte("GIF89a"), make([]byte, 16)...)
	if mt, err := DetectAttachmentMediaType(gif); err != nil || mt != "image/gif" {
		t.Errorf("gif: got %q, %v", mt, err)
	}
	// SVG sniffs as XML/text — must be refused (script-bearing).
	if _, err := DetectAttachmentMediaType([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); err == nil {
		t.Error("svg accepted")
	}
	if _, err := DetectAttachmentMediaType([]byte("just text")); err == nil {
		t.Error("plain text accepted")
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
