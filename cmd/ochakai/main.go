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
	"runtime/debug"
	"syscall"
	"time"

	"github.com/na0fu3y/ochakai/internal/blob"
	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/mcpserver"
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
	case "serve-ui":
		err = serveUI(log)
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

Client commands (talk to a server; --url > $OCHAKAI_URL > "use" selection):
  use [name | url]        pick the server for later commands (saved locally)
  whoami                  print target server, identity, and reachability
  search [query]          search knowledge; verified entries rank higher
  browse [type[/prefix]]  list one level of the ID hierarchy (folder view)
  context <question>      the one-call read before a data question (full entries)
  get <type>/<id>         print one entry as an OKF document
  create [-f file]        create an entry from OKF markdown or JSON
  update <type>/<id>      replace an entry (every change kept as a revision)
  delete <type>/<id>      soft-delete an entry (history retained)
  attach <type>/<id> <f>  attach files to an entry (png/jpeg/webp/pdf/text)
  detach <type>/<id> <n>  remove an attachment
  usage <type>/<id>       show usage totals (search hits, fetches, compiles, outcomes)
  report <type>/<id> <o>  report an outcome: worked | failed (--note for why)
  revisions <type>/<id>   list an entry's change history (newest first)
  backlinks <type>/<id>   list entries whose links point at this one
  compile --metric <m>    compile metrics into SQL (exit 2 = outside subset)
  export <dir | ->        download the knowledge base as an OKF bundle
  import <dir | tgz | ->  upload an OKF bundle (any producer's, not just ours)
  import-ossie <file>     import an Apache Ossie semantic model
  ui                      serve the web UI locally, acting as you (no deploy)

Server commands (run as deployed services, configured by environment):
  serve                   start the MCP + REST server (runs next to the database)
  serve-ui                serve the team web UI, proxying to $OCHAKAI_URL as the
                          service identity (same image as serve: --args=serve-ui)

  version                 print the version
  completion <shell>      print a completion script (zsh, bash, fish)

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
	// Attachment bytes live only on GCS (design doc 0013). The blob store
	// is wired and the legacy bytea backfill runs BEFORE schema migrations:
	// migration 0009 drops the bytea column and refuses to run while
	// inline bytes remain, so this order lets one boot with the bucket set
	// finish the move. Failing the start on a backfill error is deliberate
	// — the backfill is idempotent and the next boot resumes it.
	if cfg.GCSBucket != "" {
		bs, err := blob.NewGCS(ctx, cfg.GCSBucket)
		if err != nil {
			return nil, nil, err
		}
		st.UseBlobStore(bs)
		moved, err := st.MigrateBlobsOut(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("migrating attachment blobs to gs://%s: %w", cfg.GCSBucket, err)
		}
		if moved > 0 {
			log.Info("migrated inline attachment blobs to GCS", "count", moved, "bucket", cfg.GCSBucket)
		}
		log.Info("attachment bytes on GCS", "bucket", cfg.GCSBucket)
	} else {
		log.Info("attachments disabled (no OCHAKAI_GCS_BUCKET); markdown entries only")
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
	// The server deliberately does not serve the web UI (design doc 0006
	// §4) — but a bare 404 at / strands newcomers who just ran the compose
	// file and opened the port. Point them at the real entry points.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, `ochakai %s — context provider for data agents

This is the API server; it has no pages. Talk to it via:

  REST     /api/v1        (spec: api/openapi.yaml in the repo)
  MCP      /mcp
  health   /health

For the web UI, run the bundled proxy on your machine:

  ochakai ui --url <this server's URL>

then open http://127.0.0.1:8098. See also: ochakai --help
`, version)
	})
	mux.Handle("/mcp", httpauth.Middleware(cfg, mcpserver.Handler(svc, version)))
	mux.Handle("/api/v1/", httpauth.Middleware(cfg, restapi.Handler(svc)))

	log.Info("ochakai listening", "addr", cfg.Addr, "version", version,
		"insecure_dev", cfg.InsecureDev, "endpoints", []string{"/mcp", "/api/v1", "/health"})
	return runServer(ctx, cfg.Addr, mux)
}

func runServer(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
