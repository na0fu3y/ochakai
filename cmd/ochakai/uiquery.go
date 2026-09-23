// `ochakai ui` runs a query the agent proposed itself, as the person at
// the keyboard (design doc 0142 §4: "手元の `ochakai ui` では、プロキシが
// 本人として実行してよい"). The shared web UI signs a person in through a
// popup instead; here the proxy already holds their identity, so the popup,
// the OAuth client and its per-port registration are all things a person
// would pay for nothing.
//
// What the popup's bigquery.readonly scope guaranteed has to be kept some
// other way, because a gcloud user token carries cloud-platform: a
// proposal that was DML would run. So every query is dry-run first, and
// only a SELECT goes on. The byte cap the page asks for is imposed here
// too, rather than trusted from the page.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"golang.org/x/oauth2"
)

// maxBytesBilled is the page's cap (internal/webui/static/js/sql.js),
// enforced where the query leaves this machine.
const maxBytesBilled = 10 << 30

// queryRoutes are the only two BigQuery calls the page makes, at the same
// paths Google serves them, so the page swaps a base URL and nothing else.
const (
	queryRoute   = "POST /bigquery/v2/projects/{project}/queries"
	resultsRoute = "GET /bigquery/v2/projects/{project}/queries/{job}"
)

// bigQueryUpstream is Google's endpoint; a variable so tests can stand in.
var bigQueryUpstream = "https://bigquery.googleapis.com"

// queryHandler serves queryRoutes against BigQuery with tokens.
func queryHandler(tokens oauth2.TokenSource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(queryRoute, func(w http.ResponseWriter, r *http.Request) {
		project := r.PathValue("project")
		var req map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			queryError(w, http.StatusBadRequest, "the request is not JSON: "+err.Error())
			return
		}
		sql, _ := req["query"].(string)
		if sql == "" {
			queryError(w, http.StatusBadRequest, "no query")
			return
		}
		tok, err := tokens.Token()
		if err != nil {
			queryError(w, http.StatusBadGateway, "ochakai ui could not get a Google token: "+err.Error())
			return
		}
		kind, status, body := statementType(r, tok.AccessToken, project, sql)
		if status != http.StatusOK {
			relay(w, status, body)
			return
		}
		if kind != "SELECT" {
			queryError(w, http.StatusForbidden, fmt.Sprintf("ochakai ui runs only a SELECT; this statement is %s", kind))
			return
		}
		// Rebuilt from what the page sends rather than passed through: a
		// field this proxy does not know (a session, connection
		// properties) is one it cannot say is harmless.
		fwd := map[string]any{
			"query":              sql,
			"useLegacySql":       false,
			"maximumBytesBilled": strconv.Itoa(maxBytesBilled),
		}
		for _, k := range []string{"timeoutMs", "maxResults", "location"} {
			if v, ok := req[k]; ok {
				fwd[k] = v
			}
		}
		out, _ := json.Marshal(fwd)
		status, body = call(r, tok.AccessToken, http.MethodPost, "/bigquery/v2/projects/"+url.PathEscape(project)+"/queries", out)
		relay(w, status, body)
	})
	mux.HandleFunc(resultsRoute, func(w http.ResponseWriter, r *http.Request) {
		tok, err := tokens.Token()
		if err != nil {
			queryError(w, http.StatusBadGateway, "ochakai ui could not get a Google token: "+err.Error())
			return
		}
		// Reading a finished job's rows changes nothing, and the job was
		// started by the route above, so the query string passes as is.
		path := "/bigquery/v2/projects/" + url.PathEscape(r.PathValue("project")) + "/queries/" + url.PathEscape(r.PathValue("job"))
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		status, body := call(r, tok.AccessToken, http.MethodGet, path, nil)
		relay(w, status, body)
	})
	return mux
}

// statementType dry-runs sql and answers what kind of statement BigQuery
// says it is. jobs.insert rather than jobs.query: only a job's statistics
// carry the statement type. On anything but 200 the status and body are
// BigQuery's, so a syntax error reads the same as it would have run.
func statementType(r *http.Request, token, project, sql string) (string, int, []byte) {
	dry, _ := json.Marshal(map[string]any{"configuration": map[string]any{
		"dryRun": true,
		"query":  map[string]any{"query": sql, "useLegacySql": false},
	}})
	status, body := call(r, token, http.MethodPost, "/bigquery/v2/projects/"+url.PathEscape(project)+"/jobs", dry)
	if status != http.StatusOK {
		return "", status, body
	}
	var job struct {
		Statistics struct {
			Query struct {
				StatementType string `json:"statementType"`
			} `json:"query"`
		} `json:"statistics"`
	}
	if err := json.Unmarshal(body, &job); err != nil || job.Statistics.Query.StatementType == "" {
		return "", http.StatusBadGateway, errorBody("BigQuery's dry run did not say what the statement is")
	}
	return job.Statistics.Query.StatementType, http.StatusOK, nil
}

func call(r *http.Request, token, method, path string, body []byte) (int, []byte) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, bigQueryUpstream+path, rd)
	if err != nil {
		return http.StatusInternalServerError, errorBody(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return http.StatusBadGateway, errorBody("BigQuery did not answer: " + err.Error())
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return http.StatusBadGateway, errorBody("reading BigQuery's answer: " + err.Error())
	}
	return res.StatusCode, b
}

func relay(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// queryError answers in BigQuery's own error shape, which the page
// already reads (error.message).
func queryError(w http.ResponseWriter, status int, msg string) {
	relay(w, status, errorBody(msg))
}

func errorBody(msg string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg}})
	return b
}
