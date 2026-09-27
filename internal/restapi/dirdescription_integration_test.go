package restapi

import (
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
