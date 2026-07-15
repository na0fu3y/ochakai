// Package config loads ochakai configuration from environment variables.
// The only hard dependency is PostgreSQL; everything else is opt-in.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Client maps a bearer token to the actor it authenticates.
type Client struct {
	Token string
	Actor domain.Actor
}

type Config struct {
	// Addr is the listen address. Cloud Run's PORT is honored when set.
	Addr string
	// DatabaseURL is the PostgreSQL connection string (required).
	DatabaseURL string
	// Clients maps bearer tokens to actors. Empty means auth is disabled
	// and every request acts as human/anonymous (local development only).
	Clients []Client
	// VerifyActorKinds lists actor kinds allowed to set status=verified.
	// Default: human only.
	VerifyActorKinds []string
	// CORSOrigins is the exact-match allowlist of origins permitted to call
	// the REST API from a browser (for separately hosted web UIs). Empty
	// (the default) emits no CORS headers at all.
	CORSOrigins []string

	// Embedding is nil when semantic search is disabled (the default).
	Embedding *EmbeddingConfig
}

// EmbeddingConfig enables hybrid search. The only bundled driver is
// Vertex AI (ADC auth, no API keys); see design doc §4.
type EmbeddingConfig struct {
	Provider string // "vertex"
	Project  string
	Location string // e.g. "us-central1" or "global"
	Model    string // e.g. "gemini-embedding-001"
	Dim      int    // output dimensionality stored in pgvector
}

func FromEnv() (*Config, error) {
	cfg := &Config{
		Addr:             ":" + envOr("PORT", "8080"),
		DatabaseURL:      firstEnv("OCHAKAI_DATABASE_URL", "DATABASE_URL"),
		VerifyActorKinds: splitList(envOr("OCHAKAI_VERIFY_ACTORS", domain.ActorHuman)),
		CORSOrigins:      splitList(os.Getenv("OCHAKAI_CORS_ORIGINS")),
	}
	if addr := os.Getenv("OCHAKAI_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("OCHAKAI_DATABASE_URL (or DATABASE_URL) is required")
	}

	clients, err := parseClients(os.Getenv("OCHAKAI_CLIENTS"))
	if err != nil {
		return nil, err
	}
	cfg.Clients = clients

	switch provider := os.Getenv("OCHAKAI_EMBEDDING_PROVIDER"); provider {
	case "":
		// semantic search disabled; trigram-only
	case "vertex":
		dim, err := strconv.Atoi(envOr("OCHAKAI_EMBEDDING_DIM", "768"))
		if err != nil || dim <= 0 {
			return nil, fmt.Errorf("OCHAKAI_EMBEDDING_DIM must be a positive integer")
		}
		emb := &EmbeddingConfig{
			Provider: provider,
			Project:  os.Getenv("OCHAKAI_VERTEX_PROJECT"),
			Location: envOr("OCHAKAI_VERTEX_LOCATION", "us-central1"),
			Model:    envOr("OCHAKAI_VERTEX_MODEL", "gemini-embedding-001"),
			Dim:      dim,
		}
		if emb.Project == "" {
			return nil, fmt.Errorf("OCHAKAI_VERTEX_PROJECT is required when OCHAKAI_EMBEDDING_PROVIDER=vertex")
		}
		cfg.Embedding = emb
	default:
		return nil, fmt.Errorf("unknown OCHAKAI_EMBEDDING_PROVIDER %q (supported: vertex)", provider)
	}

	return cfg, nil
}

// parseClients parses "token=kind:name,token2=kind2:name2".
func parseClients(s string) ([]Client, error) {
	if s == "" {
		return nil, nil
	}
	var clients []Client
	for _, entry := range splitList(s) {
		token, actor, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("OCHAKAI_CLIENTS entry %q must be token=kind:name", entry)
		}
		kind, name, ok := strings.Cut(actor, ":")
		if !ok || (kind != domain.ActorHuman && kind != domain.ActorAgent) || name == "" {
			return nil, fmt.Errorf("OCHAKAI_CLIENTS actor %q must be human:<name> or agent:<name>", actor)
		}
		clients = append(clients, Client{Token: token, Actor: domain.Actor{Kind: kind, Name: name}})
	}
	return clients, nil
}

func (c *Config) CanVerify(actor domain.Actor) bool {
	for _, kind := range c.VerifyActorKinds {
		if actor.Kind == kind {
			return true
		}
	}
	return false
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

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
