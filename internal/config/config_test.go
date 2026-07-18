package config

import (
	"strings"
	"testing"
)

// The connector service is removed (design doc 0012); a deployment still
// configured for it was publicly invokable and must not silently start
// as the private trust-the-headers surface.
func TestRemovedConnectorRefusesToStart(t *testing.T) {
	t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")

	t.Run("guard", func(t *testing.T) {
		t.Setenv("OCHAKAI_CONNECTOR_PUBLIC_URL", "https://connector.example")
		if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "OCHAKAI_CONNECTOR_PUBLIC_URL") {
			t.Errorf("err = %v, want refuse-to-start guard", err)
		}
	})

	t.Run("absent is fine", func(t *testing.T) {
		if _, err := FromEnv(); err != nil {
			t.Fatal(err)
		}
	})
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
