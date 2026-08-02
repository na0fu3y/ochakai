// Plumbing shared by the two web-UI commands, `ochakai ui` and
// `ochakai serve-ui` (design doc 0006). They stay separate commands so
// neither can drift into the other's threat model; what they share is
// the page layout and the rule that no inbound credential ever reaches
// the upstream.
package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/webui"
)

// webUIMux serves the embedded page at / and routes /api/v1 and /mcp
// through proxy. Callers add their own extras (serve-ui's /health) and
// wrapping (ui's localBrowserGuard).
func webUIMux(proxy http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webui.Index)
	})
	mux.Handle("/api/v1/", proxy)
	mux.Handle("/mcp", proxy)
	return mux
}

// newCredentialProxy reverse-proxies to target, substituting the proxy's
// own identity for whatever the browser sent. Both credential headers
// httpauth reads — Authorization and X-Serverless-Authorization — are
// dropped before setAuth attaches the outbound identity, whether or not
// it attaches one: the moment the proxy has no identity of its own is
// exactly when a forwarded browser header would get to name the actor
// instead. The delegation header is the caller's business
// (stripDelegation / withIAPIdentity), because serve-ui's IAP path
// legitimately sets one after stripping the inbound claim.
//
// Rewrite rather than Director: it drops the browser's X-Forwarded-*
// headers instead of appending to them, which is what we want from a
// proxy that trusts nothing the page sends.
func newCredentialProxy(target *url.URL, setAuth func(http.Header)) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target) // also points the outbound Host at the target
		r.Out.Header.Del("Authorization")
		r.Out.Header.Del("X-Serverless-Authorization")
		setAuth(r.Out.Header)
	}}
}

// stripDelegation removes an inbound delegation header: whatever the
// browser sent is a claim about identity from the one party with no
// standing to make it. It wraps any handler that may legitimately set
// one, so it runs first and cannot undo it.
//
// Both spellings are dropped, not just the current one. Design doc 0064
// renamed OnBehalfOfHeader by dropping its "X-" prefix with no dual-accept
// window, and this proxy is a separate Cloud Run service from the API
// (design doc 0032) that can lag behind it during a staggered upgrade: a
// build old enough to still call this the retired name would otherwise
// strip only that name and forward the current one untouched, straight
// to an API new enough to honor it (issue #410).
func stripDelegation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del(httpauth.OnBehalfOfHeader)
		r.Header.Del("X-" + httpauth.OnBehalfOfHeader)
		next.ServeHTTP(w, r)
	})
}
