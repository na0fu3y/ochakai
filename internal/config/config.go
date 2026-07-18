// Package config loads ochakai configuration from environment variables.
// ochakai targets Google Cloud (Cloud Run + Cloud SQL, optionally Vertex
// AI) exclusively — design doc 0003.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	// Connector is nil unless connector mode is enabled (design doc
	// 0010): the publicly reachable second service that guards /mcp with
	// OAuth for claude.ai / ChatGPT remote connectors. Set
	// OCHAKAI_CONNECTOR_PUBLIC_URL to enable it.
	Connector *ConnectorConfig
}

// ConnectorConfig configures the MCP OAuth connector service: a minimal
// authorization server that delegates login to Google (design doc 0010).
type ConnectorConfig struct {
	// PublicURL is the connector's own public base URL (issuer and
	// resource base), e.g. "https://ochakai-connector-xyz.a.run.app".
	PublicURL string
	// GoogleClientID / GoogleClientSecret identify the organization's
	// Google OAuth client used to delegate login.
	GoogleClientID     string
	GoogleClientSecret string
	// AllowedDomain is the Workspace domain enforced on the id_token's
	// hd claim.
	AllowedDomain string
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

	if publicURL := os.Getenv("OCHAKAI_CONNECTOR_PUBLIC_URL"); publicURL != "" {
		conn, err := connectorFromEnv(publicURL)
		if err != nil {
			return nil, err
		}
		if cfg.InsecureDev {
			return nil, fmt.Errorf("OCHAKAI_CONNECTOR_PUBLIC_URL and OCHAKAI_INSECURE_DEV are mutually exclusive: the connector is the public surface and must never run unauthenticated")
		}
		cfg.Connector = conn
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

// connectorFromEnv validates connector-mode configuration. The public
// URL must be https (http is tolerated for loopback hosts only, for
// local testing against a real Google client).
func connectorFromEnv(publicURL string) (*ConnectorConfig, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("OCHAKAI_CONNECTOR_PUBLIC_URL must be an absolute URL: %q", publicURL)
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("OCHAKAI_CONNECTOR_PUBLIC_URL must be https (got %q)", publicURL)
	}
	conn := &ConnectorConfig{
		PublicURL:          strings.TrimRight(publicURL, "/"),
		GoogleClientID:     os.Getenv("OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("OCHAKAI_CONNECTOR_GOOGLE_CLIENT_SECRET"),
		AllowedDomain:      os.Getenv("OCHAKAI_CONNECTOR_ALLOWED_DOMAIN"),
	}
	if conn.GoogleClientID == "" || conn.GoogleClientSecret == "" || conn.AllowedDomain == "" {
		return nil, fmt.Errorf("connector mode needs all of OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID, OCHAKAI_CONNECTOR_GOOGLE_CLIENT_SECRET, OCHAKAI_CONNECTOR_ALLOWED_DOMAIN (design doc 0010)")
	}
	return conn, nil
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
