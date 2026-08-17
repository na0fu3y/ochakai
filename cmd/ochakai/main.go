// ochakai is a context provider for data agents: one knowledge base for
// metric definitions, attested computations, interpretation knowledge,
// glossary terms, and table catalogs, served over MCP and REST.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
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
// module version Go records in the binary (`go install …@v0.9.0` →
// "v0.9.0"), falling back to "dev" for in-tree builds.
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

	// A name this release renamed still runs, for this release only
	// (design doc 0088). This sits in front of the dispatch rather than
	// behind it because the notice belongs on the way in; nothing here
	// may shadow a name that exists, which is what
	// TestARetiredNameIsNotAlsoLive checks rather than trust.
	cmd = commandAfterRenames(cmd, os.Stderr)

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
		unknownCommand(os.Stderr, cmd)
		os.Exit(1)
	}
	if err != nil {
		log.Error("ochakai failed", "command", cmd, "error", err)
		os.Exit(1)
	}
}

// adminCommands are the names the switch above dispatches on that are
// not client commands. Spelled as a list because a switch is a statement
// — nothing can read it — and two things need to: the suggestion below,
// and TestCompletionScriptsStayInSync, which had its own copy.
var adminCommands = []string{"serve", "serve-ui", "version", "help"}

// unknownCommand answers a name that is not a command. It used to print
// the whole command list — forty-odd lines for a mistyped letter, and
// the same forty for `ochakai --url … whoami`, where the list is not
// what the reader got wrong.
//
// So it says the one thing that is wrong. A leading dash is a flag put
// where a command belongs, which is a mistake worth naming: this CLI has
// no global flags (design doc 0004 §8) — --url is a flag of every client
// command, and the usage line's "--url > $OCHAKAI_URL" describes which
// wins, not where it goes. Anything else gets the nearest command, if
// one is near enough to mean it.
func unknownCommand(w io.Writer, cmd string) {
	fmt.Fprintf(w, "ochakai: unknown command %q\n", cmd)
	switch {
	case strings.HasPrefix(cmd, "-"):
		fmt.Fprintf(w, "\nA flag belongs to a command, so it goes after it:\n"+
			"  ochakai whoami %s …\n", cmd)
	default:
		if near := nearestCommand(cmd); near != "" {
			fmt.Fprintf(w, "\nDid you mean %q?\n", near)
		}
	}
	fmt.Fprintln(w, "\nRun `ochakai help` for the command list.")
}

// nearestCommand is the command a name was most likely meant to be, or
// "" when nothing is close enough. The bound scales with the name's own
// length so that a short word is not "near" everything and a long one
// can still carry a typo: `serach` reaches `search`, `frobnicate`
// reaches nothing.
func nearestCommand(cmd string) string {
	best, bound := "", len(cmd)/3+1
	names := append([]string{}, adminCommands...)
	for name := range clientCommands {
		names = append(names, name)
	}
	sort.Strings(names) // ties resolve to the first name alphabetically, not to map order
	for _, name := range names {
		if d := editDistance(cmd, name); d < bound {
			best, bound = name, d
		}
	}
	return best
}

// editDistance is Levenshtein, over bytes: every command name is ASCII,
// and so is anything close enough to one to be worth suggesting.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ochakai — context provider for data agents

Client commands (talk to a server; --url > $OCHAKAI_URL > "use" selection):
  use [name | url]        pick the server for later commands (saved locally)
  whoami                  print target server, identity, and reachability
  search <query>          search knowledge; verified concepts rank higher
  list [feed]             list a review feed or a reverse lookup, page by page
                          (usage, verified_at, failed, stale_after)
  browse [prefix]         list one level of the ID hierarchy (folder view)
  get <id>                print one concept as an OKF document (stderr notes
                          what links at it)
  put <path> [-f file]    write one object of the bundle: a concept from OKF
                          markdown or JSON at <id>, or a file at its own path
                          (every change kept as a revision)
  verify <id>             record a verification (re-affirms a verified concept too)
  reject <id>             record a rejection and why (--withdraw takes it back)
  delete <path>           remove one object: a concept by id (history retained)
                          or a file by the path it lives at
  purge <id>              hard-delete a soft-deleted concept, freeing its id
  reembed                 embed concepts missing a vector for the current model
  move <id> <new-id>      move (rename) a concept; references are rewritten
  usage <id>              show usage totals (search hits, fetches, outcomes)
  stats                   the whole loop: what is stored, what each queue holds,
                          what review did, what came back empty
  access [-f file]        show or replace the access policy: who may read and
                          write under which directory (administrators only)
  report <id> <outcome>   report an outcome: worked | failed (--note for why)
  revisions <id>          list a concept's change history (newest first)
  log [path]              print the history under a path as OKF's log.md
  export <dir | ->        download the knowledge base as an OKF bundle
  seed <file.json | ->    turn a warehouse's own schema listing into a bundle of
                          draft concepts (pipe into import; reads no warehouse)
  import <dir | tgz | ->  upload an OKF bundle (any producer's, not just ours)
  ui                      serve the web UI locally, acting as you (no deploy)
  mcp-stdio               speak MCP on stdin/stdout, forwarding to the server
                          (for clients that cannot open an HTTP MCP endpoint)

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
	st.UseLogger(log)
	// The second way to answer "who is calling" (design doc 0086). Built
	// here so a misconfigured issuer stops the deployment starting
	// rather than failing the first request that needs it.
	if cfg.OIDCIssuer != "" {
		v, err := httpauth.NewOIDC(cfg.OIDCIssuer, cfg.OIDCAudience)
		if err != nil {
			return nil, nil, err
		}
		cfg.Verifier = v
		log.Info("authenticating with OIDC", "issuer", cfg.OIDCIssuer, "audience", cfg.OIDCAudience)
	}
	embedder, err := semanticSearch(ctx, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	embedDim := 0
	if cfg.Embedding != nil {
		embedDim = cfg.Embedding.Dim
	}
	// File bytes live only on GCS (design doc 0013).
	if cfg.GCSBucket != "" {
		bs, err := blob.NewGCS(ctx, cfg.GCSBucket)
		if err != nil {
			return nil, nil, err
		}
		st.UseBlobStore(bs)
		log.Info("file bytes on GCS", "bucket", cfg.GCSBucket)
	} else {
		log.Info("files disabled (no OCHAKAI_GCS_BUCKET); markdown concepts only")
	}
	if err := st.Migrate(ctx, embedDim); err != nil {
		// A database that cannot hold vectors is not a reason to refuse
		// to serve knowledge, unless this deployment asked for semantic
		// search by name (design doc 0080 §1.3). Everything else the
		// migration does has already run — the vector schema is the last
		// step — so there is nothing to redo here.
		if cfg.Embedding == nil || !cfg.Embedding.Discovered ||
			!errors.Is(err, store.ErrEmbeddingUnavailable) {
			return nil, nil, err
		}
		log.Warn("semantic search is off: this database cannot hold vectors", "err", err)
		embedder, cfg.Embedding = nil, nil
	}
	// A deployment carrying an access policy but naming no administrator
	// is one nobody can edit the policy of, and refusing to start is the
	// only moment that can still be said out loud (design doc 0109 §3).
	// The same shape as OCHAKAI_MODE's unreadable spelling: a boundary
	// that locked its operator out would be discovered from the
	// knowledge rather than from the logs.
	if len(cfg.Admins) == 0 {
		p, err := st.AccessPolicy(ctx)
		if err != nil {
			return nil, nil, err
		}
		if p.Active {
			return nil, nil, fmt.Errorf("this database holds %d access rules and OCHAKAI_ADMINS is empty: "+
				"nobody could read or edit the policy, so set it to the principals that administer this "+
				"deployment (design doc 0109 §3)", len(p.Rules))
		}
	}
	return &service.Service{Store: st, Embedder: embedder, Config: cfg, Log: log}, cfg, nil
}

// semanticSearch builds the embedder behind hybrid search, and decides
// whether this deployment has one at all.
//
// Embeddings are the default on Google Cloud (design doc 0080 §1.1): a
// deployment that names no model gets the project *and the region* it is
// running in (design doc 0080 §1.2) and the product's own model, and whether
// it may call Vertex AI there is IAM's answer rather than a setting — so
// the answer is asked for, once, with a probe. That one probe now covers
// two ways of not having it: the identity may not call Vertex AI, or the
// region may not carry the model. A deployment that named a model asked
// for semantic search and is told when it cannot have it; a discovered
// one falls back to lexical search, which is what it would have had
// before this was the default.
func semanticSearch(ctx context.Context, cfg *config.Config, log *slog.Logger) (embed.Embedder, error) {
	discoveredProject, discoveredRegion := "", ""
	if cfg.Embedding == nil && !cfg.EmbeddingsOff {
		discoveredProject, discoveredRegion = config.DiscoverVertex(ctx)
		cfg.EnableDiscoveredEmbedding(discoveredProject, discoveredRegion)
	}
	if cfg.Embedding == nil {
		switch {
		case cfg.EmbeddingsOff:
			log.Info("semantic search off by configuration (OCHAKAI_EMBEDDINGS=off); using lexical search only")
		case discoveredProject != "" && discoveredRegion == "":
			// The project answered but the region did not. Picking one
			// would send this deployment's text to a region nobody
			// chose, which is the decision design doc 0080 §1.2 declines to
			// make for an operator.
			log.Warn("semantic search off: this deployment's region could not be read from the metadata server, and ochakai will not embed in a region nobody chose. Name one to turn it on: OCHAKAI_EMBEDDINGS=projects/<project>/locations/<region>/publishers/google/models/gemini-embedding-001",
				"project", discoveredProject)
		default:
			log.Info("semantic search disabled; using lexical search only")
		}
		return nil, nil
	}

	v, err := embed.NewVertex(ctx, cfg.Embedding.Project, cfg.Embedding.Location, cfg.Embedding.Model, cfg.Embedding.Dim)
	if err == nil && cfg.Embedding.Discovered {
		// One embedding of one word, bounded so a cold start is not held
		// up by an unreachable API. Without it a deployment whose service
		// account was never granted roles/aiplatform.user would log a
		// failure on every write forever.
		probe, cancel := context.WithTimeout(ctx, vertexProbeTimeout)
		defer cancel()
		_, err = v.Embed(probe, embed.TaskQuery, []string{"ochakai"})
	}
	if err != nil {
		if !cfg.Embedding.Discovered {
			return nil, err
		}
		log.Warn("semantic search off: Vertex AI did not answer for this deployment; using lexical search only. Grant roles/aiplatform.user to the service identity and enable aiplatform.googleapis.com to turn it on. If this region has no such model, name a region that does with OCHAKAI_EMBEDDINGS — knowing that it is where the text will go",
			"project", cfg.Embedding.Project, "location", cfg.Embedding.Location, "err", err)
		cfg.Embedding = nil
		return nil, nil
	}
	log.Info("semantic search enabled", "model", cfg.Embedding.Model, "dim", cfg.Embedding.Dim,
		"project", cfg.Embedding.Project, "location", cfg.Embedding.Location,
		"discovered", cfg.Embedding.Discovered)
	return v, nil
}

// vertexProbeTimeout bounds the one call that decides whether a
// discovered deployment has semantic search. Cloud Run's startup
// allowance is not the place to wait out an API that is not answering.
const vertexProbeTimeout = 10 * time.Second

func serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, cfg, err := setup(ctx, log)
	if err != nil {
		return err
	}
	// Runs after runServer returns, i.e. after the HTTP server has drained:
	// Close stops the usage flush loop and drains its buffer one last time,
	// so a SIGTERM does not discard the events recorded in the last few
	// seconds (design doc 0029). Ordering matters — flushing before the
	// server drains would miss the requests still in flight.
	defer svc.Store.Close()

	mux := http.NewServeMux()
	// /health, not /healthz: Google Frontends intercept /healthz on
	// run.app URLs and return their own 404 — discovered the hard way.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// The server deliberately does not serve the web UI (design doc 0006
	// §4) — but a bare 404 at / strands newcomers who just ran the compose
	// file and opened the port. Point them at the real entry points.
	//
	// Everything unmatched, not only /: the stdlib's "404 page not found"
	// is the same dead end one path over, and /healthz is the path people
	// actually try (this server answers /health — Google Frontends
	// intercept /healthz, which is why). The status still says not found;
	// only the body is worth anything.
	//
	// Registered without a method. "GET /" would be the narrower pattern
	// to want, and the stdlib refuses it: against "/mcp" neither is more
	// specific — one wins on method, the other on path — which is a
	// registration panic at startup rather than a routing surprise later.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
		}
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
	// /mcp gets the same line as /api/v1 (internal/restapi/requestlog.go).
	// Its whole surface is one address, so the route is that address; a
	// deployment whose agents talk MCP would otherwise have a request log
	// covering the half of its traffic it reads least.
	mux.Handle("/mcp", restapi.RequestLog(log, "/mcp",
		httpauth.Middleware(cfg, mcpserver.Handler(svc, version))))
	mux.Handle("/api/v1/", restapi.AnnounceReadOnly(svc, httpauth.Middleware(cfg, restapi.Handler(svc))))

	log.Info("ochakai listening", "addr", cfg.Addr, "version", version,
		"insecure_dev", cfg.InsecureDev, "endpoints", []string{"/mcp", "/api/v1", "/health"})
	return runServer(ctx, cfg.Addr, mux)
}

// drainTimeout bounds how long in-flight requests get after SIGTERM.
// Cloud Run allows 10 seconds between SIGTERM and SIGKILL, and the final
// usage flush happens after the drain (see serve) — spending the whole
// allowance here would leave the flush to be killed mid-write, losing the
// events the drained requests just recorded. Five seconds is more than
// any handler here needs; export streams longer, and a truncated backup
// is the cheapest thing in the process to lose.
const drainTimeout = 5 * time.Second

func runServer(ctx context.Context, addr string, handler http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// A request body arrives within a minute or not at all: the
		// largest accepted body is the 4 MiB PUT cap, so this asks
		// ~70 KB/s of the slowest legitimate writer while ending the
		// slow-drip connection that ReadHeaderTimeout cannot see.
		ReadTimeout: time.Minute,
		// Idle keep-alive connections are the other way to hoard a
		// listener without ever tripping a per-request deadline.
		IdleTimeout: 2 * time.Minute,
		// WriteTimeout stays unset, deliberately: the MCP endpoint is a
		// streamable-HTTP handler whose GET is a hanging stream by
		// design, and a write deadline would sever it — and sever an
		// export to a slow reader with it. Where response duration
		// needs a bound, Cloud Run's own request timeout is that bound.
	}
	return serveAndDrain(ctx, server, ln)
}

// serveAndDrain serves ln until ctx is cancelled, then returns only once
// the drain has finished. Serve returns ErrServerClosed the moment
// Shutdown is *called*, not when the drain completes — returning on it
// alone would run the caller's deferred Store.Close (final usage flush,
// pool close) while requests are still in flight, failing them on a
// closed pool and losing the usage events they just recorded.
func serveAndDrain(ctx context.Context, server *http.Server, ln net.Listener) error {
	errc := make(chan error, 1)
	go func() { errc <- server.Serve(ln) }()
	select {
	case err := <-errc:
		return err // stopped before any shutdown was asked for
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	// Best-effort: past drainTimeout the stragglers are cut off and the
	// process exits; nothing useful to do with the error.
	_ = server.Shutdown(shutdownCtx)
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
