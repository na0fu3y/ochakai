// Package config loads ochakai configuration from environment variables.
// ochakai targets Google Cloud (Cloud Run + Cloud SQL, optionally Vertex
// AI) exclusively — design doc 0003.
package config

import (
	"context"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
)

type Config struct {
	// Addr is the listen address, ":" + PORT (Cloud Run's contract).
	Addr string
	// DatabaseURL is the Cloud SQL connection string (required).
	DatabaseURL string
	// DBIAMAuth enables Cloud SQL IAM database authentication: the
	// connection password is a short-lived access token fetched from the
	// GCE metadata server, so no database password exists anywhere.
	DBIAMAuth bool
	// InsecureDev disables authentication for local development: every
	// request acts as human:anonymous. Never enable on a deployment.
	//
	// This and the two posture fields below are derived from OCHAKAI_MODE
	// and never set independently, which is what makes the combinations
	// that had to be refused or corrected unspellable (design doc 0060).
	InsecureDev bool
	// Delegators lists the caller identities allowed to forward an end
	// user's identity with Ochakai-On-Behalf-Of (design doc 0027): the
	// service accounts of applications that embed ochakai and serve many
	// people. "*" trusts every authenticated caller. Empty (the default)
	// disables delegation, and a header from anyone not listed is an
	// error rather than a silent downgrade — a caller that believes it is
	// writing as tanaka must not find out later that it was not.
	//
	// This is not authorization: it decides whose identity claim is
	// recorded, not what anyone may do. Every caller that reaches ochakai
	// can already read and write everything (design doc 0002).
	Delegators []string

	// Admins names the principals that may do anything: read and write
	// the whole bundle, run the operations that take it as a whole, and
	// edit the access policy itself (design doc 0109 §3).
	//
	// This one is authorization, and it is configuration rather than
	// data for the reason a policy needs a floor: the rules live in the
	// database, and whoever may edit them may grant themselves anything,
	// so the answer to "who may edit them" cannot also live there. It
	// arrives from the deployment's own configuration, which is the one
	// place inside ochakai nobody can reach through ochakai.
	//
	// Spelled as principals — human:name, process:name, or "*" — the
	// same way the policy's rows and the ledger's actors are spelled
	// (0065 §2). Empty is the default, and a deployment with a policy
	// but no administrators refuses to start rather than lock its
	// operator out of it (0109 §3).
	Admins []string

	// OIDCIssuer and OIDCAudience configure the second way a deployment
	// can say who is calling: an OpenID Connect issuer of its own, for a
	// deployment that is not behind Cloud Run's IAM check (design doc
	// 0086). Both are set together or neither is — an issuer without an
	// audience would accept a token it minted for some other service,
	// which is the confused deputy `aud` exists to prevent, so the pair
	// is checked at startup rather than left to be discovered.
	//
	// No secret arrives with them: verification reads the issuer's
	// public keys over HTTPS, so a deployment configured this way still
	// has nothing to issue and nothing to rotate (0065, 0003).
	OIDCIssuer   string
	OIDCAudience string

	// Verifier is built from the pair above at startup, and is nil on the
	// Cloud Run path — where the token was verified in front of the
	// process and parsing it is all that is left to do.
	Verifier any

	// ReadOnly makes the deployment refuse every change to knowledge
	// (design doc 0040). It is not authorization: it does not look at the
	// caller, and it cannot be narrowed to some entries or some people —
	// it says this deployment does not write, for everyone equally,
	// including whoever operates it. Usage telemetry still records, being
	// the server's own observation rather than content a caller wrote.
	ReadOnly bool

	// Sandbox is the disposable public deployment (design doc 0087):
	// anonymous, writable, and restored on a schedule by whoever runs
	// it. The server's part is to say so — anything written here is
	// going to be erased, and a caller that does not know that may
	// curate into it.
	Sandbox bool

	// PublicReadOnly is the posture for a deployment anyone may reach: a
	// demo, or a reference-only copy handed out (design doc 0042). It
	// reads no identity at all — the Authorization header is ignored,
	// because without Cloud Run IAM in front nothing verified its
	// signature and believing it would let any caller name any person;
	// delegation is ignored for the same reason; every caller is
	// human:anonymous and none is refused.
	//
	// It implies ReadOnly and cannot be separated from it. Not reading
	// provenance is only defensible because nothing is written, so a
	// deployment that is publicly readable and writable is not a
	// configuration this program accepts — and since 0060 it is not one
	// anybody can write down either: OCHAKAI_MODE=public is one word, and
	// there is no second variable to disagree with it.
	PublicReadOnly bool

	// GCSBucket names the bucket holding file bytes as GCS objects
	// (blob/<sha256>, design doc 0013). Auth is ADC. When empty,
	// files are unsupported — markdown concepts only.
	GCSBucket string

	// RecordMisses keeps the searches that found nothing, with the query
	// as it was typed (design doc 0051 §3.4). It is the one thing
	// ochakai stores that a caller did not choose to curate, so it has
	// an off switch — OCHAKAI_RECORD_MISSES=false — and it is off by
	// construction on a public deployment, which reads no identity and
	// would be keeping strangers' questions where every other stranger
	// can read them.
	//
	// Default on: the list of unanswered questions is what tells a
	// curator what to write next, and a measurement nobody switches on
	// is a measurement nobody has.
	RecordMisses bool

	// Embedding is nil when semantic search is off. It is filled from
	// the model resource name in OCHAKAI_EMBEDDINGS here, and by
	// EnableDiscoveredEmbedding when the deployment is running on Google
	// Cloud and named nothing — semantic search is the default there
	// (design doc 0080 §1).
	Embedding *EmbeddingConfig

	// EmbeddingsOff says the deployment refuses semantic search it would
	// otherwise get (OCHAKAI_EMBEDDINGS=off, design doc 0080 §2). It is
	// not the same as naming nothing, which means "discover it": this is
	// the one way to run lexical-only on Google Cloud, and it is what a
	// deployment sets when it wants no Vertex AI call made on its behalf
	// at all.
	EmbeddingsOff bool
}

// EmbeddingConfig enables hybrid search via Vertex AI embeddings
// (ADC auth, no API keys); see design doc 0080 §1.1.
// Model gemini-embedding-2 (locations global/us/eu) also embeds image
// and PDF files for search (design doc 0080 §5).
type EmbeddingConfig struct {
	Project  string
	Location string // e.g. "us-central1"; "global" for gemini-embedding-2
	Model    string // e.g. "gemini-embedding-001" or "gemini-embedding-2"
	Dim      int    // output dimensionality stored in pgvector
	// Discovered records that this is the product's default rather than
	// something the deployment wrote down: the project came off the
	// metadata server, and the model and location are ochakai's own. It
	// decides what a failure means — a deployment that named a model
	// asked for semantic search and is told when it is not there, while
	// a discovered one falls back to lexical search rather than refusing
	// to start (design doc 0080 §1.3).
	Discovered bool
}

// splitList parses a comma-separated env var, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Mode is what a deployment is, as one word. The postures are exclusive
// by construction rather than by rule: three booleans could spell a
// combination that had to be rejected at startup, and two more that had
// to be silently corrected, while a mode can only be one of these
// (design doc 0060).
//
// The two axes underneath are whether the caller is identified and
// whether anyone may write, and each spelling below is one cell of that
// square. The fourth cell — anonymous and writable — is ModeDev, which
// says in its name that it is not for a deployment.
const (
	// ModeDefault is the empty value: Cloud Run IAM decides who reaches
	// the deployment, and whoever reaches it can read and write
	// (design doc 0002).
	ModeDefault = ""
	// ModeReadOnly serves knowledge without changing it. The caller is
	// still identified; every write is refused, including the
	// operator's (design doc 0040).
	ModeReadOnly = "read-only"
	// ModePublic is the posture for a deployment anyone may reach. It
	// reads no identity at all and refuses every write (design doc
	// 0042).
	ModePublic = "public"
	// ModeDev disables authentication for local development: every
	// request acts as human:anonymous and writes are allowed. Never on a
	// deployment.
	ModeDev = "dev"
	// ModeSandbox is the fourth cell of the square deployed on purpose:
	// anonymous and writable, reachable by anyone, and **disposable**
	// (design doc 0087). It exists so the loop this product is about —
	// draft, ruling, outcome — can be tried without standing up a
	// database, which the read-only demo could never show.
	//
	// It is not ModeDev under another name. dev says in its own
	// documentation that it is not for a deployment, and an operator
	// reading a config cannot tell an accident from an intent; a word of
	// its own can be granted allUsers deliberately, and — the part that
	// matters — it makes the impermanence something the product says
	// rather than something a README hopes was read. A sandbox that does
	// not announce itself steals the work somebody curated into it.
	ModeSandbox = "sandbox"
)

// Modes is every spelling OCHAKAI_MODE takes, for the error that lists
// them.
var Modes = []string{ModeReadOnly, ModePublic, ModeDev, ModeSandbox}

// Anonymous reports whether no identity is read from the request at all:
// the Authorization header is ignored, delegation is ignored, and every
// caller is human:anonymous. Both postures anyone may reach answer yes,
// for the same reason — nothing in front of the process verified a
// token, so believing one would let any caller name any person (design
// docs 0042 §2.2, 0087 §3).
//
// Derived rather than stored, and that is the point: a field would be
// one more thing a hand-built config could set inconsistently, which is
// the class of mistake one word for the posture exists to remove
// (design doc 0060).
func (c *Config) Anonymous() bool { return c.PublicReadOnly || c.Sandbox }

func FromEnv() (*Config, error) {
	cfg := &Config{
		Addr:        ":" + envOr("PORT", "8080"),
		DatabaseURL: os.Getenv("OCHAKAI_DATABASE_URL"),
		DBIAMAuth:   os.Getenv("OCHAKAI_DB_IAM_AUTH") == "true",
		Admins:      splitList(os.Getenv("OCHAKAI_ADMINS")),
		Delegators:  splitList(os.Getenv("OCHAKAI_DELEGATING_CALLERS")),
		GCSBucket:   os.Getenv("OCHAKAI_GCS_BUCKET"),
		OIDCIssuer:  os.Getenv("OCHAKAI_OIDC_ISSUER"),

		OIDCAudience: os.Getenv("OCHAKAI_OIDC_AUDIENCE"),
		// The only default-on boolean here, so the only one read as
		// "anything but false": an operator turning it off writes false.
		RecordMisses: os.Getenv("OCHAKAI_RECORD_MISSES") != "false",
	}
	switch mode := os.Getenv("OCHAKAI_MODE"); mode {
	case ModeDefault:
	case ModeReadOnly:
		cfg.ReadOnly = true
	case ModePublic:
		// The implication is spelled here rather than checked: there is
		// no way to ask for the public posture and not get read-only, so
		// the dangerous combination never exists to be rejected (design
		// doc 0042 §2.1).
		cfg.PublicReadOnly = true
		cfg.ReadOnly = true
		// Same shape, same reason: a public deployment reads no identity
		// (0042 §2.2), so it does not keep what its callers typed either
		// — and OCHAKAI_RECORD_MISSES=true alongside does not turn it
		// back on (design doc 0051 §3.4).
		cfg.RecordMisses = false
	case ModeDev:
		cfg.InsecureDev = true
	case ModeSandbox:
		// Anonymous like public, writable like the default. The
		// anonymity is spelled here rather than checked, as public's
		// read-only is (design doc 0042 §2.1): there is no way to ask
		// for a sandbox and get a deployment that records who wrote
		// what, so the combination that would be wrong never exists.
		cfg.Sandbox = true
		// And it keeps nothing its visitors typed, for public's reason
		// (0042 §2.2, 0051 §3.4): a deployment that does not identify
		// its callers does not collect what strangers asked it.
		cfg.RecordMisses = false
	default:
		// Not a posture, and guessing at one is the wrong way to be
		// wrong: a deployment that misspelled "read-only" and got a
		// writable one would find out from its knowledge, not from its
		// logs (design doc 0060 §2.3).
		return nil, fmt.Errorf("OCHAKAI_MODE is %q; it takes %s, or is unset for the default posture",
			mode, strings.Join(Modes, " / "))
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL is required")
	}
	for _, a := range cfg.Admins {
		// Refused at startup rather than left to never match: an
		// administrator spelled "tanaka@example.co.jp" instead of
		// "human:tanaka@example.co.jp" is a deployment whose operator
		// believes they have access and finds out from a 404 (design
		// doc 0109 §3).
		if !domain.ValidPrincipal(a) {
			return nil, fmt.Errorf("OCHAKAI_ADMINS has %q; spell each one human:<name>, process:<name>, or %q",
				a, domain.AnyPrincipal)
		}
	}

	switch v := os.Getenv("OCHAKAI_EMBEDDINGS"); v {
	case "", embeddingsOn:
	case embeddingsOff:
		cfg.EmbeddingsOff = true
	default:
		// The third form is a model resource name, and anything that is
		// not one of the three is a startup error. Guessing is the wrong
		// way to be wrong here twice over: a deployment that spelled the
		// refusal "false" would pay Vertex AI for every write it asked
		// not to make, and one that misspelled a model would find out
		// from its search results rather than from its logs (design doc
		// 0080 §2).
		e, err := embeddingFromResourceName(v)
		if err != nil {
			return nil, err
		}
		cfg.Embedding = e
	}

	// The pair is refused at startup rather than half-honoured: an
	// issuer with no audience accepts tokens minted for other services,
	// and an audience with no issuer authenticates nothing while looking
	// configured. Both are the shape of failure design doc 0027 §5.2
	// refuses — a deployment that believes it is authenticating must not
	// find out otherwise from an audit.
	if (cfg.OIDCIssuer == "") != (cfg.OIDCAudience == "") {
		return nil, fmt.Errorf(
			"OCHAKAI_OIDC_ISSUER and OCHAKAI_OIDC_AUDIENCE are set together or not at all (design doc 0086)")
	}
	if cfg.OIDCIssuer != "" && (cfg.InsecureDev || cfg.Anonymous()) {
		// Both of those postures answer "who is calling" without reading
		// anything, so an issuer beside them is a configuration that
		// says two different things. Naming the conflict is the whole
		// reason OCHAKAI_MODE became one word (design doc 0060).
		return nil, fmt.Errorf(
			"OCHAKAI_OIDC_ISSUER cannot be combined with OCHAKAI_MODE=%s: that posture reads no identity",
			os.Getenv("OCHAKAI_MODE"))
	}
	return cfg, nil
}

// Embedding spellings OCHAKAI_EMBEDDINGS takes besides a Vertex AI model
// resource name (design doc 0080 §2).
const (
	embeddingsOn  = "on"
	embeddingsOff = "off"
)

// The product's own choice of model, used wherever nobody named one.
// Changing it here would leave every deployment's stored vectors in a
// space nothing queries, so it is a decision rather than a default to
// tune (design doc 0080 §5).
//
// There is no companion default location: the location is the region the
// deployment is running in, which discovery reads rather than the product
// choosing (design doc 0080 §1.2).
const defaultEmbeddingModel = "gemini-embedding-001"

// embeddingResourceForm is the third spelling, quoted back in every error
// that refuses one.
const embeddingResourceForm = "projects/<project>/locations/<location>/publishers/google/models/<model>"

// embeddingFromResourceName reads the one spelling that carries a project,
// a location and a model at once — the Vertex AI model resource name, the
// same string the API is called with. A deployment that writes it has
// asked for semantic search by name, so Discovered stays false and a
// Vertex AI that does not answer stops the start (design doc 0080 §1.3).
func embeddingFromResourceName(v string) (*EmbeddingConfig, error) {
	unreadable := fmt.Errorf("OCHAKAI_EMBEDDINGS is %q; it takes %q, %q, or a Vertex AI model resource name (%s)",
		v, embeddingsOn, embeddingsOff, embeddingResourceForm)
	p := strings.Split(v, "/")
	if len(p) != 8 || p[0] != "projects" || p[2] != "locations" ||
		p[4] != "publishers" || p[5] != "google" || p[6] != "models" ||
		p[1] == "" || p[3] == "" || p[7] == "" {
		return nil, unreadable
	}
	// The dimension is the model's, not the deployment's: ochakai carries
	// one per model it knows, and for a model it does not know it has no
	// width to ask for (design doc 0080 §3).
	dim, ok := embed.Dimension(p[7])
	if !ok {
		return nil, fmt.Errorf("OCHAKAI_EMBEDDINGS names the model %q, which ochakai does not know a vector width for; it knows %s",
			p[7], strings.Join(embed.Models(), " / "))
	}
	return &EmbeddingConfig{Project: p[1], Location: p[3], Model: p[7], Dim: dim}, nil
}

// EnableDiscoveredEmbedding turns semantic search on for a deployment
// nobody configured — it is running on Google Cloud, and that is where
// embeddings are the default (design doc 0080 §1). What discovery
// supplies is the project *and the region*; the model and the width that
// follows from it are the product's own.
//
// Both are required. Embedding somewhere the deployment did not choose
// is a data-residency decision, and this is not the place to make one on
// an operator's behalf — a region nobody could read leaves semantic
// search to be asked for by name (design doc 0080 §1.2).
//
// A deployment that set OCHAKAI_EMBEDDINGS=off never reaches here, and
// one that named a model keeps it.
func (c *Config) EnableDiscoveredEmbedding(project, region string) {
	if c.EmbeddingsOff || c.Embedding != nil || project == "" || region == "" {
		return
	}
	// Known by construction: TestTheDefaultModelHasAWidth holds it.
	dim, _ := embed.Dimension(defaultEmbeddingModel)
	c.Embedding = &EmbeddingConfig{
		Project:    project,
		Location:   region,
		Model:      defaultEmbeddingModel,
		Dim:        dim,
		Discovered: true,
	}
}

// DiscoverVertex names the Google Cloud project and region this process
// is running in, or "", "" when it is not running on Google Cloud. The
// answers come from the metadata server, which is the same source Cloud
// SQL IAM auth already takes its access token from — no configuration,
// and nothing to hold (design doc 0003).
//
// The region is half the answer because it decides where the text goes:
// ochakai embeds in the region it was deployed to, so a deployment in
// asia-northeast1 does not send concept bodies and search queries to
// another continent to have them embedded (design doc 0080 §1.2).
//
// Together they say where ochakai runs, not what it may do there:
// whether the service identity can call Vertex AI, and whether the model
// exists in that region at all, is one answer given by the probe at
// startup (design doc 0080 §1.3).
func DiscoverVertex(ctx context.Context) (project, region string) {
	if !metadata.OnGCE() {
		return "", ""
	}
	project, err := metadata.ProjectIDWithContext(ctx)
	if err != nil {
		return "", ""
	}
	return project, discoverRegion(ctx)
}

// discoverRegion reads the region out of the metadata server, or ""
// when it cannot be read. Cloud Run — the deployment this project
// documents (design doc 0003) — answers `instance/region` with
// projects/<number>/regions/<region>. A GCE VM has no such key and
// answers `instance/zone` with projects/<number>/zones/<region>-<letter>
// instead, so the zone's last segment is dropped to get back to a
// region.
func discoverRegion(ctx context.Context) string {
	if v, err := metadata.GetWithContext(ctx, "instance/region"); err == nil {
		if r := v[strings.LastIndex(v, "/")+1:]; r != "" {
			return r
		}
	}
	zone, err := metadata.ZoneWithContext(ctx)
	if err != nil {
		return ""
	}
	if i := strings.LastIndex(zone, "-"); i > 0 {
		return zone[:i]
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
