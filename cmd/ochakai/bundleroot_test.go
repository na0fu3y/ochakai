package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bundle is ordinary content: cloned from somewhere, unpacked from an
// archive, handed over on a drive. Reading one must stay inside it.
//
// The link below is the whole attack: name it revenue.md, point it at
// something outside, and `ochakai import` used to read the target and
// write it to the knowledge base under that id — a file the importer
// never looked at, published to everyone the deployment serves. The walk
// declined to *descend* a symlinked directory and said nothing about
// reading a symlinked file, which is the half that reads something.
func TestImportDoesNotReadThroughALinkOutOfTheBundle(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "metrics.md"),
		[]byte("---\ntype: metrics\n---\n\nordinary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(bundle, "revenue.md")); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}

	files, err := readBundleDir(bundle)
	for _, data := range files {
		if strings.Contains(string(data), "PRIVATE KEY") {
			t.Fatal("readBundleDir read a file outside the bundle through a symlink")
		}
	}
	// Refusing the bundle outright is the honest answer: the reader asked
	// for a directory and part of it is not in the directory, and an
	// import that silently dropped concepts would be worse than one that
	// stopped. What must never happen is the read succeeding.
	if err == nil {
		t.Errorf("readBundleDir accepted a bundle holding a link out of it, returning %d files", len(files))
	}
}

// The same boundary on the way back out. filepath.IsLocal reads the name
// in the archive; it cannot see that a directory already on disk is a
// link somewhere else, which is what this leaves to the kernel.
func TestExportDoesNotWriteThroughALinkOutOfTheDirectory(t *testing.T) {
	outside := t.TempDir()
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "metrics")); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("---\ntype: metrics\n---\n\nplanted\n")
	// A perfectly local name: nothing about it says where "metrics"
	// actually leads.
	if err := tw.WriteHeader(&tar.Header{
		Name: "metrics/revenue.md", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := extractTarGz(dir, bytes.NewReader(buf.Bytes())); err == nil {
		t.Error("extractTarGz followed a symlinked directory out of the export directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "revenue.md")); err == nil {
		t.Error("extractTarGz wrote a file outside the directory it was given")
	}
}

// The ordinary bundle still imports, links or no links: the guard is
// about leaving the directory, not about being careful.
func TestImportReadsAnOrdinaryBundle(t *testing.T) {
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"metrics/revenue.md": "---\ntype: metrics\n---\n\n売上\n",
		"glossary/term.md":   "---\ntype: glossary\n---\n\n用語\n",
		".git/config":        "[core]\n", // skipped, as it always was
	} {
		full := filepath.Join(bundle, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := readBundleDir(bundle)
	if err != nil {
		t.Fatalf("readBundleDir: %v", err)
	}
	// Slash-separated relative paths, which is what the bundle address
	// space is (design doc 0075) — not the host's separator.
	for _, want := range []string{"metrics/revenue.md", "glossary/term.md"} {
		if _, ok := files[want]; !ok {
			t.Errorf("%s missing from %v", want, keysOf(files))
		}
	}
	if _, ok := files[".git/config"]; ok {
		t.Error(".git/config was read: dot-directories are not part of a bundle")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
