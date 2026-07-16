// Client-side subcommands (design doc 0004): pure clients of a remote
// ochakai's /api/v1, named by --url or $OCHAKAI_URL. Server-admin
// commands (serve, import-ossie, export-okf) live in main.go and talk to
// the database directly.
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

	"github.com/na0fu3y/ochakai/internal/apiclient"
	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
)

var clientCommands = map[string]func(context.Context, []string) error{
	"search":  cmdSearch,
	"get":     cmdGet,
	"create":  cmdCreate,
	"update":  cmdUpdate,
	"delete":  cmdDelete,
	"compile": cmdCompile,
	"export":  cmdExport,
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

func newFlagSet(synopsis, examples string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "%s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
		if examples != "" {
			fmt.Fprintf(fs.Output(), "\nExamples:\n%s", examples)
		}
	}
	url := fs.String("url", os.Getenv("OCHAKAI_URL"), "ochakai server URL (default: $OCHAKAI_URL)")
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
		return nil, errors.New("server URL required: pass --url or set OCHAKAI_URL")
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
		"Usage: ochakai search [flags] [query]\n\nSearch the knowledge base; verified entries rank higher.\nOutput: score, uri, status, title — description (one hit per line).",
		"  ochakai search \"gross margin\" --type metric --type term --status verified\n  ochakai search churn --json | jq '.hits[0].attrs'\n")
	var types, statuses, tags repeated
	fs.Var(&types, "type", "filter by type: metric|query|insight|term|table (repeatable)")
	fs.Var(&statuses, "status", "filter by status: draft|verified|deprecated (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	limit := fs.Int("limit", 0, "max results (server default 10, max 50)")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	hits, err := c.Search(ctx, strings.Join(pos, " "), types, statuses, tags, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(map[string]any{"hits": hits})
	}
	for _, h := range hits {
		line := fmt.Sprintf("%.3f\t%s\t%s\t%s", h.Score, h.URI(), h.Status, h.Title)
		if h.Description != "" {
			line += " — " + h.Description
		}
		fmt.Println(line)
	}
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
	req := service.CompileRequest{Model: *model, Request: compiler.Request{
		Metrics:    metrics,
		Dimensions: dims,
		Dialect:    *dialect,
		Limit:      *limit,
	}}
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

func parseFilter(s string) (compiler.Filter, error) {
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return compiler.Filter{}, fmt.Errorf(`invalid filter %q (want "dataset.field op value")`, s)
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
	return compiler.Filter{Field: field, Op: op, Value: value}, nil
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

func parseGrain(s string) (compiler.TimeGrain, error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return compiler.TimeGrain{}, fmt.Errorf(`invalid grain %q (want "dataset.field:day|week|month|quarter|year")`, s)
	}
	return compiler.TimeGrain{Field: s[:i], Grain: s[i+1:]}, nil
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
