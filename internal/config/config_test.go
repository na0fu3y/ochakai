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
