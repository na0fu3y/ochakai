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
// OCHAKAI_EMBEDDINGS=off is (design doc 0053 §2.4). A deployment that
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
// (design doc 0042 §2.1, §3). Since 0060 the implication is the only
// thing left to check — there is no second variable that could ask for
// writes back.
func TestPublicModeImpliesReadOnly(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_MODE", "public")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PublicReadOnly || !cfg.ReadOnly {
		t.Errorf("public=%v read_only=%v; a publicly readable deployment must never be writable",
			cfg.PublicReadOnly, cfg.ReadOnly)
	}
}

// One word, one posture. The combination 0042 §2.3 had to refuse —
// public together with insecure dev, where a stranger may name any
// person they like — is now unspellable rather than rejected, which is
// the whole of design doc 0060 in one test.
func TestModesAreExclusive(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	for _, c := range []struct {
		mode                          string
		readOnly, public, insecureDev bool
	}{
		{"", false, false, false},
		{"read-only", true, false, false},
		{"public", true, true, false},
		{"dev", false, false, true},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("OCHAKAI_MODE", c.mode)
			cfg, err := FromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ReadOnly != c.readOnly || cfg.PublicReadOnly != c.public || cfg.InsecureDev != c.insecureDev {
				t.Errorf("mode %q: read_only=%v public=%v dev=%v, want %v/%v/%v",
					c.mode, cfg.ReadOnly, cfg.PublicReadOnly, cfg.InsecureDev,
					c.readOnly, c.public, c.insecureDev)
			}
		})
	}
}

// A misspelled posture is refused, not read as the default. A deployment
// that meant read-only and got a writable one would find out from its
// knowledge rather than from its logs (design doc 0060 §2.3) — the same
// reasoning OCHAKAI_EMBEDDINGS already used for "false".
func TestUnknownModeRefused(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	for _, mode := range []string{"readonly", "read_only", "true", "public-read-only", "READ-ONLY"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("OCHAKAI_MODE", mode)
			if _, err := FromEnv(); err == nil {
				t.Errorf("OCHAKAI_MODE=%q was accepted; a posture nobody can spell must not be silently the default", mode)
			}
		})
	}
}

// Off by default: a deployment that names no mode is unchanged.
func TestModeDefaultsToTheOrdinaryPosture(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicReadOnly || cfg.ReadOnly || cfg.InsecureDev {
		t.Errorf("public=%v read_only=%v dev=%v, want the ordinary posture",
			cfg.PublicReadOnly, cfg.ReadOnly, cfg.InsecureDev)
	}
}

// Misses are recorded unless the operator says otherwise: a measurement
// nobody switches on is a measurement nobody has (design doc 0051 §3.4).
// It is the only default-on boolean here, so it is the only one that
// reads anything but "true" as on.
func TestRecordMissesDefaultsOn(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RecordMisses {
		t.Error("misses are not recorded by default")
	}
	t.Setenv("OCHAKAI_RECORD_MISSES", "false")
	if cfg, err = FromEnv(); err != nil {
		t.Fatal(err)
	} else if cfg.RecordMisses {
		t.Error("OCHAKAI_RECORD_MISSES=false did not turn recording off")
	}
}

// A public deployment reads no identity, so it keeps no query text
// either — and asking for it back is not a way out, exactly as with
// read-only (design doc 0051 §3.4).
func TestPublicModeKeepsNoQueries(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_MODE", "public")
	t.Setenv("OCHAKAI_RECORD_MISSES", "true")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecordMisses {
		t.Error("a public deployment is keeping what its callers typed")
	}
}
