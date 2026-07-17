// Sample web UI server for ochakai, deployable as its own Cloud Run
// service. It serves the static UI (internal/webui, shared with the
// zero-deploy `ochakai ui` command) and reverse-proxies /api/v1 (and
// /mcp) to the ochakai service, attaching this service's identity token
// in X-Serverless-Authorization for Cloud Run service-to-service auth.
// The ochakai deployment therefore stays IAM-restricted, and browser
// users are recorded as this service's identity (agent:<sa-email>).
//
// Without OCHAKAI_URL it serves the static UI only (the page then calls
// whatever base URL the user enters, e.g. a local ochakai).
package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/na0fu3y/ochakai/internal/webui"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webui.Index)
	})

	if target := os.Getenv("OCHAKAI_URL"); target != "" {
		u, err := url.Parse(target)
		if err != nil {
			log.Error("invalid OCHAKAI_URL", "error", err)
			os.Exit(1)
		}
		proxy := newOchakaiProxy(u, log)
		mux.Handle("/api/v1/", proxy)
		mux.Handle("/mcp", proxy)
		log.Info("proxying to ochakai", "target", target)
	}

	addr := ":" + envOr("PORT", "8080")
	log.Info("webui listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("webui failed", "error", err)
		os.Exit(1)
	}
}

func newOchakaiProxy(target *url.URL, log *slog.Logger) *httputil.ReverseProxy {
	tokens := &idTokenSource{audience: target.String()}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = target.Host
		// Cloud Run service-to-service auth; harmless if unavailable
		// (e.g. running locally against an unrestricted ochakai).
		if tok, err := tokens.token(); err == nil {
			r.Header.Set("X-Serverless-Authorization", "Bearer "+tok)
		} else {
			log.Warn("no service identity token; forwarding without it", "error", err)
		}
	}
	return proxy
}

// idTokenSource fetches this service's Google-signed ID token from the
// metadata server and caches it briefly (tokens live ~1h).
type idTokenSource struct {
	audience string

	mu      sync.Mutex
	token_  string
	expires time.Time
}

func (s *idTokenSource) token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token_ != "" && time.Now().Before(s.expires) {
		return s.token_, nil
	}
	req, err := http.NewRequest(http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity?audience="+url.QueryEscape(s.audience), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata identity token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	s.token_ = strings.TrimSpace(string(body))
	s.expires = time.Now().Add(50 * time.Minute)
	return s.token_, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
