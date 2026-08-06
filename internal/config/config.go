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

	// ReadOnly makes the deployment refuse every change to knowledge
	// (design doc 0040). It is not authorization: it does not look at the
	// caller, and it cannot be narrowed to some entries or some people —
	// it says this deployment does not write, for everyone equally,
	// including whoever operates it. Usage telemetry still records, being
	// the server's own observation rather than content a caller wrote.
	ReadOnly bool

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
)

// Modes is every spelling OCHAKAI_MODE takes, for the error that lists
// them.
var Modes = []string{ModeReadOnly, ModePublic, ModeDev}

func FromEnv() (*Config, error) {
	cfg := &Config{
		Addr:        ":" + envOr("PORT", "8080"),
		DatabaseURL: os.Getenv("OCHAKAI_DATABASE_URL"),
		DBIAMAuth:   os.Getenv("OCHAKAI_DB_IAM_AUTH") == "true",
		Delegators:  splitList(os.Getenv("OCHAKAI_DELEGATING_CALLERS")),
		GCSBucket:   os.Getenv("OCHAKAI_GCS_BUCKET"),
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

	return cfg, nil
}

// Embedding spellings OCHAKAI_EMBEDDINGS takes besides a Vertex AI model
// resource name (design doc 0080 §2).
const (
	embeddingsOn  = "on"
	embeddingsOff = "off"
)

// The product's own choice of model and location, used wherever nobody
// named one. Changing the model here would leave every deployment's
// stored vectors in a space nothing queries, so it is a decision rather
// than a default to tune (design doc 0080 §5).
const (
	defaultEmbeddingModel    = "gemini-embedding-001"
	defaultEmbeddingLocation = "us-central1"
)

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

// EnableDiscoveredEmbedding turns semantic search on for a project
// nobody configured — the deployment is running on Google Cloud, and
// that is where embeddings are the default (design doc 0080 §1.1). What
// discovery supplies is the project; the model, the location and the
// width that follows from the model are the product's own.
//
// A deployment that set OCHAKAI_EMBEDDINGS=off never reaches here, and
// one that named a model keeps it.
func (c *Config) EnableDiscoveredEmbedding(project string) {
	if c.EmbeddingsOff || c.Embedding != nil || project == "" {
		return
	}
	// Known by construction: TestTheDefaultModelHasAWidth holds it.
	dim, _ := embed.Dimension(defaultEmbeddingModel)
	c.Embedding = &EmbeddingConfig{
		Project:    project,
		Location:   defaultEmbeddingLocation,
		Model:      defaultEmbeddingModel,
		Dim:        dim,
		Discovered: true,
	}
}

// DiscoverVertexProject names the Google Cloud project this process is
// running in, or "" when it is not running on Google Cloud. The answer
// comes from the metadata server, which is the same source Cloud SQL IAM
// auth already takes its access token from — no configuration, and
// nothing to hold (design doc 0003).
//
// It says where ochakai runs, not what it may do there: whether the
// service identity can call Vertex AI is IAM's answer, given once the
// first embedding is attempted (design doc 0080 §1.2).
func DiscoverVertexProject(ctx context.Context) string {
	if !metadata.OnGCE() {
		return ""
	}
	project, err := metadata.ProjectIDWithContext(ctx)
	if err != nil {
		return ""
	}
	return project
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
