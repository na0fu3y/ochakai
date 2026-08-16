package restapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A dry run answers with the values the write would stamp, not with the
// zero values of the fields the write happens to fill (design doc 0064).
// The empty actor was the worst of them: `observed.generated.by` with
// kind "" is a value the contract's own closed enum [human, process]
// forbids, and it was reaching the wire on every update plan.
func TestRESTIntegrationDryRunStampsWhatTheWriteWould(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restdrystamp") + "/revenue"
	removeEntries(t, srv, id)
	doc := docFrom(t, map[string]any{"type": testdb.Unique(t, "restdrystamp"), "id": id, "title": "plan values"})

	// The create plan resolves everything a created concept resolves.
	resp := dryRunDoc(t, srv.URL, id, doc)
	var planned domain.View
	if err := json.NewDecoder(resp.Body).Decode(&planned); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry run = %d, want 200", resp.StatusCode)
	}
	if planned.Observed.Generated.By.Kind == "" || planned.Observed.Generated.By.Name == "" {
		t.Errorf("create plan generated.by = %+v, want the caller resolved", planned.Observed.Generated.By)
	}
	if planned.Observed.Generated.At.IsZero() {
		t.Error("create plan generated.at is the zero time")
	}
	if planned.Summary.CreatedAt.IsZero() || planned.Summary.UpdatedAt.IsZero() {
		t.Errorf("create plan times = %v / %v, want the instants the write would stamp",
			planned.Summary.CreatedAt, planned.Summary.UpdatedAt)
	}
	if planned.Summary.ContentHash == "" {
		t.Error("create plan carries no content_hash; the hash is deterministic and the write would return it")
	}

	// The hash the plan promised is the hash the write returns.
	resp = putDoc(t, srv.URL, id, doc, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("ETag"); got != `"`+planned.Summary.ContentHash+`"` {
		t.Errorf("write ETag = %q, want the plan's content_hash %q", got, planned.Summary.ContentHash)
	}

	// The update plan resolves the same set.
	edited := bytes.Replace(doc, []byte("plan values"), []byte("planned values"), 1)
	resp = dryRunDoc(t, srv.URL, id, edited)
	planned = domain.View{}
	if err := json.NewDecoder(resp.Body).Decode(&planned); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Plan"); got != "updated" {
		t.Fatalf("Ochakai-Plan = %q, want updated", got)
	}
	if planned.Observed.Generated.By.Kind == "" || planned.Observed.Generated.By.Name == "" {
		t.Errorf("update plan generated.by = %+v, want the caller resolved", planned.Observed.Generated.By)
	}
	if planned.Observed.Generated.At.IsZero() || planned.Summary.UpdatedAt.IsZero() {
		t.Error("update plan carries zero times")
	}
	if planned.Summary.ContentHash == "" {
		t.Error("update plan carries no content_hash")
	}
}

// The file plan resolves created_by and created_at the way the file
// write does — `File` has no omitempty on either, so a plan that left
// them zero put an empty actor and the year one on the wire (design doc
// 0064).
func TestRESTIntegrationFileDryRunStampsWhatTheWriteWould(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	dir := testdb.Unique(t, "restdryfile")
	path := dir + "/seeds.csv"
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/api/v1/bundle/"+path+"?dry_run=true", strings.NewReader("a,b\n1,2\n"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var planned domain.File
	if err := json.NewDecoder(resp.Body).Decode(&planned); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file dry run = %d, want 200", resp.StatusCode)
	}
	if planned.CreatedBy.Kind == "" || planned.CreatedBy.Name == "" {
		t.Errorf("file plan created_by = %+v, want the caller resolved", planned.CreatedBy)
	}
	if planned.CreatedAt.IsZero() {
		t.Error("file plan created_at is the zero time")
	}
	// And nothing was written.
	head, err := http.Get(srv.URL + "/api/v1/bundle/" + path)
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != http.StatusNotFound {
		t.Errorf("the file dry run wrote: GET = %d, want 404", head.StatusCode)
	}
}

// Overwriting a file keeps the original creator on the row — created_at
// is not in the ON CONFLICT arm — so the response says the row's values
// rather than the current caller's, or it disagrees with the next GET
// (design doc 0064).
func TestRESTIntegrationFileOverwriteKeepsTheCreator(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	path := testdb.Unique(t, "restfileover") + "/seeds.csv"
	put := func(body string) domain.File {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut,
			srv.URL+"/api/v1/bundle/"+path, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var f domain.File
		if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	first := put("a,b\n1,2\n")
	second := put("a,b\n3,4\n")
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("overwrite created_at = %v, want the original %v", second.CreatedAt, first.CreatedAt)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+path, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var meta domain.File
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !meta.CreatedAt.Equal(second.CreatedAt) {
		t.Errorf("GET created_at = %v, the PUT answered %v", meta.CreatedAt, second.CreatedAt)
	}
}

// status and trust are closed request vocabularies: a value outside them
// is a 400 naming the vocabulary, not a 200 with no hits — the silent
// no-op design doc 0064 §2 closed for unknown keys, closed for unknown
// values of the two keys whose misspelling used to read as an empty
// knowledge base.
func TestRESTIntegrationStatusAndTrustOutsideTheVocabularyAre400(t *testing.T) {
	// Not checkedServer: these requests violate the contract's own
	// request enums on purpose, and the validator would report exactly
	// that. What is under test is the server's answer to an off-contract
	// value, so the requests go to the bare handler.
	srv := httptest.NewServer(Handler(newIntegrationService(t)))
	t.Cleanup(srv.Close)

	for _, q := range []string{
		"/api/v1/search?q=revenue&status=Draft",
		"/api/v1/search?q=revenue&status=verified",
		"/api/v1/search?q=revenue&trust=verified",
		"/api/v1/search?q=revenue&trust=Human-Reviewed",
	} {
		resp, err := http.Get(srv.URL + q)
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", q, resp.StatusCode)
			continue
		}
		if !strings.Contains(e.Error, "must be one of") {
			t.Errorf("%s error = %q, want it to name the vocabulary", q, e.Error)
		}
	}

	// The vocabulary's own spellings still answer.
	resp, err := http.Get(srv.URL + "/api/v1/search?q=revenue&status=draft&trust=human-reviewed")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid status+trust = %d, want 200", resp.StatusCode)
	}
}

// reembed reads no body, so a body is refused rather than dropped:
// `{"limit": 10}` in the body used to run the default-sized pass — the
// body-shaped false green design doc 0064 §2 closed for query keys.
func TestRESTIntegrationReembedRefusesABody(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	resp, err := http.Post(srv.URL+"/api/v1/reembed", "application/json",
		strings.NewReader(`{"limit": 10}`))
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reembed with a body = %d, want 400", resp.StatusCode)
	}
	if !strings.Contains(e.Error, "no request body") {
		t.Errorf("error = %q, want it to say the operation takes no body", e.Error)
	}
}

// fm.id and fm.status_note are ochakai's own envelope keys, not OKF's —
// SPEC v0.2 defines no top-level id and no status_note — and the filter
// vocabulary is OKF's, so they are refused the way any producer's
// extension key is (design doc 0064; 0074 §4.2).
func TestRESTIntegrationOchakaiOwnKeysAreNotAskable(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	for _, q := range []string{
		"/api/v1/search?q=revenue&fm.id=metrics/revenue",
		"/api/v1/search?q=revenue&fm.status_note=stale",
	} {
		resp, err := http.Get(srv.URL + q)
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", q, resp.StatusCode)
			continue
		}
		if !strings.Contains(e.Error, "OKF does not define") {
			t.Errorf("%s error = %q, want the OKF-vocabulary refusal", q, e.Error)
		}
	}
}
