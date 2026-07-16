// Package config loads ochakai configuration from environment variables.
// ochakai targets Google Cloud (Cloud Run + Cloud SQL, optionally Vertex
// AI) exclusively — design doc 0003.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	// Addr is the listen address. Cloud Run's PORT is honored when set.
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

	// Embedding is nil when semantic search is disabled (the default).
	// Set OCHAKAI_VERTEX_PROJECT to enable it.
	Embedding *EmbeddingConfig
}

// EmbeddingConfig enables hybrid search via Vertex AI embeddings
// (ADC auth, no API keys); see design doc 0001 §4.
type EmbeddingConfig struct {
	Project  string
	Location string // e.g. "us-central1" or "global"
	Model    string // e.g. "gemini-embedding-001"
	Dim      int    // output dimensionality stored in pgvector
}

func FromEnv() (*Config, error) {
	if err := rejectRemovedEnv(); err != nil {
		return nil, err
	}

	cfg := &Config{
		Addr:        ":" + envOr("PORT", "8080"),
		DatabaseURL: firstEnv("OCHAKAI_DATABASE_URL", "DATABASE_URL"),
		DBIAMAuth:   os.Getenv("OCHAKAI_DB_IAM_AUTH") == "true",
		InsecureDev: os.Getenv("OCHAKAI_INSECURE_DEV") == "true",
	}
	if addr := os.Getenv("OCHAKAI_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL (or DATABASE_URL) is required")
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

// rejectRemovedEnv fails fast when configuration removed in v0.3 (design
// doc 0003: Google Cloud only) is still present. Variables whose meaning
// is unchanged are tolerated silently; ones whose behavior would be lost
// refuse to start rather than silently weakening the deployment.
func rejectRemovedEnv() error {
	if os.Getenv("OCHAKAI_CLIENTS") != "" {
		return fmt.Errorf("OCHAKAI_CLIENTS was removed in v0.3: ochakai authenticates via Cloud Run IAM only (docs/design/0003-gcp-only.md); unset it and use IAM invoker grants")
	}
	if os.Getenv("OCHAKAI_CORS_ORIGINS") != "" {
		return fmt.Errorf("OCHAKAI_CORS_ORIGINS was removed in v0.3: host browser UIs behind a same-origin proxy like examples/webui (docs/design/0003-gcp-only.md)")
	}
	switch v := os.Getenv("OCHAKAI_AUTH"); v {
	case "", "cloudrun-iam": // unchanged meaning; tolerated
	default:
		return fmt.Errorf("OCHAKAI_AUTH=%q was removed in v0.3: Cloud Run IAM is the only auth mode (docs/design/0003-gcp-only.md)", v)
	}
	switch v := os.Getenv("OCHAKAI_EMBEDDING_PROVIDER"); v {
	case "", "vertex": // unchanged meaning; tolerated
	default:
		return fmt.Errorf("OCHAKAI_EMBEDDING_PROVIDER=%q is not supported: only Vertex AI (set OCHAKAI_VERTEX_PROJECT to enable)", v)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
