package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUseSavesSwitchesAndValidates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OCHAKAI_URL", "")
	ctx := context.Background()

	if err := cmdUse(ctx, []string{"http://localhost:8080", "--name", "local"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdUse(ctx, []string{"https://ochakai-prod.run.app/"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Current != "ochakai-prod.run.app" {
		t.Errorf("current = %q, want the URL's host", cfg.Current)
	}
	if cfg.Instances["ochakai-prod.run.app"] != "https://ochakai-prod.run.app" {
		t.Errorf("trailing slash kept: %q", cfg.Instances["ochakai-prod.run.app"])
	}
	if cfg.Instances["local"] != "http://localhost:8080" {
		t.Errorf("named instance lost: %+v", cfg.Instances)
	}

	if err := cmdUse(ctx, []string{"local"}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = loadCLIConfig(); cfg.Current != "local" {
		t.Errorf("switch by name: current = %q", cfg.Current)
	}

	if err := cmdUse(ctx, []string{"nope"}); err == nil {
		t.Error("unknown name accepted")
	}
	if err := cmdUse(ctx, []string{"ftp://example.com"}); err == nil {
		t.Error("non-http scheme accepted")
	}
}

func TestConfigPathPerOS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", `C:\Users\na0\AppData\Roaming`)

	p, err := configPathFor("windows")
	if err != nil || !strings.HasPrefix(p, `C:\Users\na0\AppData\Roaming`) {
		t.Errorf("windows path = %q, %v; want under %%AppData%%", p, err)
	}

	p, err = configPathFor("darwin")
	if err != nil || !strings.HasSuffix(p, filepath.Join(".config", "ochakai", "config.json")) {
		t.Errorf("darwin path = %q, %v; want ~/.config/ochakai/config.json", p, err)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join("/", "tmp", "xdg"))
	for _, goos := range []string{"windows", "darwin", "linux"} {
		p, err := configPathFor(goos)
		want := filepath.Join("/", "tmp", "xdg", "ochakai", "config.json")
		if err != nil || p != want {
			t.Errorf("explicit XDG on %s = %q, %v; want %q", goos, p, err, want)
		}
	}
}

func TestDefaultURLPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OCHAKAI_URL", "")
	if got := defaultURL(); got != "" {
		t.Errorf("empty state: %q", got)
	}
	if err := saveCLIConfig(&cliConfig{Current: "prod", Instances: map[string]string{"prod": "https://prod.example"}}); err != nil {
		t.Fatal(err)
	}
	if got := defaultURL(); got != "https://prod.example" {
		t.Errorf("config selection ignored: %q", got)
	}
	t.Setenv("OCHAKAI_URL", "http://env.example")
	if got := defaultURL(); got != "http://env.example" {
		t.Errorf("$OCHAKAI_URL should win over config: %q", got)
	}
}

func TestWhoamiAgainstPlainHTTPServer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OCHAKAI_URL", "")
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer healthy.Close()
	if err := cmdWhoami(context.Background(), []string{"--url", healthy.URL}); err != nil {
		t.Errorf("healthy server: %v", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	if err := cmdWhoami(context.Background(), []string{"--url", broken.URL}); err == nil {
		t.Error("unhealthy server reported as ok")
	}
}
