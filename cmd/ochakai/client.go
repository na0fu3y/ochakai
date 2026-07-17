// Client-side subcommands (design docs 0004/0007): pure clients of a
// remote ochakai's /api/v1, named by --url or $OCHAKAI_URL. The only
// command that talks to the database directly is `serve` (main.go).
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/apiclient"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
)

var clientCommands = map[string]func(context.Context, []string) error{
	"search":  cmdSearch,
	"context": cmdContext,
	"get":     cmdGet,
	"create":  cmdCreate,
	"update":  cmdUpdate,
	"delete":  cmdDelete,
	"usage":   cmdUsage,
	"compile": cmdCompile,
	"export":  cmdExport,
	"import":  cmdImport,
	"use":     cmdUse,
	"whoami":  cmdWhoami,
	"ui":      cmdUI,

	"import-ossie": cmdImportOssie,

	"completion": cmdCompletion,
}

// runClient dispatches a client command and maps errors to exit codes:
// 0 success, 1 error, 2 = the server understood and refused the request
// (compile outside the supported subset; see stderr for the reason).
func runClient(name string, args []string) int {
	err := clientCommands[name](context.Background(), args)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errReported):
		return 1
	}
	fmt.Fprintf(os.Stderr, "ochakai %s: %v\n", name, err)
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity {
		return 2
	}
	return 1
}

// errReported means the FlagSet already printed the problem.
var errReported = errors.New("usage error")

func newBareFlagSet(synopsis, examples string) *flag.FlagSet {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "%s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
		if examples != "" {
			fmt.Fprintf(fs.Output(), "\nExamples:\n%s", examples)
		}
	}
	return fs
}

func newFlagSet(synopsis, examples string) (*flag.FlagSet, *string) {
	fs := newBareFlagSet(synopsis, examples)
	url := fs.String("url", defaultURL(), "ochakai server URL (default: $OCHAKAI_URL, else the `ochakai use` selection)")
	return fs, url
}

// parseArgs parses flags and returns positional arguments, allowing flags
// after positionals (stdlib flag stops at the first non-flag).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, flag.ErrHelp
			}
			return nil, errReported
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func newClient(ctx context.Context, url string) (*apiclient.Client, error) {
	if url == "" {
		return nil, errors.New("server URL required: run `ochakai use <url>`, set OCHAKAI_URL, or pass --url")
	}
	return apiclient.New(ctx, url)
}

// repeated collects a repeatable string flag.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// typeList and statusList render the domain enumerations for help text,
// so flag help can never drift from the model again.
func typeList() string {
	ss := make([]string, len(domain.Types))
	for i, t := range domain.Types {
		ss[i] = string(t)
	}
	return strings.Join(ss, "|")
}

func statusList() string {
	ss := make([]string, len(domain.Statuses))
	for i, s := range domain.Statuses {
		ss[i] = string(s)
	}
	return strings.Join(ss, "|")
}

// splitRef parses "<type>/<id>" (an "ochakai://" prefix is tolerated).
func splitRef(s string) (string, string, error) {
	typ, id, ok := strings.Cut(strings.TrimPrefix(s, "ochakai://"), "/")
	if !ok || typ == "" || id == "" {
		return "", "", fmt.Errorf("want <type>/<id> (e.g. metric/revenue), got %q", s)
	}
	return typ, id, nil
}

func cmdSearch(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai search [flags] [query]\n\nSearch the knowledge base; verified entries rank higher.\nOutput: score, uri, status, title — description (one hit per line).\nWith --sort verified_at the command lists by verification age instead\nof searching (oldest first, never-verified last — the golden-query\ncanary feed); output is then verified_at, uri, status, title.",
		"  ochakai search \"gross margin\" --type metric --type term --status verified\n  ochakai search churn --json | jq '.hits[0].attrs'\n  ochakai search --sort verified_at --type query --status verified --limit 100\n")
	var types, statuses, tags repeated
	fs.Var(&types, "type", "filter by type: "+typeList()+", or any custom type (repeatable)")
	fs.Var(&statuses, "status", "filter by status: "+statusList()+" (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	sortBy := fs.String("sort", "", `list instead of search: "verified_at" = by verification age, oldest first`)
	limit := fs.Int("limit", 0, "max results (server default 10, max 50; with --sort: 100, max 1000)")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *sortBy != "" && len(pos) > 0 {
		return fmt.Errorf("--sort lists entries by verification age; it cannot be combined with a search query")
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	hits, err := c.Search(ctx, apiclient.SearchParams{
		Query: strings.Join(pos, " "), Types: types, Statuses: statuses, Tags: tags,
		Sort: *sortBy, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(map[string]any{"hits": hits})
	}
	for _, h := range hits {
		lead := fmt.Sprintf("%.3f", h.Score)
		if *sortBy != "" {
			lead = "-" // never verified sorts last
			if h.VerifiedAt != nil {
				lead = h.VerifiedAt.Format(time.RFC3339)
			}
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%s", lead, h.URI(), h.Status, h.Title)
		if h.Description != "" {
			line += " — " + h.Description
		}
		fmt.Println(line)
	}
	return nil
}

func cmdContext(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai context [flags] <question>\n\nGather what to read before answering a data question, in one call:\nthe full entries behind the top search hits (verified entries rank\nhigher), expanded one hop through links so the insight explaining a\nmetric travels with it. Markdown on stdout, ready for an agent's\ncontext window. No hits print nothing (exit 0).",
		"  ochakai context \"why did revenue drop in March?\"\n  ochakai context \"monthly revenue\" --type query --status verified --json\n  ochakai context \"$PROMPT\" --budget 4000   # hooks: cap the injected bytes\n")
	var types, statuses, tags repeated
	fs.Var(&types, "type", "filter by type: "+typeList()+", or any custom type (repeatable)")
	fs.Var(&statuses, "status", "filter by status: "+statusList()+" (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	limit := fs.Int("limit", 0, "max full entries (server default 5, max 20)")
	budget := fs.Int("budget", 0, "stop rendering entries after ~this many bytes (0 = no cap)")
	minScore := fs.Float64("min-score", 0, "drop hits scoring below this; scores depend on the server's search mode (trigram vs hybrid), so calibrate before use (0 = off)")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		fs.Usage()
		return errReported
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	res, err := c.Context(ctx, strings.Join(pos, " "), types, statuses, tags, *limit, *minScore)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(res)
	}
	renderContext(os.Stdout, res, *budget)
	return nil
}

// renderContext prints the pack as compact markdown: per entry a heading
// with URI, status, and title, a provenance line, the golden-query
// question and SQL when present, then the body. Once the byte budget is
// spent the remaining entries are dropped with a note (the first entry
// always renders); hits without a rendered entry become one-line pointers.
func renderContext(w io.Writer, res *apiclient.ContextResult, budget int) {
	rendered := map[string]bool{}
	written, omitted := 0, 0
	for i := range res.Entries {
		k := &res.Entries[i]
		sec := renderEntry(k)
		if budget > 0 && written > 0 && written+len(sec) > budget {
			omitted = len(res.Entries) - i
			break
		}
		fmt.Fprint(w, sec)
		written += len(sec)
		rendered[string(k.Type)+"/"+k.ID] = true
	}
	if omitted > 0 {
		fmt.Fprintf(w, "(%d more entries beyond --budget; raise it or `ochakai get` them)\n", omitted)
	}
	var rest []string
	for _, h := range res.Hits {
		if !rendered[string(h.Type)+"/"+h.ID] {
			rest = append(rest, fmt.Sprintf("- %s (%s) — %s", h.URI(), h.Status, h.Title))
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(w, "\nAlso relevant (`ochakai get <type>/<id>` for the full entry):\n%s\n", strings.Join(rest, "\n"))
	}
}

func renderEntry(k *domain.Knowledge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (%s) — %s\n", k.URI(), k.Status, k.Title)
	prov := fmt.Sprintf("created by %s:%s", k.CreatedBy.Kind, k.CreatedBy.Name)
	if k.VerifiedBy != nil && k.VerifiedAt != nil {
		prov = fmt.Sprintf("verified by %s:%s on %s; %s",
			k.VerifiedBy.Kind, k.VerifiedBy.Name, k.VerifiedAt.Format("2006-01-02"), prov)
	}
	fmt.Fprintln(&b, prov)
	if k.StatusNote != "" {
		fmt.Fprintf(&b, "status note: %s\n", k.StatusNote)
	}
	if k.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", k.Description)
	}
	if q, ok := k.Attrs["question"].(string); ok && q != "" {
		fmt.Fprintf(&b, "\nQ: %s\n", q)
	}
	if sql, ok := k.Attrs["sql"].(string); ok && sql != "" {
		fmt.Fprintf(&b, "\n```sql\n%s\n```\n", strings.TrimRight(sql, "\n"))
	}
	if body := strings.TrimSpace(k.Body); body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	}
	b.WriteString("\n")
	return b.String()
}

func cmdUsage(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai usage [flags] <type>/<id>\n\nShow how often an entry was actually used: appeared in search results,\nfetched individually, referenced by compile — and when it was last\nused. The measure of the write-back loop: evidence for promoting a\ndraft, and a staleness signal for verified entries nobody uses.",
		"  ochakai usage query/monthly-revenue\n  ochakai usage metric/revenue --json\n")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	typ, id, err := splitRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	u, err := c.Usage(ctx, typ, id)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(u)
	}
	last := "-"
	if u.LastUsedAt != nil {
		last = u.LastUsedAt.Format(time.RFC3339)
	}
	fmt.Printf("search_hits\t%d\nfetches\t%d\ncompiles\t%d\nlast_used_at\t%s\n",
		u.SearchHits, u.Fetches, u.Compiles, last)
	return nil
}

func cmdGet(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai get [flags] <type>/<id>\n\nPrint one knowledge entry as an OKF document (YAML frontmatter +\nmarkdown body). The output round-trips through `ochakai update`.",
		"  ochakai get metric/revenue\n  ochakai get query/monthly-revenue --json | jq -r '.attrs.sql'\n")
	asJSON := fs.Bool("json", false, "print JSON instead of the OKF document")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	typ, id, err := splitRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	k, err := c.Get(ctx, typ, id)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(k)
	}
	doc, err := okf.Document(k)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(doc)
	return err
}

func cmdCreate(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai create [flags]\n\nCreate a knowledge entry from -f or stdin. Input is an OKF document\n(--- frontmatter with type/id/title, markdown body — the format\n`ochakai get` prints) or JSON (see api/openapi.yaml). Entries default\nto draft; provenance is recorded from your Google identity.",
		"  ochakai get insight/revenue-seasonality | sed s/40%/45%/ | ochakai create\n  ochakai create -f entry.md\n")
	file := fs.String("f", "", "input file (default: stdin)")
	asJSON := fs.Bool("json", false, "print the created entry as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		fs.Usage()
		return errReported
	}
	k, err := readEntry(*file)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	created, err := c.Create(ctx, k)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(created)
	}
	fmt.Printf("created %s (%s)\n", created.URI(), created.Status)
	return nil
}

func cmdUpdate(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai update [flags] <type>/<id>\n\nReplace a knowledge entry from -f or stdin (OKF document or JSON;\ntype and id come from the argument). Every change is kept as a\nrevision server-side.",
		"  ochakai get metric/revenue | $EDITOR /dev/stdin | ochakai update metric/revenue\n  ochakai update metric/revenue -f revenue.md\n")
	file := fs.String("f", "", "input file (default: stdin)")
	asJSON := fs.Bool("json", false, "print the updated entry as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	typ, id, err := splitRef(pos[0])
	if err != nil {
		return err
	}
	k, err := readEntry(*file)
	if err != nil {
		return err
	}
	k.Type, k.ID = domain.Type(typ), id
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	updated, err := c.Update(ctx, k)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(updated)
	}
	fmt.Printf("updated %s (%s)\n", updated.URI(), updated.Status)
	return nil
}

func cmdDelete(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai delete [flags] <type>/<id>\n\nSoft-delete a knowledge entry (history is retained server-side).",
		"  ochakai delete term/obsolete-kpi\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	typ, id, err := splitRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	if err := c.Delete(ctx, typ, id); err != nil {
		return err
	}
	fmt.Printf("deleted %s/%s\n", typ, id)
	return nil
}

func cmdCompile(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai compile [flags] --metric <name>\n\nDeterministically compile metrics into SQL (never executed, never\nguessed). SQL goes to stdout; notes and related verified golden\nqueries go to stderr — prefer a verified query when it answers the\nquestion. Exit 2 means the request is outside the supported subset;\nthe reason is on stderr.",
		"  ochakai compile --metric revenue --dimension orders.region --grain orders.created_at:month\n  ochakai compile --metric revenue --filter \"orders.status = shipped\" > revenue.sql\n")
	var metrics, dims, filters repeated
	fs.Var(&metrics, "metric", "metric name (repeatable, required)")
	fs.Var(&dims, "dimension", "group-by column as dataset.field (repeatable)")
	fs.Var(&filters, "filter", `filter as "dataset.field op value"; op: = != > >= < <= in not_in (in/not_in take comma-separated values) (repeatable)`)
	grain := fs.String("grain", "", "time grain as dataset.field:day|week|month|quarter|year")
	model := fs.String("model", "", "semantic model name (default: resolved from the first metric)")
	dialect := fs.String("dialect", "", "SQL dialect: bigquery (default) | ansi")
	limit := fs.Int("limit", 0, "LIMIT clause")
	asJSON := fs.Bool("json", false, "print the full JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || len(metrics) == 0 {
		fs.Usage()
		return errReported
	}
	req := apiclient.CompileRequest{
		Model:      *model,
		Metrics:    metrics,
		Dimensions: dims,
		Dialect:    *dialect,
		Limit:      *limit,
	}
	for _, f := range filters {
		pf, err := parseFilter(f)
		if err != nil {
			return err
		}
		req.Filters = append(req.Filters, pf)
	}
	if *grain != "" {
		tg, err := parseGrain(*grain)
		if err != nil {
			return err
		}
		req.TimeGrain = &tg
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	res, err := c.Compile(ctx, req)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(res)
	}
	for _, n := range res.Notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}
	for _, q := range res.VerifiedQueries {
		label := q.Title
		if question, ok := q.Attrs["question"].(string); ok && question != "" {
			label = question
		}
		fmt.Fprintf(os.Stderr, "verified query: %s — %s\n", q.URI(), label)
	}
	fmt.Println(strings.TrimRight(res.SQL, "\n"))
	return nil
}

func cmdExport(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai export [flags] <dir | ->\n\nDownload the whole knowledge base as an OKF bundle (markdown + YAML\nfrontmatter) into dir, or stream the tar.gz to stdout with \"-\".\nYour knowledge is yours.",
		"  ochakai export ./knowledge\n  ochakai export - > ochakai-okf.tar.gz\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	rc, err := c.Export(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()
	if pos[0] == "-" {
		_, err := io.Copy(os.Stdout, rc)
		return err
	}
	n, err := extractTarGz(pos[0], rc)
	if err != nil {
		return err
	}
	fmt.Printf("exported %d files to %s\n", n, pos[0])
	return nil
}

func cmdImport(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai import [flags] <dir | file.tar.gz | ->\n\nImport an OKF bundle (a directory of markdown + YAML frontmatter, or\na tar.gz of one; \"-\" reads the tar.gz from stdin). The inverse of\n`ochakai export`: paths name the entries (first segment = type, rest =\nid), reserved index.md / log.md files are skipped, unknown frontmatter\nkeys are kept as attrs, existing entries are replaced (kept as\nrevisions). An archive wrapped in a single directory is unwrapped.\nWorks with any OKF bundle, not just ochakai's own.",
		"  ochakai import ./knowledge\n  ochakai import ga4-bundle.tar.gz --dry-run\n  ochakai export - | OCHAKAI_URL=https://other ochakai import -\n")
	dryRun := fs.Bool("dry-run", false, "parse and list what would be written, write nothing")
	keepRoot := fs.Bool("keep-root", false, "keep a single top-level directory as the type instead of unwrapping it")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	files, err := readBundle(pos[0])
	if err != nil {
		return err
	}
	if !*keepRoot {
		if unwrapped, root := okf.StripWrapper(files); root != "" {
			fmt.Fprintf(os.Stderr, "note: unwrapped bundle directory %q (pass --keep-root to treat it as the type)\n", root)
			files = unwrapped
		}
	}
	entries, skipped := okf.FromBundle(files)
	for _, s := range skipped {
		fmt.Fprintln(os.Stderr, "skip:", s)
	}
	if *dryRun {
		for i := range entries {
			fmt.Printf("would import %s\n", entries[i].URI())
		}
		fmt.Printf("dry run: %d entries, %d skipped\n", len(entries), len(skipped))
		return nil
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	var created, updated int
	for i := range entries {
		k := &entries[i]
		if _, err := c.Create(ctx, k); err == nil {
			created++
			fmt.Printf("created %s\n", k.URI())
			continue
		} else if !isConflict(err) {
			return fmt.Errorf("%s: %w", k.URI(), err)
		}
		if _, err := c.Update(ctx, k); err != nil {
			return fmt.Errorf("%s: %w", k.URI(), err)
		}
		updated++
		fmt.Printf("updated %s\n", k.URI())
	}
	fmt.Printf("imported %d entries (%d created, %d updated, %d skipped)\n",
		created+updated, created, updated, len(skipped))
	return nil
}

func cmdImportOssie(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai import-ossie [flags] <semantic-model.yaml | ->\n\nImport an Apache Ossie semantic model. Each model is stored for\n`compile`, and metric/table knowledge entries are derived so the\ndefinitions are searchable. Re-import refreshes definitions without\nclobbering human-curated status, tags, and bodies.",
		"  ochakai import-ossie examples/semantic-model.yaml\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	var src []byte
	if pos[0] == "-" {
		src, err = io.ReadAll(os.Stdin)
	} else {
		src, err = os.ReadFile(pos[0])
	}
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	report, err := c.ImportOssie(ctx, src)
	if err != nil {
		return err
	}
	for _, uri := range report.Created {
		fmt.Println("created", uri)
	}
	for _, uri := range report.Updated {
		fmt.Println("updated", uri)
	}
	fmt.Printf("imported %d models (%d entries created, %d updated)\n",
		len(report.Models), len(report.Created), len(report.Updated))
	return nil
}

func isConflict(err error) bool {
	var apiErr *apiclient.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// readBundle loads an OKF bundle into a path→content map from a directory,
// a tar.gz file, or stdin ("-", tar.gz).
func readBundle(path string) (map[string][]byte, error) {
	if path == "-" {
		return readBundleTarGz(os.Stdin)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return readBundleDir(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBundleTarGz(f)
}

// readBundleDir walks root, skipping dot-entries (.git and friends —
// bundles live happily in git).
func readBundleDir(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func readBundleTarGz(r io.Reader) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a gzip stream (directories are imported directly): %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(hdr.Name)) {
			return nil, fmt.Errorf("refusing unsafe path %q in archive", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		files[hdr.Name] = data
	}
}

// readEntry reads a knowledge entry from path ("" or "-" = stdin) in
// either of the interchange formats from design doc 0004 §5: an OKF
// document (leading ---) or JSON.
func readEntry(path string) (*domain.Knowledge, error) {
	var data []byte
	var err error
	if path == "" || path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return decodeEntry(data)
}

func decodeEntry(data []byte) (*domain.Knowledge, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("---")) {
		return okf.Parse(trimmed)
	}
	var k domain.Knowledge
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("input is neither an OKF document nor valid JSON: %w", err)
	}
	return &k, nil
}

func parseFilter(s string) (apiclient.Filter, error) {
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return apiclient.Filter{}, fmt.Errorf(`invalid filter %q (want "dataset.field op value")`, s)
	}
	field, op := parts[0], parts[1]
	raw := strings.Join(parts[2:], " ")
	var value any
	if op == "in" || op == "not_in" {
		var list []any
		for _, v := range strings.Split(raw, ",") {
			list = append(list, scalar(strings.TrimSpace(v)))
		}
		value = list
	} else {
		value = scalar(raw)
	}
	return apiclient.Filter{Field: field, Op: op, Value: value}, nil
}

// scalar guesses the value's type: numbers and booleans matter when the
// compiler renders them into SQL; everything else stays a string.
func scalar(s string) any {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	if s == "true" || s == "false" {
		return s == "true"
	}
	return s
}

func parseGrain(s string) (apiclient.TimeGrain, error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return apiclient.TimeGrain{}, fmt.Errorf(`invalid grain %q (want "dataset.field:day|week|month|quarter|year")`, s)
	}
	return apiclient.TimeGrain{Field: s[:i], Grain: s[i+1:]}, nil
}

// extractTarGz unpacks the OKF bundle under dir, refusing entries that
// could escape it (absolute or non-local paths) and anything but regular
// files.
func extractTarGz(dir string, r io.Reader) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.FromSlash(hdr.Name)
		if !filepath.IsLocal(name) {
			return n, fmt.Errorf("refusing unsafe path %q in archive", hdr.Name)
		}
		dst := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return n, err
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return n, err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return n, err
		}
		if err := f.Close(); err != nil {
			return n, err
		}
		n++
	}
}
