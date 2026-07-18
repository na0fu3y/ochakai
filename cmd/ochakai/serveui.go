// `ochakai serve-ui`: serve the bundled web UI as a deployable service —
// the team-shared counterpart of `ochakai ui` (design doc 0006). It
// serves the static page (internal/webui) and reverse-proxies /api/v1
// (and /mcp) to the ochakai server named by $OCHAKAI_URL, attaching this
// service's identity token in X-Serverless-Authorization for Cloud Run
// service-to-service auth. The ochakai deployment therefore stays
// IAM-restricted, and browser users are recorded as this service's
// identity (agent:<sa-email>). Same image as `serve`: deploy it with
// `--args=serve-ui` (deploy guide §5b).
//
// Without OCHAKAI_URL it serves the static page only (the page then
// calls whatever base URL the user enters, e.g. a local ochakai).
//
// Unlike `ochakai ui` this binds all interfaces and acts as the service,
// never as a person — access control is the deployment's job (Cloud Run
// IAM, IAP). The two stay separate commands so neither can drift into
// the other's threat model.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/na0fu3y/ochakai/internal/webui"
)

func serveUI(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var handler http.Handler
	if target := os.Getenv("OCHAKAI_URL"); target != "" {
		u, err := url.Parse(target)
		if err != nil {
			return fmt.Errorf("invalid OCHAKAI_URL %q: %w", target, err)
		}
		handler = serveUIHandler(newServiceProxy(u, &metadataTokenSource{audience: target}, log))
		log.Info("proxying to ochakai", "target", target)
	} else {
		handler = serveUIHandler(nil)
		log.Info("no OCHAKAI_URL; serving the static page only")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Info("serve-ui listening", "addr", ":"+port, "version", version)
	return runServer(ctx, ":"+port, handler)
}

// serveUIHandler serves the embedded page at / plus /health, and routes
// /api/v1 and /mcp through proxy (nil = no upstream configured: the API
// paths 404 and the page talks to whatever base URL the user enters).
func serveUIHandler(proxy http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webui.Index)
	})
	if proxy != nil {
		mux.Handle("/api/v1/", proxy)
		mux.Handle("/mcp", proxy)
	}
	return mux
}

// newServiceProxy reverse-proxies to target as this service (not as the
// browser user): every request carries the service's Google-signed ID
// token in X-Serverless-Authorization, the header Cloud Run validates
// in preference to Authorization — which therefore passes through
// untouched for the backend's httpauth to ignore.
func newServiceProxy(target *url.URL, tokens serviceTokenSource, log *slog.Logger) http.Handler {
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

type serviceTokenSource interface {
	token() (string, error)
}

// metadataTokenSource fetches this service's Google-signed ID token from
// the metadata server and caches it briefly (tokens live ~1h).
type metadataTokenSource struct {
	audience string

	mu      sync.Mutex
	token_  string
	expires time.Time
}

func (s *metadataTokenSource) token() (string, error) {
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
