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
	return localBrowserGuard(mux), nil
}

// localBrowserGuard fends off the two ways a web page in the user's
// browser could abuse a loopback proxy that signs requests as them. The
// listener is already bound to 127.0.0.1, so a network peer can't reach
// it; these guards are about the browser the user drives.
//
//   - Host header: rejects non-loopback names, stopping DNS rebinding (a
//     malicious page pointing its own hostname at 127.0.0.1 to get
//     same-origin access).
//   - Origin header: rejects cross-site writes (CSRF). A direct
//     fetch('http://127.0.0.1:PORT/...') from evil.com carries the right
//     Host but a foreign Origin; without this check a text/plain POST is
//     a "simple request" (no CORS preflight) and would be proxied with
//     the user's token. Same-origin UI requests send no Origin (GET) or a
//     loopback Origin, so they pass.
//
// kubectl proxy learned the rebinding half the hard way; the CSRF half is
// the same lesson one step further.
func localBrowserGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "unexpected Host header (DNS rebinding guard): open the UI via http://127.0.0.1 or http://localhost", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			http.Error(w, "cross-origin request refused (CSRF guard): this UI is only for the local browser", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether a Host header (host or host:port) names
// the loopback interface.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// isLoopbackOrigin reports whether an Origin header ("scheme://host[:port]")
// points at the local machine — the only origin the UI is served from.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return isLoopbackHost(u.Host)
}
