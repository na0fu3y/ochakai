package connector

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/na0fu3y/ochakai/internal/store"
)

// handleAuthorize starts the flow: validate the CIMD client and the
// request, then show the consent page. Per OAuth, errors in client_id /
// redirect_uri render an error page (never redirect to an unvalidated
// URI); later errors redirect back to the validated redirect_uri.
func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	_ = s.store.PruneOAuth(r.Context())

	q := r.URL.Query()
	clientID := q.Get("client_id")
	meta, err := s.cimd.fetch(r.Context(), clientID)
	if err != nil {
		s.log.Warn("connector: rejecting client", "client_id", clientID, "error", err)
		http.Error(w, "invalid client_id: "+err.Error(), http.StatusBadRequest)
		return
	}
	redirectURI := q.Get("redirect_uri")
	if !redirectURIAllowed(redirectURI, meta.RedirectURIs) {
		s.log.Warn("connector: redirect_uri not registered", "client_id", clientID, "redirect_uri", redirectURI)
		http.Error(w, "redirect_uri is not registered by this client", http.StatusBadRequest)
		return
	}

	state := q.Get("state")
	if q.Get("response_type") != "code" {
		redirectError(w, r, redirectURI, state, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	// PKCE S256 is mandatory; accepting "plain" (or nothing) would let a
	// leaked code be exchanged without the verifier.
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		redirectError(w, r, redirectURI, state, "invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	if resource := q.Get("resource"); resource != "" && resource != s.cfg.PublicURL+"/mcp" {
		redirectError(w, r, redirectURI, state, "invalid_target", "unknown resource")
		return
	}

	pending := store.OAuthPending{
		ID:            newToken(),
		ClientID:      meta.ClientID,
		ClientName:    meta.ClientName,
		RedirectURI:   redirectURI,
		State:         state,
		CodeChallenge: challenge,
		ExpiresAt:     time.Now().Add(pendingTTL),
	}
	if err := s.store.CreateOAuthPending(r.Context(), pending); err != nil {
		s.log.Error("connector: store pending", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderConsent(w, pending)
}

// consentPage shows who is asking and where the browser will be sent —
// the MCP spec requires the redirect target to be visible on consent,
// because with CIMD any HTTPS URL can present itself as a client (and
// loopback redirects can be claimed by any local process).
var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ochakai — connection request</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 34rem; margin: 4rem auto; padding: 0 1rem; color: #222; }
  dt { font-weight: 600; margin-top: .8rem; } dd { margin: .1rem 0 0; word-break: break-all; }
  .warn { color: #664d03; background: #fff3cd; padding: .6rem .8rem; border-radius: .4rem; margin: 1.2rem 0; }
  button { font-size: 1rem; padding: .55rem 1.4rem; border-radius: .4rem; border: none; background: #1a73e8; color: #fff; cursor: pointer; }
  a.cancel { margin-left: 1rem; color: #555; }
</style>
<h1>Connect to ochakai?</h1>
<p>An MCP client is asking to read and write this knowledge base as you.</p>
<dl>
  <dt>Client</dt><dd>{{.ClientName}}</dd>
  <dt>Client ID</dt><dd>{{.ClientID}}</dd>
  <dt>After sign-in your browser is sent to</dt><dd>{{.RedirectURI}}</dd>
</dl>
<p class="warn">Only continue if you initiated this connection yourself and
the addresses above match the app you are connecting.</p>
<form method="post" action="/oauth/consent">
  <input type="hidden" name="pending" value="{{.ID}}">
  <button type="submit">Continue with Google</button>
  <a class="cancel" href="{{.CancelURL}}">Cancel</a>
</form>
`))

func (s *server) renderConsent(w http.ResponseWriter, p store.OAuthPending) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_ = consentPage.Execute(w, struct {
		store.OAuthPending
		CancelURL string
	}{p, errorURL(p.RedirectURI, p.State, "access_denied", "the user cancelled the request")})
}

// handleConsent is the only step that forwards the browser to Google;
// authorize itself never redirects, so a crafted link can't silently
// chain a Google session into a token.
func (s *server) handleConsent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	p, err := s.store.GetOAuthPending(r.Context(), r.PostForm.Get("pending"))
	if err != nil {
		http.Error(w, "unknown or expired authorization request; restart the connection from your client", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.google.authURL(p.ID, s.cfg.AllowedDomain), http.StatusFound)
}

// handleCallback finishes the Google leg: verify the id_token, enforce
// the domain, mint a single-use authorization code for the client.
func (s *server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := s.store.TakeOAuthPending(r.Context(), q.Get("state"))
	if err != nil {
		http.Error(w, "unknown or expired authorization request; restart the connection from your client", http.StatusBadRequest)
		return
	}
	if errCode := q.Get("error"); errCode != "" {
		redirectError(w, r, p.RedirectURI, p.State, "access_denied", "google sign-in was not completed")
		return
	}
	email, err := s.google.verifiedEmail(r.Context(), q.Get("code"), s.cfg.AllowedDomain)
	if err != nil {
		s.log.Warn("connector: sign-in rejected", "error", err)
		redirectError(w, r, p.RedirectURI, p.State, "access_denied", err.Error())
		return
	}

	code := newToken()
	err = s.store.CreateOAuthCode(r.Context(), store.OAuthCode{
		CodeHash:      hashToken(code),
		ClientID:      p.ClientID,
		RedirectURI:   p.RedirectURI,
		CodeChallenge: p.CodeChallenge,
		ActorEmail:    email,
		ExpiresAt:     time.Now().Add(codeTTL),
	})
	if err != nil {
		s.log.Error("connector: store code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	u, _ := url.Parse(p.RedirectURI)
	qq := u.Query()
	qq.Set("code", code)
	if p.State != "" {
		qq.Set("state", p.State)
	}
	u.RawQuery = qq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func verifierMatches(challenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return verifier != "" && base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func errorURL(redirectURI, state, code, description string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", description)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, description string) {
	http.Redirect(w, r, errorURL(redirectURI, state, code, description), http.StatusFound)
}
