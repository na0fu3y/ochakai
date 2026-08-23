// Plumbing shared by the two web-UI commands, `ochakai ui` and
// `ochakai serve-ui` (design doc 0006). They stay separate commands so
// neither can drift into the other's threat model; what they share is
// the page layout and the rule that no inbound credential ever reaches
// the upstream.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"

	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/webui"
)

// webUIMux serves the embedded page and its modules at /, and routes
// /api/v1 and /mcp through proxy. Callers add their own extras
// (serve-ui's /health) and wrapping (ui's loopbackHostGuard).
//
// The two API patterns are more specific than "/", so they win the
// ServeMux match however the page's own asset paths are spelled.
func webUIMux(proxy http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	// No method on the catch-all: "GET /" and "/api/v1/" are ambiguous to
	// ServeMux — one is more specific in its method and the other in its
	// path — and it says so by panicking at registration. The method
	// check moves inside the handler instead.
	mux.Handle("/", assets(webui.Files))
	mux.Handle("/api/v1/", proxy)
	mux.Handle("/mcp", proxy)
	return mux
}

// crossOriginGuard refuses a write a browser says came from another
// site, and is the one rule both web-UI commands share for the same
// reason: a browser reaches ochakai only through one of them, and only
// there does an ambient credential exist for a foreign page to spend
// (design doc 0126).
//
// The check is the standard library's. It reads Sec-Fetch-Site, which
// every browser has sent since 2023, and falls back to comparing the
// Origin header's host with the Host header. What it does *not* do is
// what makes it free here: a request carrying neither header is allowed,
// so the CLI, an agent over MCP and curl are untouched — nothing to
// send, nothing to configure, and no token to keep. GET, HEAD and
// OPTIONS are always allowed, which is what leaves serve-ui's /health
// where design doc 0006 put it.
//
// It is not enough on its own for `ochakai ui`, and loopbackHostGuard is
// why: a page at attacker.example pointed at 127.0.0.1 sends a Host and
// an Origin that agree with each other, so this check passes it. The
// rebinding guard is what does not.
func crossOriginGuard(next http.Handler) http.Handler {
	return http.NewCrossOriginProtection().Handler(next)
}

// assets serves the embedded UI, read-only and revalidatable.
//
// The ETag is there because the page is a page plus a stylesheet plus
// eighteen modules now: an embedded file carries no modification time, so
// without a validator the browser has nothing to revalidate against and
// every load re-downloads all twenty in full.
//
// no-cache, not no-store and not a max-age: the browser keeps its copy
// and asks whether it is still good, so the steady state costs twenty
// 304s and an upgrade is picked up on the first load after it. A max-age
// would serve a module out of cache against an API that has moved on,
// which is the one failure this page cannot show anybody.
func assets(files fs.FS) http.Handler {
	srv := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("ETag", assetTag())
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		srv.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy is what the page is allowed to do. It became
// writable when the page stopped carrying an inline script, and design
// doc 0094 is what decided to write it: `script-src 'self'` with nothing
// inline is the whole point — an injection that reaches the DOM cannot
// become code.
//
// The page renders a concept's body, which is somebody else's text, and
// every value it interpolates goes through one escape function. This is
// the second wall behind that one.
//
//   - default-src 'none' — nothing is allowed that is not named below.
//   - script-src 'self' — the modules under js/, and nothing inline.
//   - style-src keeps 'unsafe-inline'. The page carries about forty
//     literal style attributes, and converting them would trade a real
//     stylesheet for three dozen utility classes for a small gain: the
//     danger CSP is here for is script, and no attacker-controlled value
//     reaches a style attribute (the one computed style, a table's
//     alignment, is one of four constants). Dropping it is a tidy-up
//     somebody can do later; the strict half is the half that matters.
//   - img-src allows https: because a body may legitimately show a
//     picture that lives elsewhere, and silently blanking those while
//     shipping a header would be breaking documents to look secure.
//     data: and http: are not allowed.
//   - connect-src 'self' — the API is same-origin through the proxy in
//     front (design doc 0006), so there is nowhere else to reach.
//   - frame-ancestors 'none' keeps the curation surface out of somebody
//     else's frame, and form-action 'none' means the editor's form
//     cannot navigate even if its submit handler ever stops running.
//
// No Referrer-Policy. It would be a fourteenth counted header
// (docs/surface.md) to improve on a browser default that already sends
// only the origin cross-site — and the page's own path is "/", since a
// route lives in the fragment and a fragment is never sent. A header
// that buys that little does not get counted for.
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' https:; connect-src 'self'; font-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// assetTag is one tag for the whole UI: the hash of every embedded file,
// so any edit to any of them invalidates all of them. One tag rather
// than one per file because they ship as a set and a page that reloaded
// half of them is not a state worth being able to reach — and because
// the build identity does not work here: in-tree builds are all "dev",
// which would pin a browser to whichever version it saw first.
var assetTag = sync.OnceValue(func() string {
	sum := sha256.New()
	_ = fs.WalkDir(webui.Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(webui.Files, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s\x00%x\x00", p, sha256.Sum256(b))
		return nil
	})
	return `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`
})

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
// Both spellings are dropped, not just the current one — but only in a
// build that carries this line. Design doc 0064 renamed OnBehalfOfHeader
// by dropping its "X-" prefix with no dual-accept window, and this proxy
// is a separate Cloud Run service from the API (design doc 0032) that can
// lag behind it during a staggered upgrade. What this closes is a *new*
// web UI in front of an API that has not yet adopted 0064 and still
// honors the retired spelling: without this line, the proxy would strip
// only the current name and forward the retired one straight through. It
// cannot close the reverse — an old web UI, built before this line
// existed, in front of a new API — because an old binary cannot run code
// it does not contain; the only mitigation for that direction is upgrade
// order (docs/guides/operating.md#upgrades, issue #418).
func stripDelegation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del(httpauth.OnBehalfOfHeader)
		r.Header.Del("X-" + httpauth.OnBehalfOfHeader)
		next.ServeHTTP(w, r)
	})
}
