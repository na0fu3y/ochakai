package restapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestRESTIntegrationServesTheBundleAsOKF pins design doc 0153: a plain
// GET — curl's */*, a static-file OKF reader, an agent following a link —
// gets what a static file server would give it. A concept's address
// answers the document, a directory's address its index, and the JSON
// forms are there for whoever names them.
func TestRESTIntegrationServesTheBundleAsOKF(t *testing.T) {
	srv, _ := newIntegrationServer(t)
	dir := testdb.Unique(t, "restokf")
	id := dir + "/revenue"
	removeEntries(t, srv, id)
	put := putDoc(t, srv.URL, id, []byte("---\ntype: Metric\ndescription: Completed sales\n---\n\nSee [orders](/tables/orders.md).\n"), false)
	put.Body.Close()

	type answer struct {
		status      int
		contentType string
		etag        string
	}
	get := func(path, accept string) (answer, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return answer{resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("ETag")}, string(body)
	}

	for _, accept := range []string{"", "*/*", "text/html, */*;q=0.8", "text/markdown"} {
		a, body := get(id+".md", accept)
		if a.status != http.StatusOK || !strings.HasPrefix(a.contentType, "text/markdown") {
			t.Errorf("Accept %q: %d %s, want the document", accept, a.status, a.contentType)
		}
		if !strings.HasPrefix(body, "---\ntype: Metric\n") || !strings.Contains(body, "generated:") {
			t.Errorf("Accept %q: not the export-form document:\n%s", accept, body)
		}
		if a.etag == "" {
			t.Errorf("Accept %q: the document came without its version", accept)
		}
	}
	_, body := get(id+".md", "application/json")
	var v domain.View
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.ID != id {
		t.Errorf("Accept: application/json did not answer the View (%v): %s", err, body)
	}

	for _, path := range []string{dir + "/", ""} {
		a, body := get(path, "")
		if a.status != http.StatusOK || !strings.HasPrefix(a.contentType, "text/markdown") {
			t.Errorf("GET %q = %d %s, want the directory's index.md", path, a.status, a.contentType)
		}
		_, index := get(path+"index.md", "")
		if body != index {
			t.Errorf("GET %q and GET %qindex.md differ:\n%s\n---\n%s", path, path, body, index)
		}
	}
	if a, body := get(dir+"/", "application/json"); a.status != http.StatusOK || !strings.Contains(body, `"id":"`+id+`"`) {
		t.Errorf("a directory's JSON listing: %d %s", a.status, body)
	}
}
