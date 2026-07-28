package httpauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// fakeIDToken builds an unsigned JWT-shaped token with the given payload,
// mimicking what Cloud Run forwards (X-Serverless-Authorization arrives
// with its signature replaced).
func fakeIDToken(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(payload)) + ".SIGNATURE_REMOVED_BY_GOOGLE"
}

func TestActorFromIDToken(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		want    domain.Actor
		wantErr bool
	}{
		{
			name:  "human user account",
			token: fakeIDToken(`{"email":"na0@example.com","email_verified":true}`),
			want:  domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.com"},
		},
		{
			name:  "service account is an agent",
			token: fakeIDToken(`{"email":"bot@myproj.iam.gserviceaccount.com"}`),
			want:  domain.Actor{Kind: domain.ActorProcess, Name: "bot@myproj.iam.gserviceaccount.com"},
		},
		{name: "empty", token: "", wantErr: true},
		{name: "not a jwt", token: "abc", wantErr: true},
		{name: "no email claim", token: fakeIDToken(`{"sub":"123"}`), wantErr: true},
	}
	for _, tc := range cases {
		got, err := actorFromIDToken(tc.token)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got %+v", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCloudRunIAMPrefersServerlessHeader(t *testing.T) {
	// Cloud Run validates only X-Serverless-Authorization when both are
	// present; trusting Authorization instead would allow impersonation.
	cfg := &config.Config{}
	var got domain.Actor
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Actor(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil)
	r.Header.Set("Authorization", "Bearer "+fakeIDToken(`{"email":"forged@example.com"}`))
	r.Header.Set("X-Serverless-Authorization", "Bearer "+fakeIDToken(`{"email":"real@example.com"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got.Name != "real@example.com" {
		t.Errorf("actor = %+v; must come from X-Serverless-Authorization", got)
	}
}

func TestCloudRunIAMRejectsMissingToken(t *testing.T) {
	cfg := &config.Config{}
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestInsecureDevActsAsAnonymous(t *testing.T) {
	cfg := &config.Config{InsecureDev: true}
	var got domain.Actor
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Actor(r.Context())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got.Kind != domain.ActorHuman || got.Name != "anonymous" {
		t.Errorf("actor = %+v, want human:anonymous", got)
	}
}

// Delegation records both identities. The forwarded one is the actor; the
// caller stays as Via, so an entry written by tanaka through an embedded
// application never reads like one tanaka wrote directly (design doc
// 0027).
func TestDelegate(t *testing.T) {
	sa := domain.Actor{Kind: domain.ActorProcess, Name: "insightflow@example.iam.gserviceaccount.com"}
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.co.jp"}
	allowed := []string{sa.Name}

	t.Run("no header acts as the caller", func(t *testing.T) {
		for _, headers := range [][]string{nil, {""}} {
			got, status, err := delegate(sa, headers, allowed)
			if err != nil || status != 0 || got != sa {
				t.Errorf("delegate(%q) = (%v, %d, %v), want the caller unchanged", headers, got, status, err)
			}
		}
	})

	// Two identities in one request is ambiguous, and picking the first
	// would attribute the write to whichever the sender happened to put
	// there first — silent degradation, which 0027 §5.2 refuses.
	t.Run("a repeated header is refused, not resolved", func(t *testing.T) {
		got, status, err := delegate(sa,
			[]string{"human:tanaka@example.co.jp", "human:suzuki@example.co.jp"}, allowed)
		if err == nil || status != http.StatusBadRequest {
			t.Errorf("got (%v, %d, %v), want a 400", got, status, err)
		}
	})

	t.Run("permitted caller delegates", func(t *testing.T) {
		got, _, err := delegate(sa, []string{"human:tanaka@example.co.jp"}, allowed)
		if err != nil {
			t.Fatalf("delegate: %v", err)
		}
		want := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp", Via: sa.String()}
		if got != want {
			t.Errorf("actor = %+v, want %+v", got, want)
		}
		if !strings.Contains(got.String(), "via process:insightflow@") {
			t.Errorf("rendered provenance hides the delegation: %s", got)
		}
	})

	// Silently ignoring the header would be worse than refusing it: the
	// application goes on believing it writes as its users.
	t.Run("unlisted caller is refused, not downgraded", func(t *testing.T) {
		got, status, err := delegate(human, []string{"human:tanaka@example.co.jp"}, allowed)
		if err == nil {
			t.Fatalf("delegation by an unlisted caller succeeded as %+v", got)
		}
		if status != http.StatusForbidden {
			t.Errorf("status = %d, want 403", status)
		}
	})

	t.Run("wildcard trusts every authenticated caller", func(t *testing.T) {
		got, _, err := delegate(human, []string{"human:tanaka@example.co.jp"}, []string{"*"})
		if err != nil || got.Name != "tanaka@example.co.jp" || got.Via != human.String() {
			t.Errorf("got (%+v, %v), want the delegation accepted", got, err)
		}
	})

	t.Run("delegation is off by default", func(t *testing.T) {
		if _, status, err := delegate(sa, []string{"human:tanaka@example.co.jp"}, nil); err == nil || status != http.StatusForbidden {
			t.Errorf("empty allowlist must refuse: status %d, err %v", status, err)
		}
	})

	for _, bad := range []string{
		"tanaka@example.co.jp",      // no kind: the kind is never guessed
		"root:tanaka@example.co.jp", // unknown kind
		"human:",                    // no identity
		"human: tanaka with spaces", // whitespace in the identity
		"human:" + strings.Repeat("x", 400),
	} {
		t.Run("rejects "+bad[:min(len(bad), 20)], func(t *testing.T) {
			_, status, err := delegate(sa, []string{bad}, allowed)
			if err == nil {
				t.Errorf("accepted malformed header %q", bad)
			} else if status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", status)
			}
		})
	}
}

// Local development is where an embedding host's delegation gets written,
// so the header has to work there. Ignoring it would hide a malformed
// header until the first deployment, which is the silent downgrade design
// doc 0027 §5.2 exists to prevent — just relocated to the place it is
// hardest to notice.
func TestInsecureDevHonorsDelegation(t *testing.T) {
	srv := httptest.NewServer(Middleware(&config.Config{InsecureDev: true},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, Actor(r.Context()).String())
		})))
	defer srv.Close()

	get := func(t *testing.T, header string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		if header != "" {
			req.Header.Set(OnBehalfOfHeader, header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	if _, got := get(t, ""); got != "human:anonymous" {
		t.Errorf("without the header: got %q, want human:anonymous", got)
	}
	_, got := get(t, "human:tanaka@example.co.jp")
	if want := "human:tanaka@example.co.jp via human:anonymous"; got != want {
		t.Errorf("with the header: got %q, want %q", got, want)
	}
	// A malformed header must fail here, where the developer can see it.
	if status, _ := get(t, "tanaka@example.co.jp"); status != http.StatusBadRequest {
		t.Errorf("malformed header status = %d, want 400", status)
	}
}

// A rejection from the middleware is an API error like any other, and the
// message is the useful part: an unpermitted delegator is told to add
// itself to OCHAKAI_DELEGATING_CALLERS. Clients read the documented
// {"error": ...} envelope (internal/apiclient does), so text/plain here
// meant they reported a bare "Forbidden" and dropped the instruction.
func TestMiddlewareRejectsInTheAPIErrorEnvelope(t *testing.T) {
	cfg := &config.Config{Delegators: []string{"someone-else@example.iam.gserviceaccount.com"}}
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("a rejected request must not reach the handler")
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil)
	r.Header.Set("X-Serverless-Authorization", "Bearer "+fakeIDToken(`{"email":"stranger@example.iam.gserviceaccount.com"}`))
	r.Header.Set(OnBehalfOfHeader, "human:tanaka@example.co.jp")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want the JSON error envelope", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the documented envelope: %v (%q)", err, w.Body.String())
	}
	if !strings.Contains(body.Error, "OCHAKAI_DELEGATING_CALLERS") {
		t.Errorf("error drops the actionable part: %q", body.Error)
	}
}

// The producer rides on the actor the write is finally recorded as, and
// never in place of it. Everything the record already said stays said:
// the authenticated caller, and — under delegation — the person it acted
// for (design doc 0049 §3.1).
func TestProducerIsRecordedBesideTheActor(t *testing.T) {
	const sa = "insightflow@example.iam.gserviceaccount.com"
	cfg := &config.Config{Delegators: []string{sa}}

	req := func(producer ...string) http.Header {
		h := http.Header{}
		h.Set("X-Serverless-Authorization", "Bearer "+fakeIDToken(`{"email":"`+sa+`"}`))
		for _, p := range producer {
			h.Add(ProducerHeader, p)
		}
		return h
	}

	t.Run("no header leaves it empty", func(t *testing.T) {
		got, _, err := ActorFromHeader(cfg, req())
		if err != nil || got.Producer != "" {
			t.Errorf("got (%+v, %v), want no producer", got, err)
		}
	})

	t.Run("recorded without an allowlist", func(t *testing.T) {
		// Unlike delegation there is nothing to permit: the name is the
		// caller's own, and the caller is authenticated and still in the
		// record beside it.
		got, _, err := ActorFromHeader(cfg, req("insightflow/1.4.0"))
		if err != nil {
			t.Fatalf("ActorFromHeader: %v", err)
		}
		want := domain.Actor{Kind: domain.ActorProcess, Name: sa, Producer: "insightflow/1.4.0"}
		if got != want {
			t.Errorf("actor = %+v, want %+v", got, want)
		}
	})

	t.Run("lands on the delegated actor, keeping the caller", func(t *testing.T) {
		h := req("insightflow/1.4.0")
		h.Set(OnBehalfOfHeader, "human:tanaka@example.co.jp")
		got, _, err := ActorFromHeader(cfg, h)
		if err != nil {
			t.Fatalf("ActorFromHeader: %v", err)
		}
		want := "human:tanaka@example.co.jp via process:" + sa + " using insightflow/1.4.0"
		if got.String() != want {
			t.Errorf("actor = %q, want %q", got, want)
		}
	})

	// A malformed value is refused rather than dropped: an integration
	// that believes it is stamping its version must not find out at the
	// first export that nothing was recorded (0027 §5.2, 0049 §3.2).
	for _, bad := range []string{
		"insightflow",                     // no version
		"insightflow/",                    // empty version
		"/1.4.0",                          // empty producer
		"insight flow/1.4.0",              // whitespace
		"a/b/c",                           // two slashes
		"process:insightflow/1.4.0",       // reads as SPEC §7's identity form
		strings.Repeat("x", 130) + "/1.0", // past the bound
	} {
		t.Run("refuses "+bad, func(t *testing.T) {
			_, status, err := ActorFromHeader(cfg, req(bad))
			if err == nil || status != http.StatusBadRequest {
				t.Errorf("got (%d, %v), want a 400", status, err)
			}
		})
	}

	t.Run("a repeated header is refused, not resolved", func(t *testing.T) {
		_, status, err := ActorFromHeader(cfg, req("a/1", "b/2"))
		if err == nil || status != http.StatusBadRequest {
			t.Errorf("got (%d, %v), want a 400", status, err)
		}
	})
}

// A public deployment authenticates nobody, so there is no name for a
// self-declaration to hang off — the header is not read there at all
// (design docs 0042, 0049 §3.3).
func TestPublicReadOnlyIgnoresTheProducerHeader(t *testing.T) {
	cfg := &config.Config{PublicReadOnly: true, ReadOnly: true}
	h := http.Header{}
	h.Set(ProducerHeader, "not even a valid one")
	got, _, err := ActorFromHeader(cfg, h)
	if err != nil {
		t.Fatalf("ActorFromHeader: %v", err)
	}
	if got.Producer != "" {
		t.Errorf("producer = %q, want it unread", got.Producer)
	}
}
