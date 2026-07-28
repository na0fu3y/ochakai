// Package config loads ochakai configuration from environment variables.
// ochakai targets Google Cloud (Cloud Run + Cloud SQL, optionally Vertex
// AI) exclusively — design doc 0003.
package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cloud.google.com/go/compute/metadata"
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
	InsecureDev bool
	// Delegators lists the caller identities allowed to forward an end
	// user's identity with X-Ochakai-On-Behalf-Of (design doc 0027): the
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
	// configuration this program accepts.
	PublicReadOnly bool

	// GCSBucket names the bucket holding attachment bytes as GCS objects
	// (blob/<sha256>, design doc 0013). Auth is ADC. When empty,
	// attachments are unsupported — markdown entries only.
	GCSBucket string

	// Embedding is nil when semantic search is off. It is filled from
	// OCHAKAI_VERTEX_PROJECT here, and by EnableDiscoveredEmbedding when
	// the deployment is running on Google Cloud and did not name a
	// project — semantic search is the default there (design doc 0049).
	Embedding *EmbeddingConfig

	// EmbeddingsOff says the deployment refuses semantic search it would
	// otherwise get (OCHAKAI_EMBEDDINGS=off, design doc 0049 §2.4). It
	// is not the same as an absent OCHAKAI_VERTEX_PROJECT, which now
	// means "discover it": this is the one way to run lexical-only on
	// Google Cloud, and it is what a deployment sets when it wants no
	// Vertex AI call made on its behalf at all.
	EmbeddingsOff bool
}

// EmbeddingConfig enables hybrid search via Vertex AI embeddings
// (ADC auth, no API keys); see design doc 0001 §4.
// Model gemini-embedding-2 (locations global/us/eu) also embeds image
// and PDF attachments for search (design doc 0020).
type EmbeddingConfig struct {
	Project  string
	Location string // e.g. "us-central1"; "global" for gemini-embedding-2
	Model    string // e.g. "gemini-embedding-001" or "gemini-embedding-2"
	Dim      int    // output dimensionality stored in pgvector
	// Discovered records that Project was read off the metadata server
	// rather than configured. It decides what a failure means: a
	// deployment that named the project asked for semantic search and is
	// told when it is not there, while a discovered one is a default and
	// falls back to lexical search rather than refusing to start
	// (design doc 0049 §2.3).
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

func FromEnv() (*Config, error) {
	cfg := &Config{
		Addr:        ":" + envOr("PORT", "8080"),
		DatabaseURL: os.Getenv("OCHAKAI_DATABASE_URL"),
		DBIAMAuth:   os.Getenv("OCHAKAI_DB_IAM_AUTH") == "true",
		InsecureDev: os.Getenv("OCHAKAI_INSECURE_DEV") == "true",
		ReadOnly:    os.Getenv("OCHAKAI_READ_ONLY") == "true",
		Delegators:  splitList(os.Getenv("OCHAKAI_DELEGATING_CALLERS")),
		GCSBucket:   os.Getenv("OCHAKAI_GCS_BUCKET"),
	}
	if os.Getenv("OCHAKAI_PUBLIC_READ_ONLY") == "true" {
		// The implication is applied here, not checked here: there is no
		// way to ask for the public posture and not get read-only, so the
		// dangerous combination never exists to be rejected (design doc
		// 0042 §2.1). Setting OCHAKAI_READ_ONLY=false alongside it does
		// not turn writes back on.
		cfg.PublicReadOnly = true
		cfg.ReadOnly = true
	}
	if cfg.PublicReadOnly && cfg.InsecureDev {
		// Both make every caller anonymous, but insecure dev also lets
		// anyone delegate, which in public is a stranger naming any
		// person they like. Refuse rather than silently pick one
		// (design doc 0042 §2.3).
		return nil, fmt.Errorf("OCHAKAI_PUBLIC_READ_ONLY and OCHAKAI_INSECURE_DEV are both set: " +
			"the public posture reads no identity, while insecure dev lets any caller claim one")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL is required")
	}

	switch v := os.Getenv("OCHAKAI_EMBEDDINGS"); v {
	case "", "on":
	case "off":
		cfg.EmbeddingsOff = true
	default:
		// Not a boolean, and guessing at one is the wrong way to be
		// wrong: a deployment that spelled the refusal "false" and got
		// semantic search would be paying Vertex AI for every write it
		// asked not to make (design doc 0049 §2.4).
		return nil, fmt.Errorf("OCHAKAI_EMBEDDINGS is %q; it takes \"on\" or \"off\"", v)
	}
	if !cfg.EmbeddingsOff {
		if project := os.Getenv("OCHAKAI_VERTEX_PROJECT"); project != "" {
			e, err := embeddingFromEnv(project, false)
			if err != nil {
				return nil, err
			}
			cfg.Embedding = e
		}
	}

	return cfg, nil
}

// EnableDiscoveredEmbedding turns semantic search on for a project
// nobody configured — the deployment is running on Google Cloud, and
// that is where embeddings are the default (design doc 0049 §2.1). The
// model, location and dimension come from the environment either way:
// what discovery supplies is the project, not the choice of model.
//
// A deployment that set OCHAKAI_EMBEDDINGS=off never reaches here, and
// one that named its own project keeps it.
func (c *Config) EnableDiscoveredEmbedding(project string) error {
	if c.EmbeddingsOff || c.Embedding != nil || project == "" {
		return nil
	}
	e, err := embeddingFromEnv(project, true)
	if err != nil {
		return err
	}
	c.Embedding = e
	return nil
}

// embeddingFromEnv reads the settings around a project: the same ones
// whether the project was configured or discovered.
func embeddingFromEnv(project string, discovered bool) (*EmbeddingConfig, error) {
	dim, err := strconv.Atoi(envOr("OCHAKAI_EMBEDDING_DIM", "768"))
	if err != nil || dim <= 0 {
		return nil, fmt.Errorf("OCHAKAI_EMBEDDING_DIM must be a positive integer")
	}
	return &EmbeddingConfig{
		Project:    project,
		Location:   envOr("OCHAKAI_VERTEX_LOCATION", "us-central1"),
		Model:      envOr("OCHAKAI_VERTEX_MODEL", "gemini-embedding-001"),
		Dim:        dim,
		Discovered: discovered,
	}, nil
}

// DiscoverVertexProject names the Google Cloud project this process is
// running in, or "" when it is not running on Google Cloud. The answer
// comes from the metadata server, which is the same source Cloud SQL IAM
// auth already takes its access token from — no configuration, and
// nothing to hold (design doc 0003).
//
// It says where ochakai runs, not what it may do there: whether the
// service identity can call Vertex AI is IAM's answer, given once the
// first embedding is attempted (design doc 0049 §2.3).
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
