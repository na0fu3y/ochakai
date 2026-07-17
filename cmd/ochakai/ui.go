// `ochakai ui`: serve the bundled web UI on loopback, reverse-proxying
// /api/v1 (and /mcp) to the selected server with the caller's own Google
// identity — the zero-deploy counterpart of examples/webui (design doc
// 0006). Same page, different proxy: here edits are recorded as
// human:<you>, on the Cloud Run sample as the webui's service account.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/webui"
)

func cmdUI(ctx context.Context, args []string) error {
	fs, target := newFlagSet(
		"Usage: ochakai ui [flags]\n\nServe the web UI at http://127.0.0.1:<port> against the selected\nserver. API calls are proxied with your own Google identity (resolved\nthe same way as every other client command), so no deployment is\nneeded and your edits are recorded as human:<you>. The proxy also\nexposes /mcp, so it doubles as an authenticated local MCP endpoint.\nFor a team-shared UI on Cloud Run, see examples/webui.",
		"  ochakai ui\n  ochakai ui --port 9000\n  claude mcp add --transport http ochakai http://127.0.0.1:8098/mcp\n")
	port := fs.Int("port", 8098, "port to listen on (always bound to 127.0.0.1: whoever reaches the proxy acts as you)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		fs.Usage()
		return errReported
	}

	c, err := newClient(ctx, *target)
	if err != nil {
		return err
	}
	// Fail fast, and tell the user whom the server will see.
	identity, auth, err := c.Identity()
	if err != nil {
		return err
	}
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("server %s is not reachable: %w", *target, err)
	}

	handler, err := uiHandler(*target, c.TokenSource())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", *port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("ochakai ui: http://127.0.0.1:%d → %s as %s (%s); Ctrl-C to stop\n",
		*port, *target, identity, auth)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// uiHandler serves the embedded page at / and reverse-proxies /api/v1
// and /mcp to target, replacing any browser-supplied Authorization with
// a fresh ID token from tokens (nil = plain-http development server,
// forward without credentials).
func uiHandler(target string, tokens oauth2.TokenSource) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL %q: %w", target, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = u.Host
		// Never forward whatever the browser sent; the proxy's whole job
		// is to substitute the CLI user's identity.
		r.Header.Del("Authorization")
		if tokens != nil {
			if tok, err := tokens.Token(); err == nil {
				r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webui.Index)
	})
	mux.Handle("/api/v1/", proxy)
	mux.Handle("/mcp", proxy)
	return loopbackOnly(mux), nil
}

// loopbackOnly rejects requests whose Host header is not a loopback name.
// The listener is already bound to 127.0.0.1; this additionally stops DNS
// rebinding (a malicious page resolving its own hostname to 127.0.0.1 to
// smuggle same-origin requests into a proxy that signs them as you —
// kubectl proxy learned this the hard way).
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		switch host {
		case "localhost", "127.0.0.1", "::1", "[::1]":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "unexpected Host header (DNS rebinding guard): open the UI via http://127.0.0.1 or http://localhost", http.StatusForbidden)
		}
	})
}
