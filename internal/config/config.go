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
	// GCSBucket, when set, stores attachment bytes as GCS objects
	// (blob/<sha256>) instead of Postgres bytea rows; existing inline
	// blobs are migrated out at startup (design doc 0011). Auth is ADC.
	GCSBucket string

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
	cfg := &Config{
		Addr:        ":" + envOr("PORT", "8080"),
		DatabaseURL: firstEnv("OCHAKAI_DATABASE_URL", "DATABASE_URL"),
		DBIAMAuth:   os.Getenv("OCHAKAI_DB_IAM_AUTH") == "true",
		InsecureDev: os.Getenv("OCHAKAI_INSECURE_DEV") == "true",
		GCSBucket:   os.Getenv("OCHAKAI_GCS_BUCKET"),
	}
	if addr := os.Getenv("OCHAKAI_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL (or DATABASE_URL) is required")
	}

	// The MCP OAuth connector service was removed (design doc 0012). This
	// is a refuse-to-start guard, not silent tolerance like other removed
	// variables: connector deployments were publicly invokable (allUsers),
	// and a binary that ignored the variable would serve the trust-the-
	// headers private surface on that public service.
	if os.Getenv("OCHAKAI_CONNECTOR_PUBLIC_URL") != "" {
		return nil, fmt.Errorf("OCHAKAI_CONNECTOR_PUBLIC_URL is set, but the MCP OAuth connector was removed (design doc 0012); this deployment is publicly invokable and must not run this image — delete the connector service (and its allUsers grant) instead of upgrading it")
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

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
