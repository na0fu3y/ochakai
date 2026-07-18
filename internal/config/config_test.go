package config

import (
	"strings"
	"testing"
)

func TestConnectorConfig(t *testing.T) {
	setValid := func(t *testing.T) {
		t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")
		t.Setenv("OCHAKAI_CONNECTOR_PUBLIC_URL", "https://connector.example/")
		t.Setenv("OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID", "cid")
		t.Setenv("OCHAKAI_CONNECTOR_GOOGLE_CLIENT_SECRET", "sec")
		t.Setenv("OCHAKAI_CONNECTOR_ALLOWED_DOMAIN", "example.co.jp")
	}

	t.Run("valid", func(t *testing.T) {
		setValid(t)
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Connector == nil {
			t.Fatal("Connector not enabled")
		}
		if cfg.Connector.PublicURL != "https://connector.example" {
			t.Errorf("PublicURL = %q, want trailing slash trimmed", cfg.Connector.PublicURL)
		}
	})

	t.Run("missing google client", func(t *testing.T) {
		setValid(t)
		t.Setenv("OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID", "")
		if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("http public URL", func(t *testing.T) {
		setValid(t)
		t.Setenv("OCHAKAI_CONNECTOR_PUBLIC_URL", "http://connector.example")
		if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("http loopback tolerated", func(t *testing.T) {
		setValid(t)
		t.Setenv("OCHAKAI_CONNECTOR_PUBLIC_URL", "http://localhost:8787")
		if _, err := FromEnv(); err != nil {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("exclusive with insecure dev", func(t *testing.T) {
		setValid(t)
		t.Setenv("OCHAKAI_INSECURE_DEV", "true")
		if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("absent means disabled", func(t *testing.T) {
		t.Setenv("OCHAKAI_DATABASE_URL", "postgres://x")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Connector != nil {
			t.Error("Connector enabled without OCHAKAI_CONNECTOR_PUBLIC_URL")
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
