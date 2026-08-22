package httpauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/na0fu3y/ochakai/internal/config"
)

// Discovery is the handshake a client performs when it has no token and
// no configuration: it calls the resource, reads the address of a
// metadata document out of the 401, fetches that document, and learns
// which authorization server to go to (RFC 9728). It is what a
// vendor-hosted MCP client needs before it can ask anyone for a token,
// and the only part of that exchange the resource itself owns.
//
// ochakai does not become an authorization server. That was design
// doc 0010, retired by 0012 for costing a deployment its only secret,
// its only public service, its only Domain Restricted Sharing exemption
// and the attack surface of a public AS. Two of those four are gone
// now, and not because this file is careful: 0086 gave a deployment its
// own OpenID Connect issuer, so the authorization server is one the
// operator already runs, and the two facts this document has to publish
// — who issues the tokens, and what audience this deployment answers to
// — are exactly the two that deployment already names. Nothing is
// issued here, nothing is registered here, nothing is rotated here.
//
// The other two are not gone. A deployment that wants this handshake is
// publicly invokable, which is a decision about reachability rather
// than about identity: every caller still arrives with a token the
// issuer signed and this process verified (0086 §2), so the posture
// (0066) does not move and the deployment records who wrote what,
// exactly as it does behind Cloud Run IAM.
//
// No scopes are advertised, because there are none: whoever the issuer
// vouches for may read and write everything (0065 §1). This publishes
// where to authenticate, not what anyone may do.
type Discovery struct {
	// resourceMetadataURL is the absolute address the challenge names.
	resourceMetadataURL string
	// path is where that document is served, derived from the
	// resource's own path (RFC 9728 §3.1).
	path string
	body []byte
}

// NewDiscovery builds the document a deployment publishes, or returns
// nil for one that publishes none. Nil is the answer for every
// deployment that existed before this: the handshake is off unless a
// deployment named an issuer (0086) *and* spelled its audience as the
// https URL clients reach it at, which is the form the metadata
// document has to carry (RFC 9728 §2, `resource`).
//
// The audience is not required to be a URL — a deployment behind Cloud
// Run IAM may answer to any string — so a non-URL audience is not a
// misconfiguration to refuse at startup. It is a deployment that is not
// asking for this, and it gets a nil here rather than an error.
func NewDiscovery(cfg *config.Config) *Discovery {
	if cfg == nil || cfg.OIDCIssuer == "" || cfg.OIDCAudience == "" {
		return nil
	}
	u, err := url.Parse(cfg.OIDCAudience)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil
	}
	// RFC 9728 §3.1: the resource's path components follow the
	// well-known segment, so https://h/mcp is described at
	// https://h/.well-known/oauth-protected-resource/mcp — one host can
	// then describe more than one protected resource.
	//
	// The path becomes a mux pattern, and a pattern the mux cannot
	// parse is a panic at startup rather than a message: a deployment
	// whose audience is not a plain path is one more that is not asking
	// for this.
	if !plainPath(u.Path) {
		return nil
	}
	path := "/.well-known/oauth-protected-resource" + strings.TrimSuffix(u.Path, "/")
	body, err := json.Marshal(map[string]any{
		"resource":                 cfg.OIDCAudience,
		"authorization_servers":    []string{cfg.OIDCIssuer},
		"bearer_methods_supported": []string{"header"},
	})
	if err != nil {
		return nil
	}
	return &Discovery{
		resourceMetadataURL: u.Scheme + "://" + u.Host + path,
		path:                path,
		body:                body,
	}
}

// Mount serves the metadata document, and does nothing for a deployment
// that publishes none — so an unconfigured server keeps answering the
// well-known address the way it answers any other address it does not
// have.
func (d *Discovery) Mount(mux *http.ServeMux) {
	if d == nil {
		return
	}
	mux.HandleFunc("GET "+d.path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// The document says nothing about the caller and changes only
		// when the deployment is reconfigured, so it is cacheable — and
		// a client that re-reads it on every reconnect should not pay
		// for that.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(d.body)
	})
}

// Challenge names the metadata document on the 401 that next writes.
// Without it the handshake has no first step: a client that gets a bare
// 401 knows it is unauthenticated and nothing else, and the header is
// the only place RFC 9728 §5.1 puts the answer.
//
// It wraps rather than living in Middleware because Middleware is on
// /api/v1 too, and that 401 is a declared response of a frozen contract
// (0086 §6). Putting a header on it is a change to the contract; this
// is one address's handshake, and it stays on that address.
func (d *Discovery) Challenge(next http.Handler) http.Handler {
	if d == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RFC 6750 §3.1: a request that carried a token was refused
		// because of the token, and a client that hears so can stop
		// reusing it. One that carried none is simply being asked.
		params := `resource_metadata="` + d.resourceMetadataURL + `"`
		if bearerFrom(r.Header.Get("Authorization")) != "" ||
			bearerFrom(r.Header.Get("X-Serverless-Authorization")) != "" {
			params = `error="invalid_token", ` + params
		}
		next.ServeHTTP(&challengeWriter{ResponseWriter: w, params: params}, r)
	})
}

// challengeWriter adds the challenge to a 401 on its way out and leaves
// every other response alone.
type challengeWriter struct {
	http.ResponseWriter
	params string
}

func (c *challengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized {
		c.Header().Set("WWW-Authenticate", "Bearer "+c.params)
	}
	c.ResponseWriter.WriteHeader(status)
}

// Flush keeps a streaming response streaming. /mcp is the one address
// this wrapper is on, and a streamable session is exactly the traffic
// that would stall if this were swallowed (internal/restapi's logging
// writer, which is on the same route, says the same thing).
func (c *challengeWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Path is where the metadata document is served, and empty for a
// deployment that serves none — so the startup log can say whether this
// handshake is on without the operator having to infer it from two
// variables.
func (d *Discovery) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// plainPath reports whether a URL path is made of ordinary segments —
// the only shape this turns into a route.
func plainPath(p string) bool {
	if p == "" || p == "/" {
		return true
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/")[1:], "/") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case r == '-', r == '_', r == '.', r == '~':
			default:
				return false
			}
		}
	}
	return true
}
