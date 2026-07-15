// ochakai is a context provider for data agents: one knowledge base for
// metric definitions, verified golden queries, interpretation knowledge,
// glossary terms, and table catalogs, served over MCP and REST.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/importer"
	"github.com/na0fu3y/ochakai/internal/mcpserver"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/restapi"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"

	"github.com/na0fu3y/ochakai/examples/webui"
)

// version is stamped by -ldflags at release; "dev" otherwise.
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = serve(log)
	case "import-ossie":
		if len(os.Args) < 3 {
			err = fmt.Errorf("usage: ochakai import-ossie <semantic-model.yaml>")
		} else {
			err = importOssie(log, os.Args[2])
		}
	case "export-okf":
		if len(os.Args) < 3 {
			err = fmt.Errorf("usage: ochakai export-okf <output-dir>")
		} else {
			err = exportOKF(log, os.Args[2])
		}
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q (commands: serve, import-ossie, export-okf, version)", cmd)
	}
	if err != nil {
		log.Error("ochakai failed", "command", cmd, "error", err)
		os.Exit(1)
	}
}

func setup(ctx context.Context, log *slog.Logger) (*service.Service, *config.Config, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, nil, err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	embedDim := 0
	var embedder embed.Embedder
	if cfg.Embedding != nil {
		embedDim = cfg.Embedding.Dim
		v, err := embed.NewVertex(ctx, cfg.Embedding.Project, cfg.Embedding.Location, cfg.Embedding.Model, cfg.Embedding.Dim)
		if err != nil {
			return nil, nil, err
		}
		embedder = v
		log.Info("semantic search enabled", "provider", cfg.Embedding.Provider, "model", cfg.Embedding.Model, "dim", cfg.Embedding.Dim)
	} else {
		log.Info("semantic search disabled; using trigram search only")
	}
	if err := st.Migrate(ctx, embedDim); err != nil {
		return nil, nil, err
	}
	return &service.Service{Store: st, Embedder: embedder, Config: cfg, Log: log}, cfg, nil
}

func serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, cfg, err := setup(ctx, log)
	if err != nil {
		return err
	}
	defer svc.Store.Close()

	mux := http.NewServeMux()
	// /health is the canonical health endpoint. /healthz is kept for local
	// use but is unreachable behind Google Frontends (Cloud Run's run.app
	// intercepts the path and returns its own 404) — discovered the hard way.
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /healthz", health)
	// Sample web UI (examples/webui), served same-origin so its REST calls
	// need no CORS. The page is static; the API it calls stays token-authed.
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webui.Index)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui", http.StatusFound)
	})
	mux.Handle("/mcp", httpauth.Middleware(cfg, mcpserver.Handler(svc, version)))
	mux.Handle("/api/v1/", httpauth.Middleware(cfg, restapi.Handler(svc)))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Info("ochakai listening", "addr", cfg.Addr, "version", version,
		"auth", len(cfg.Clients) > 0, "endpoints", []string{"/mcp", "/api/v1", "/healthz"})
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func importOssie(log *slog.Logger, path string) error {
	ctx := context.Background()
	svc, _, err := setup(ctx, log)
	if err != nil {
		return err
	}
	defer svc.Store.Close()

	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	report, err := importer.ImportOssie(ctx, svc, src, cliActor())
	if err != nil {
		return err
	}
	log.Info("import complete", "models", report.Models,
		"created", len(report.Created), "updated", len(report.Updated))
	for _, uri := range report.Created {
		fmt.Println("created", uri)
	}
	for _, uri := range report.Updated {
		fmt.Println("updated", uri)
	}
	return nil
}

// exportOKF writes the knowledge base as an OKF bundle directory,
// ready to commit to git or ship as an archive.
func exportOKF(log *slog.Logger, dir string) error {
	ctx := context.Background()
	svc, _, err := setup(ctx, log)
	if err != nil {
		return err
	}
	defer svc.Store.Close()

	entries, err := svc.Store.ListAll(ctx)
	if err != nil {
		return err
	}
	files, err := okf.Bundle(entries)
	if err != nil {
		return err
	}
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}
	log.Info("export complete", "dir", dir, "concepts", len(entries), "files", len(files))
	return nil
}

func cliActor() domain.Actor {
	name := "cli"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return domain.Actor{Kind: domain.ActorHuman, Name: name}
}
