// `ochakai ui`: serve the bundled web UI on loopback, reverse-proxying
// /api/v1 (and /mcp) to the selected server with the caller's own Google
// identity — the zero-deploy counterpart of `ochakai serve-ui` (design
// doc 0006). Same page, different proxy: here edits are recorded as
// human:<you>, on the deployed serve-ui as its service account.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/oauth2"
)

func cmdUI(ctx context.Context, args []string) error {
	fs, target := newFlagSet(
		"ui",
		"Usage: ochakai ui [flags]\n\nServe the web UI at http://127.0.0.1:<port> against the selected\nserver. API calls are proxied with your own Google identity (resolved\nthe same way as every other client command), so no deployment is\nneeded and your edits are recorded as human:<you>. The proxy also\nexposes /mcp, so it doubles as an authenticated local MCP endpoint.\nFor a team-shared UI on Cloud Run, deploy `ochakai serve-ui`.",
		"  ochakai ui\n  ochakai ui --port 9000\n  claude mcp add --transport http ochakai http://127.0.0.1:8098/mcp\n")
	port := fs.Int("port", 8098, "port to listen on (always bound to 127.0.0.1: whoever reaches the proxy acts as you)")
	if _, err := exactArgs(fs, args, 0); err != nil {
		return err
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
	fmt.Printf("ochakai ui: http://127.0.0.1:%d → %s as %s (%s); Ctrl-C to stop\n",
		*port, *target, identity, auth)
	return runServer(ctx, fmt.Sprintf("127.0.0.1:%d", *port), handler)
}

// uiHandler serves the embedded page at / and reverse-proxies /api/v1
// and /mcp to target, replacing any browser-supplied credentials with
// a fresh ID token from tokens (nil = plain-http development server,
// forward without credentials). The delegation header is stripped too:
// this proxy has no verified source for one (serve-ui gets its from
// IAP, design doc 0032), and a page that could set it would be forging
// an author on a server where you are permitted to delegate.
func uiHandler(target string, tokens oauth2.TokenSource) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL %q: %w", target, err)
	}
	proxy := newCredentialProxy(u, func(h http.Header) {
		if tokens != nil {
			if tok, err := tokens.Token(); err == nil {
				h.Set("Authorization", "Bearer "+tok.AccessToken)
			}
		}
	})
	return loopbackHostGuard(crossOriginGuard(webUIMux(stripDelegation(proxy)))), nil
}

// loopbackHostGuard fends off the way a web page in the user's browser
// could abuse a loopback proxy that signs requests as them and that the
// cross-origin check cannot see. The listener is already bound to
// 127.0.0.1, so a network peer can't reach it; this guard is about the
// browser the user drives.
//
// Host header: rejects non-loopback names, stopping DNS rebinding — a
// malicious page pointing its own hostname at 127.0.0.1 to get
// same-origin access. That is precisely the attack crossOriginGuard is
// blind to: the page's Origin and the Host it reaches agree, because the
// attacker owns both, and Sec-Fetch-Site says same-origin. This guard
// runs first and reads the one thing that still gives it away.
//
// The cross-site half of this function is gone: it hand-rolled the
// Origin comparison that net/http.CrossOriginProtection now makes, and
// makes better — the standard check reads Sec-Fetch-Site as well, and
// serve-ui, which never had this function, gets the same rule from the
// same place (design doc 0126).
//
// kubectl proxy learned the rebinding lesson the hard way.
func loopbackHostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "unexpected Host header (DNS rebinding guard): open the UI via http://127.0.0.1 or http://localhost", http.StatusForbidden)
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
