package restapi

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestRESTIntegrationKeepsWhatAnIndexSaysOfItsSubdirectories pins
// decision 0154: a PUT of a directory's index.md keeps the sentence it
// writes about each subdirectory, and the index ochakai generates carries
// it from then on. The listing itself stays generated.
func TestRESTIntegrationKeepsWhatAnIndexSaysOfItsSubdirectories(t *testing.T) {
	srv, _ := newIntegrationServer(t)
	dir := testdb.Unique(t, "restdirdesc")
	id := dir + "/sales/revenue"
	removeEntries(t, srv, id)
	resp := putDoc(t, srv.URL, id, []byte("---\ntype: Metric\n---\n\nbody\n"), false)
	resp.Body.Close()

	put := func(index string, dry bool) (int, string, string) {
		t.Helper()
		u := srv.URL + "/api/v1/bundle/" + dir + "/index.md"
		if dry {
			u += "?dry_run=true"
		}
		req, err := http.NewRequest(http.MethodPut, u, strings.NewReader(index))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "text/markdown")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Ochakai-Plan"), string(body)
	}
	index := func() string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/v1/bundle/" + dir + "/index.md")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	// A producer's listing: its own grouping heading, a subdirectory
	// with a description, and a concept line that is not read.
	producer := "# Subdirectories\n\n* [sales](sales/index.md) - Revenue, orders and returns by channel\n" +
		"* [Revenue](sales/revenue.md) - ignored: a concept's description lives in the concept\n"

	status, plan, _ := put(producer, true)
	if status != http.StatusOK || plan != "updated" {
		t.Errorf("dry run = %d plan %q, want 200 updated", status, plan)
	}
	if strings.Contains(index(), "by channel") {
		t.Fatal("the dry run kept the description")
	}

	status, _, body := put(producer, false)
	if status != http.StatusOK {
		t.Fatalf("PUT index.md = %d: %s", status, body)
	}
	want := "* [sales/](sales/index.md) - Revenue, orders and returns by channel"
	if !strings.Contains(body, want) || !strings.Contains(index(), want) {
		t.Errorf("the generated index does not carry the description:\n%s", index())
	}
	var listing struct {
		Dirs []struct{ Name, Description string } `json:"dirs"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+dir+"/index.md", &listing)
	if len(listing.Dirs) != 1 || listing.Dirs[0].Description != "Revenue, orders and returns by channel" {
		t.Errorf("the JSON listing = %+v", listing.Dirs)
	}

	// ochakai's own export, handed back, changes nothing; and the count
	// it writes for an undescribed directory is not a description.
	if _, plan, _ = put(index(), false); plan != "unchanged" {
		t.Errorf("its own index handed back planned %q, want unchanged", plan)
	}
	if _, _, _ = put("* [sales](sales/) - 1 concept\n", false); strings.Contains(index(), "by channel") {
		t.Error("a line with only the generated count did not clear the description")
	}
	if !strings.Contains(index(), "* [sales/](sales/index.md) - 1 concept") {
		t.Errorf("an undescribed directory lost its count:\n%s", index())
	}
}

// TestRESTIntegrationKeepsWhatTheRootIndexSaysOfTheBundle pins OKF SPEC
// §8.1: a bundle-root index.md's frontmatter — title, description,
// license, extension keys — is what the bundle says about itself, and a
// producer that regenerates index.md must keep it. A PUT of the root
// index.md keeps it as written, and every root index.md served after
// carries it after ochakai's own okf_version — at its address, and in an
// archive, with the directory descriptions beside it (decision 0154).
func TestRESTIntegrationKeepsWhatTheRootIndexSaysOfTheBundle(t *testing.T) {
	srv, _ := newIntegrationServer(t)
	dir := testdb.Unique(t, "restbundlemeta")
	id := dir + "/sales/revenue"
	removeEntries(t, srv, id)
	resp := putDoc(t, srv.URL, id, []byte("---\ntype: Metric\n---\n\nbody\n"), false)
	resp.Body.Close()

	put := func(path, index string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+path, strings.NewReader(index))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "text/markdown")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode, resp.Header.Get("Ochakai-Plan")
	}
	get := func(path string) string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/v1/bundle/" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	// A defer rather than t.Cleanup: cleanups run after this function's
	// defers, by which time the server is closed. The metadata is the one
	// row a deployment has, so the test leaves none behind.
	defer put("index.md", "")

	const meta = "# who may use it\nlicense: CC-BY-4.0\ntitle: \"Acme retail\"\nx-acme-owner: retail\n"
	if status, plan := put("index.md", "---\nokf_version: \"0.2\"\n"+meta+"---\n\n# Acme\n"); status != http.StatusOK || plan != "updated" {
		t.Fatalf("PUT index.md = %d plan %q, want 200 updated", status, plan)
	}
	want := "---\nokf_version: \"0.3\"\n" + meta + "---\n\n"
	root := get("index.md")
	if !strings.HasPrefix(root, want) {
		t.Errorf("the root index =\n%s\nwant it to begin\n%s", root, want)
	}
	// Handed back as served, nothing changes; a deeper index has no
	// frontmatter and a PUT of one does not touch the bundle's.
	if _, plan := put("index.md", root); plan != "unchanged" {
		t.Errorf("the root index handed back planned %q, want unchanged", plan)
	}
	if _, plan := put(dir+"/index.md", "---\ntitle: Not the bundle\n---\n\n* [sales](sales/) - By channel\n"); plan != "updated" {
		t.Errorf("a deeper index planned %q, want updated (its description)", plan)
	}
	if sub := get(dir + "/index.md"); strings.HasPrefix(sub, "---") {
		t.Errorf("a deeper index carries frontmatter:\n%s", sub)
	}
	if !strings.HasPrefix(get("index.md"), want) {
		t.Error("a deeper index's frontmatter changed the bundle's")
	}

	// The archive says what the addresses say.
	resp, err := getArchive(t, srv.URL+"/api/v1/bundle/"+dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	indexes := map[string]string{}
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(hdr.Name, "index.md") {
			b, _ := io.ReadAll(tr)
			indexes[hdr.Name] = string(b)
		}
	}
	if !strings.HasPrefix(indexes["index.md"], want) {
		t.Errorf("the archive's root index =\n%s", indexes["index.md"])
	}
	if line := "* [sales/](sales/index.md) - By channel"; !strings.Contains(indexes[dir+"/index.md"], line) {
		t.Errorf("the archive's %s/index.md does not carry the description:\n%s", dir, indexes[dir+"/index.md"])
	}

	// A frontmatter that is not one is refused; a root index with none
	// clears what was kept.
	if status, _ := put("index.md", "---\ntitle: a\ntitle: b\n---\n"); status != http.StatusBadRequest {
		t.Errorf("a key written twice = %d, want 400", status)
	}
	if _, plan := put("index.md", "# Acme\n"); plan != "updated" {
		t.Errorf("a root index with no frontmatter planned %q, want updated", plan)
	}
	if root := get("index.md"); !strings.HasPrefix(root, "---\nokf_version: \"0.3\"\n---\n\n") {
		t.Errorf("the metadata was not cleared:\n%s", root)
	}
}
