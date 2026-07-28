package config

import (
	"strings"
	"testing"
)

func TestDatabaseURLRequired(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "OCHAKAI_DATABASE_URL") {
		t.Errorf("err = %v, want OCHAKAI_DATABASE_URL requirement", err)
	}
}

func TestGCSBucket(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")

	t.Run("default off", func(t *testing.T) {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GCSBucket != "" {
			t.Errorf("GCSBucket = %q, want empty", cfg.GCSBucket)
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv("OCHAKAI_GCS_BUCKET", "my-blobs")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GCSBucket != "my-blobs" {
			t.Errorf("GCSBucket = %q, want my-blobs", cfg.GCSBucket)
		}
	})
}

// Semantic search is the default where ochakai runs on Google Cloud, so
// the absence of OCHAKAI_VERTEX_PROJECT is no longer the off switch —
// OCHAKAI_EMBEDDINGS=off is (design doc 0049 §2.4). A deployment that
// wants no Vertex AI call made on its behalf must be able to say so and
// be believed, so a spelling that is not "on" or "off" is refused rather
// than read as one of them.
func TestEmbeddingsSwitch(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_VERTEX_PROJECT", "")

	t.Run("off refuses discovery", func(t *testing.T) {
		t.Setenv("OCHAKAI_EMBEDDINGS", "off")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.EmbeddingsOff {
			t.Fatal("OCHAKAI_EMBEDDINGS=off did not turn embeddings off")
		}
		if err := cfg.EnableDiscoveredEmbedding("some-project"); err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding != nil {
			t.Errorf("a discovered project turned embeddings back on: %+v", cfg.Embedding)
		}
	})

	t.Run("a discovered project is not a configured one", func(t *testing.T) {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding != nil {
			t.Fatalf("Embedding = %+v before discovery, want nil", cfg.Embedding)
		}
		if err := cfg.EnableDiscoveredEmbedding("some-project"); err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding == nil || cfg.Embedding.Project != "some-project" {
			t.Fatalf("Embedding = %+v, want the discovered project", cfg.Embedding)
		}
		// The flag is what decides that a Vertex AI that does not answer
		// is a fallback rather than a failure to start.
		if !cfg.Embedding.Discovered {
			t.Error("a discovered project was recorded as configured")
		}
		if cfg.Embedding.Model != "gemini-embedding-001" || cfg.Embedding.Dim != 768 {
			t.Errorf("model=%q dim=%d, want the defaults around the discovered project",
				cfg.Embedding.Model, cfg.Embedding.Dim)
		}
	})

	t.Run("a configured project stays configured", func(t *testing.T) {
		t.Setenv("OCHAKAI_VERTEX_PROJECT", "named-project")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding == nil || cfg.Embedding.Discovered {
			t.Fatalf("Embedding = %+v, want the named project, not a discovered one", cfg.Embedding)
		}
		if err := cfg.EnableDiscoveredEmbedding("some-other-project"); err != nil {
			t.Fatal(err)
		}
		if cfg.Embedding.Project != "named-project" {
			t.Errorf("Project = %q; discovery overrode a configured project", cfg.Embedding.Project)
		}
	})

	t.Run("an unreadable spelling is refused", func(t *testing.T) {
		t.Setenv("OCHAKAI_EMBEDDINGS", "false")
		if _, err := FromEnv(); err == nil {
			t.Error("OCHAKAI_EMBEDDINGS=false was accepted; a deployment that means off must not get on")
		}
	})
}

// The public posture implies read-only and cannot be separated from it:
// not reading provenance is only defensible because nothing is written
// (design doc 0042 §2.1, §3).
func TestPublicReadOnlyImpliesReadOnly(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_PUBLIC_READ_ONLY", "true")
	// Asking for writes back is not a way out.
	t.Setenv("OCHAKAI_READ_ONLY", "false")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PublicReadOnly || !cfg.ReadOnly {
		t.Errorf("public=%v read_only=%v; a publicly readable deployment must never be writable",
			cfg.PublicReadOnly, cfg.ReadOnly)
	}
}

// Both make callers anonymous, but insecure dev also lets anyone
// delegate. Refuse rather than silently pick one (design doc 0042 §2.3).
func TestPublicReadOnlyRefusesInsecureDev(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_PUBLIC_READ_ONLY", "true")
	t.Setenv("OCHAKAI_INSECURE_DEV", "true")
	if _, err := FromEnv(); err == nil {
		t.Error("accepted the public posture together with insecure dev")
	}
}

// Off by default: an existing deployment that sets neither is unchanged.
func TestPublicReadOnlyDefaultsOff(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicReadOnly || cfg.ReadOnly {
		t.Errorf("public=%v read_only=%v, want both off", cfg.PublicReadOnly, cfg.ReadOnly)
	}
}
