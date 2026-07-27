// Package config loads ochakai configuration from environment variables.
// ochakai targets Google Cloud (Cloud Run + Cloud SQL, optionally Vertex
// AI) exclusively — design doc 0003.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// GCSBucket names the bucket holding attachment bytes as GCS objects
	// (blob/<sha256>, design doc 0013). Auth is ADC. When empty,
	// attachments are unsupported — markdown entries only.
	GCSBucket string

	// Embedding is nil when semantic search is disabled (the default).
	// Set OCHAKAI_VERTEX_PROJECT to enable it.
	Embedding *EmbeddingConfig
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
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL is required")
	}

	if project := os.Getenv("OCHAKAI_VERTEX_PROJECT"); project != "" {
		dim, err := strconv.Atoi(envOr("OCHAKAI_EMBEDDING_DIM", "768"))
		if err != nil || dim <= 0 {
			return nil, fmt.Errorf("OCHAKAI_EMBEDDING_DIM must be a positive integer")
		}
		cfg.Embedding = &EmbeddingConfig{
			Project:  project,
			Location: envOr("OCHAKAI_VERTEX_LOCATION", "us-central1"),
			Model:    envOr("OCHAKAI_VERTEX_MODEL", "gemini-embedding-001"),
			Dim:      dim,
		}
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
