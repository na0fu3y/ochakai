// ochakai is a context provider for data agents: one knowledge base for
// metric definitions, verified golden queries, interpretation knowledge,
// glossary terms, and table catalogs, served over MCP and REST.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime/debug"
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
)

// version is stamped by -ldflags at release; otherwise it comes from the
// module version Go records in the binary (`go install …@v0.4.0` →
// "v0.4.0"), falling back to "dev" for in-tree builds.
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)
	version = resolveVersion()

	// No default command: a bare `ochakai` is almost always a CLI user
	// exploring, not a server operator (the container image passes
	// `serve` explicitly via CMD).
	if len(os.Args) < 2 {
		usage(os.Stdout)
		return
	}
	cmd := os.Args[1]

	if _, ok := clientCommands[cmd]; ok {
		os.Exit(runClient(cmd, os.Args[2:]))
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
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "ochakai: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(1)
	}
	if err != nil {
		log.Error("ochakai failed", "command", cmd, "error", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ochakai — context provider for data agents

Client commands (talk to a server; --url or $OCHAKAI_URL):
  search [query]          search knowledge; verified entries rank higher
  get <type>/<id>         print one entry as an OKF document
  create [-f file]        create an entry from OKF markdown or JSON
  update <type>/<id>      replace an entry (every change kept as a revision)
  delete <type>/<id>      soft-delete an entry (history retained)
  compile --metric <m>    compile metrics into SQL (exit 2 = outside subset)
  export <dir | ->        download the knowledge base as an OKF bundle

Server admin commands (run next to the database):
  serve                   start the MCP + REST server
  import-ossie <file>     import an Apache Ossie semantic model
  export-okf <dir>        write the OKF bundle straight from the database

  version                 print the version

Run "ochakai <command> -h" for flags and examples.
`)
}

func setup(ctx context.Context, log *slog.Logger) (*service.Service, *config.Config, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, nil, err
	}
	st, err := store.New(ctx, cfg.DatabaseURL, cfg.DBIAMAuth)
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
		log.Info("semantic search enabled", "model", cfg.Embedding.Model, "dim", cfg.Embedding.Dim)
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
		"insecure_dev", cfg.InsecureDev, "endpoints", []string{"/mcp", "/api/v1", "/health"})
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
