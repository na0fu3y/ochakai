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
func TestPublicReadOnlyKeepsNoQueries(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x/y")
	t.Setenv("OCHAKAI_PUBLIC_READ_ONLY", "true")
	t.Setenv("OCHAKAI_RECORD_MISSES", "true")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecordMisses {
		t.Error("a public deployment is keeping what its callers typed")
	}
}
