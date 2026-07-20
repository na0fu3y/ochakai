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
	"search":    cmdSearch,
	"browse":    cmdBrowse,
	"context":   cmdContext,
	"get":       cmdGet,
	"create":    cmdCreate,
	"update":    cmdUpdate,
	"delete":    cmdDelete,
	"move":      cmdMove,
	"attach":    cmdAttach,
	"detach":    cmdDetach,
	"usage":     cmdUsage,
	"report":    cmdReport,
	"revisions": cmdRevisions,
	"backlinks": cmdBacklinks,
	"compile":   cmdCompile,
	"export":    cmdExport,
	"import":    cmdImport,
	"use":       cmdUse,
	"whoami":    cmdWhoami,
	"ui":        cmdUI,

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

// parseRef parses an entry id (an "ochakai://" prefix is tolerated).
func parseRef(s string) (string, error) {
	id := strings.TrimPrefix(s, "ochakai://")
	if id == "" {
		return "", fmt.Errorf("want an entry id (e.g. metrics/revenue), got %q", s)
	}
	return id, nil
}

func cmdSearch(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai search [flags] [query]\n\nSearch the knowledge base; verified entries rank higher.\nOutput: score, uri, status, title — description (one hit per line).\nWith --sort verified_at the command lists by verification age instead\nof searching (oldest first, never-verified last — the golden-query\ncanary feed); output leads with verified_at. With --sort usage it lists\nby demand (most search_hits first, never-used oldest-first at the bottom\n— the draft review feed); output leads with the search_hits count.",
		"  ochakai search \"gross margin\" --type Metric --type 'Glossary Term' --status verified\n  ochakai search churn --json | jq '.hits[0].attrs'\n  ochakai search --sort verified_at --type 'Golden Query' --status verified --limit 100\n  ochakai search --sort usage --status draft --limit 50   # review queue\n")
	var types, statuses, tags repeated
	fs.Var(&types, "type", "filter by type: "+typeList()+", or any custom type (repeatable)")
	fs.Var(&statuses, "status", "filter by status: "+statusList()+" (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	sortBy := fs.String("sort", "", `list instead of search: "verified_at" = by verification age (oldest first), "usage" = by demand (most search_hits first)`)
	limit := fs.Int("limit", 0, "max results (server default 10, max 50; with --sort: 100, max 1000)")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *sortBy != "" && len(pos) > 0 {
		return fmt.Errorf("--sort lists entries; it cannot be combined with a search query")
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
		switch *sortBy {
		case "verified_at":
			lead = "-" // never verified sorts last
			if h.VerifiedAt != nil {
				lead = h.VerifiedAt.Format(time.RFC3339)
			}
		case "usage":
			lead = "0" // never-used drafts sort last
			if h.Usage != nil {
				lead = strconv.FormatInt(h.Usage.SearchHits, 10)
			}
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%s", lead, h.URI(), h.Status, h.DisplayTitle())
		if h.Description != "" {
			line += " — " + h.Description
		}
		fmt.Println(line)
	}
	return nil
}

func cmdBrowse(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai browse [flags] [prefix]\n\nList one level of the ID hierarchy (the folder view of design docs\n0014 and 0017, the CLI counterpart of the web UI's Browse tab).\nWithout an argument, the top-level directories with their entry\ncounts; with a prefix, the subdirectories and entries directly under\nit. Directories print as \"name/\tcount\", entries as\n\"segment\ttype\tstatus\ttitle\". Rejected entries are hidden, as in search.",
		"  ochakai browse\n  ochakai browse queries\n  ochakai browse ga4/tables\n")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		fs.Usage()
		return errReported
	}
	var prefix string
	if len(pos) == 1 {
		prefix = strings.TrimPrefix(pos[0], "ochakai://")
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	res, err := c.Browse(ctx, prefix)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(res)
	}
	for _, d := range res.Dirs {
		fmt.Printf("%s/\t%d\n", d.Name, d.Count)
	}
	prefix = strings.TrimSuffix(prefix, "/")
	for _, e := range res.Entries {
		seg := e.ID
		if prefix != "" {
			seg = strings.TrimPrefix(seg, prefix+"/")
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", seg, e.Type, e.Status, domain.DisplayTitle(e.Title, e.ID))
	}
	if res.Truncated {
		fmt.Fprintln(os.Stderr, "note: showing the first 1000 entries at this level (server cap)")
	}
	return nil
}

func cmdContext(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai context [flags] <question>\n\nGather what to read before answering a data question, in one call:\nthe full entries behind the top search hits (verified entries rank\nhigher), expanded one hop through links so the insight explaining a\nmetric travels with it. Markdown on stdout, ready for an agent's\ncontext window. No hits print nothing (exit 0).",
		"  ochakai context \"why did revenue drop in March?\"\n  ochakai context \"monthly revenue\" --type 'Golden Query' --status verified --json\n  ochakai context \"$PROMPT\" --budget 4000   # hooks: cap the injected bytes\n")
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
		rendered[k.ID] = true
	}
	if omitted > 0 {
		fmt.Fprintf(w, "(%d more entries beyond --budget; raise it or `ochakai get` them)\n", omitted)
	}
	var rest []string
	for _, h := range res.Hits {
		if !rendered[h.ID] {
			rest = append(rest, fmt.Sprintf("- %s (%s) — %s", h.URI(), h.Status, h.DisplayTitle()))
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(w, "\nAlso relevant (`ochakai get <id>` for the full entry):\n%s\n", strings.Join(rest, "\n"))
	}
}

func renderEntry(k *domain.Knowledge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (%s) — %s\n", k.URI(), k.Status, k.DisplayTitle())
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
		"Usage: ochakai usage [flags] <id>\n\nShow how often an entry was actually used: appeared in search results,\nfetched individually, referenced by compile — and when it was last\nused. The measure of the write-back loop: evidence for promoting a\ndraft, and a staleness signal for verified entries nobody uses.",
		"  ochakai usage queries/monthly-revenue\n  ochakai usage metrics/revenue --json\n")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	u, err := c.Usage(ctx, id)
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
	fmt.Printf("search_hits\t%d\nfetches\t%d\ncompiles\t%d\nworked\t%d\nfailed\t%d\nlast_used_at\t%s\n",
		u.SearchHits, u.Fetches, u.Compiles, u.Worked, u.Failed, last)
	return nil
}

func cmdReport(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai report [flags] <id> <worked|failed>\n\nReport whether acting on an entry gave a correct result — the last\nedge of the write-back loop. After running a golden query or compiled\nSQL, report worked or failed (say what went wrong with --note);\nfailed counts against verified entries flag them for re-verification.\nPrints the entry's updated usage totals.",
		"  ochakai report queries/monthly-revenue worked\n  ochakai report queries/monthly-revenue failed --note \"joins dropped 2024 rows after schema change\"\n")
	note := fs.String("note", "", "context recorded with the report: what was run, what went wrong (max 2000 bytes)")
	asJSON := fs.Bool("json", false, "print the updated usage totals as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	u, err := c.ReportOutcome(ctx, id, pos[1], *note)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(u)
	}
	fmt.Printf("reported %s ochakai://%s (worked %d, failed %d)\n", pos[1], id, u.Worked, u.Failed)
	return nil
}

func cmdRevisions(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai revisions [flags] <id>\n\nList an entry's change history, newest first: who changed it, how,\nand when — the audit surface behind \"every change kept as a\nrevision\". Works for soft-deleted entries too. Full snapshots are in\nthe JSON output (--json).",
		"  ochakai revisions metrics/revenue\n  ochakai revisions queries/monthly-revenue --json | jq '.revisions[0].snapshot'\n")
	limit := fs.Int("limit", 0, "max revisions (server default 50, max 200)")
	asJSON := fs.Bool("json", false, "print the raw JSON response (includes full snapshots)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	revs, err := c.Revisions(ctx, id, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(map[string]any{"revisions": revs})
	}
	for _, r := range revs {
		fmt.Printf("#%d\t%s\t%s:%s\t%s\n", r.Rev, r.Change,
			r.ChangedBy.Kind, r.ChangedBy.Name, r.ChangedAt.Format(time.RFC3339))
	}
	return nil
}

func cmdBacklinks(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai backlinks [flags] <id>\n\nList live entries whose links point at this entry, most recently\nupdated first — the reverse edge the web UI shows as \"linked from\"\n(context already follows it when packing companions).\nOutput: uri, status, title — description (one entry per line).",
		"  ochakai backlinks metrics/revenue\n  ochakai backlinks metrics/revenue --json | jq '.entries[].id'\n")
	limit := fs.Int("limit", 0, "max entries (server default 20, max 100)")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	entries, err := c.Backlinks(ctx, id, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(map[string]any{"entries": entries})
	}
	for i := range entries {
		e := &entries[i]
		line := fmt.Sprintf("%s\t%s\t%s", e.URI(), e.Status, e.Title)
		if e.Description != "" {
			line += " — " + e.Description
		}
		fmt.Println(line)
	}
	return nil
}

func cmdGet(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai get [flags] <id>\n\nPrint one knowledge entry as an OKF document (YAML frontmatter +\nmarkdown body). The output round-trips through `ochakai update`.\nAttachment metadata is listed on stderr; --download saves the\nattachment files themselves (an agent can then read them from disk).",
		"  ochakai get metrics/revenue\n  ochakai get queries/monthly-revenue --json | jq -r '.attrs.sql'\n  ochakai get insights/revenue-reading --download ./img\n")
	asJSON := fs.Bool("json", false, "print JSON instead of the OKF document")
	download := fs.String("download", "", "save the entry's attachments into this directory")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	k, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	if *download != "" && len(k.Attachments) > 0 {
		if err := os.MkdirAll(*download, 0o755); err != nil {
			return err
		}
		for _, att := range k.Attachments {
			data, _, err := c.Attachment(ctx, id, att.Name)
			if err != nil {
				return fmt.Errorf("attachment %s: %w", att.Name, err)
			}
			dst := filepath.Join(*download, att.Name)
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "saved %s (%s, %d bytes)\n", dst, att.MediaType, att.Size)
		}
	}
	if *asJSON {
		return printJSON(k)
	}
	doc, err := okf.Document(k)
	if err != nil {
		return err
	}
	if _, err = os.Stdout.Write(doc); err != nil {
		return err
	}
	if *download == "" {
		for _, att := range k.Attachments {
			fmt.Fprintf(os.Stderr, "attachment: %s (%s, %d bytes) — `ochakai get %s --download DIR` to save\n",
				att.Name, att.MediaType, att.Size, id)
		}
	}
	return nil
}

func cmdAttach(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai attach [flags] <id> <file...>\n\nAttach files to a knowledge entry (png, jpeg, webp, pdf, plain\ntext — the type is sniffed from the bytes; max 5 MiB each, 20 per\nentry). An attachment of the same name is replaced (the change is kept\nas a revision). Reference the file from the entry's body so its\ncaption is searchable and it survives OKF export/import — the hint\nprinted after attaching shows the canonical relative link. Requires\nthe server to have GCS configured (OCHAKAI_GCS_BUCKET).",
		"  ochakai attach insights/revenue-reading weekly.png\n  ochakai attach tables/orders seeds.txt\n  ochakai attach tables/orders er-diagram.png --name schema.png\n")
	name := fs.String("name", "", "attachment name (default: the file's basename; single file only)")
	asJSON := fs.Bool("json", false, "print the attachment metadata as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 || (*name != "" && len(pos) != 2) {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	// The canonical body link is relative to the entry's own document:
	// "<id last segment>/<name>" (design doc 0008).
	lastSeg := id[strings.LastIndex(id, "/")+1:]
	for _, file := range pos[1:] {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		attName := *name
		if attName == "" {
			attName = filepath.Base(file)
		}
		att, err := c.Attach(ctx, id, attName, "", data)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if *asJSON {
			if err := printJSON(att); err != nil {
				return err
			}
			continue
		}
		fmt.Printf("attached %s/%s (%s, %d bytes)\n", id, att.Name, att.MediaType, att.Size)
		link := fmt.Sprintf("[%s](%s/%s)", att.Name, lastSeg, att.Name)
		if strings.HasPrefix(att.MediaType, "image/") {
			link = "!" + link
		}
		fmt.Fprintf(os.Stderr, "hint: reference it from the body: %s\n", link)
	}
	return nil
}

func cmdDetach(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai detach [flags] <id> <name>\n\nRemove an attachment from a knowledge entry (the change is kept as a\nrevision; content-addressed bytes stay referenced by history).",
		"  ochakai detach insights/revenue-reading weekly.png\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	if err := c.Detach(ctx, id, pos[1]); err != nil {
		return err
	}
	fmt.Printf("detached %s/%s\n", id, pos[1])
	return nil
}

func cmdCreate(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai create [flags] [id]\n\nCreate a knowledge entry from -f or stdin. Input is an OKF document\n(--- frontmatter with type, markdown body — the format `ochakai get`\nprints; title is optional, the id's last segment is the display name\nwhen it is absent) or JSON (see api/openapi.yaml). The id is the\nentry's path; pass it as the argument (it overrides an id in the\ninput, and OKF documents carry none — the path is the id). Entries\ndefault to draft; provenance is recorded from your Google identity.",
		"  ochakai get insights/revenue-seasonality | sed s/40%/45%/ | ochakai create insights/revenue-seasonality-v2\n  ochakai create runbook/restore -f entry.md\n")
	file := fs.String("f", "", "input file (default: stdin)")
	asJSON := fs.Bool("json", false, "print the created entry as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		fs.Usage()
		return errReported
	}
	k, err := readEntry(*file)
	if err != nil {
		return err
	}
	if len(pos) == 1 {
		if k.ID, err = parseRef(pos[0]); err != nil {
			return err
		}
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
		"Usage: ochakai update [flags] <id>\n\nReplace a knowledge entry from -f or stdin (OKF document or JSON;\nthe id comes from the argument, the type from the input). Every\nchange is kept as a revision server-side.",
		"  ochakai get metrics/revenue | $EDITOR /dev/stdin | ochakai update metrics/revenue\n  ochakai update metrics/revenue -f revenue.md\n")
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
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	k, err := readEntry(*file)
	if err != nil {
		return err
	}
	k.ID = id
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	updated, changed, err := c.Update(ctx, k)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(updated)
	}
	if !changed {
		fmt.Printf("unchanged %s (%s)\n", updated.URI(), updated.Status)
		return nil
	}
	fmt.Printf("updated %s (%s)\n", updated.URI(), updated.Status)
	return nil
}

func cmdDelete(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai delete [flags] <id>\n\nSoft-delete a knowledge entry (history is retained server-side).",
		"  ochakai delete terms/obsolete-kpi\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	if err := c.Delete(ctx, id); err != nil {
		return err
	}
	fmt.Printf("deleted ochakai://%s\n", id)
	return nil
}

func cmdMove(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai move [flags] <id> <new-id>\n\nMove (rename) a knowledge entry to a new id. Revisions, usage, and\nattachments follow, and inbound references (link targets, attrs.model)\nare rewritten so nothing breaks.",
		"  ochakai move insights/revenue-seasonality insights/sales/revenue-seasonality\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		fs.Usage()
		return errReported
	}
	id, err := parseRef(pos[0])
	if err != nil {
		return err
	}
	newID, err := parseRef(pos[1])
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	moved, err := c.Move(ctx, id, newID)
	if err != nil {
		return err
	}
	fmt.Printf("moved ochakai://%s -> %s\n", id, moved.URI())
	return nil
}

func cmdCompile(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"Usage: ochakai compile [flags] --metric <name>\n\nDeterministically compile metrics into BigQuery SQL (never executed, never\nguessed). SQL goes to stdout; notes and related verified golden\nqueries go to stderr — prefer a verified query when it answers the\nquestion. Exit 2 means the request is outside the supported subset;\nthe reason is on stderr.",
		"  ochakai compile --metric revenue --dimension customers.region --grain orders.ordered_at:month\n  ochakai compile --metric revenue --filter \"orders.status = shipped\" > revenue.sql\n")
	var metrics, dims, filters repeated
	fs.Var(&metrics, "metric", "metric name (repeatable, required)")
	fs.Var(&dims, "dimension", "group-by column as dataset.field (repeatable)")
	fs.Var(&filters, "filter", `filter as "dataset.field op value"; op: = != > >= < <= in not_in (in/not_in take comma-separated values) (repeatable)`)
	grain := fs.String("grain", "", "time grain as dataset.field:day|week|month|quarter|year")
	model := fs.String("model", "", "models entry id, e.g. models/sales (default: the models entry defining the first metric; required when several do)")
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
		label := q.DisplayTitle()
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
		"Usage: ochakai import [flags] <dir | file.tar.gz | ->\n\nImport an OKF bundle (a directory of markdown + YAML frontmatter, or\na tar.gz of one; \"-\" reads the tar.gz from stdin). The inverse of\n`ochakai export`: each path names its entry (the path minus .md is\nthe id), the frontmatter type key names the type (required — files\nwithout one are skipped and reported), reserved index.md / log.md\nfiles are skipped, unknown frontmatter keys are kept as attrs, and\nexisting entries are replaced (kept as revisions; entries identical\nto what is stored are left untouched and reported as unchanged;\nentries the server rejects as invalid — e.g. a models entry whose\nspec fails validation — are skipped and reported).\nFiles referenced by an entry's body markdown links become its\nattachments, wherever they sit in the bundle (their location is\npreserved for re-export); unreferenced data files inside an entry's\ndirectory (<id>/<name>) attach to that entry. The packed shape is\nthe structure: an archive wrapped in a single directory imports\nunder that directory — the bundle keeps its own namespace. Works\nwith any OKF bundle, not just ochakai's own.",
		"  ochakai import ./knowledge\n  ochakai import ga4-bundle.tar.gz --dry-run\n  ochakai export - | OCHAKAI_URL=https://other ochakai import -\n")
	dryRun := fs.Bool("dry-run", false, "parse and list what would be written, write nothing")
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
	entries, atts, skipped := okf.FromBundle(files)
	for _, s := range skipped {
		fmt.Fprintln(os.Stderr, "skip:", s)
	}
	if *dryRun {
		for i := range entries {
			fmt.Printf("would import %s\n", entries[i].URI())
		}
		for _, a := range atts {
			fmt.Printf("would attach %s/%s (from %s)\n", a.ID, a.Name, a.Path)
		}
		fmt.Printf("dry run: %d entries, %d attachments, %d skipped\n", len(entries), len(atts), len(skipped))
		return nil
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	// A 400 is the server's judgment on one document (e.g. a models entry
	// whose spec fails write-time validation) — skip and report it like a
	// parse failure, instead of aborting the bundle halfway (design doc
	// 0019). Anything else (auth, network, 5xx) still aborts.
	var created, updated, unchanged int
	rejected := map[string]bool{}
	skipEntry := func(k *domain.Knowledge, err error) {
		rejected[k.ID] = true
		skipped = append(skipped, k.ID+".md: rejected by the server: "+err.Error())
		fmt.Fprintln(os.Stderr, "skip:", skipped[len(skipped)-1])
	}
	for i := range entries {
		k := &entries[i]
		if _, err := c.Create(ctx, k); err == nil {
			created++
			fmt.Printf("created %s\n", k.URI())
			continue
		} else if isInvalid(err) {
			skipEntry(k, err)
			continue
		} else if !isConflict(err) {
			return fmt.Errorf("%s: %w", k.URI(), err)
		}
		_, changed, err := c.Update(ctx, k)
		if err != nil {
			if isInvalid(err) {
				skipEntry(k, err)
				continue
			}
			return fmt.Errorf("%s: %w", k.URI(), err)
		}
		if !changed {
			unchanged++
			fmt.Printf("unchanged %s\n", k.URI())
			continue
		}
		updated++
		fmt.Printf("updated %s\n", k.URI())
	}
	attached := 0
	for _, a := range atts {
		if rejected[a.ID] {
			skipped = append(skipped, a.Path+": its entry "+a.ID+" was not imported")
			fmt.Fprintln(os.Stderr, "skip:", skipped[len(skipped)-1])
			continue
		}
		// A file already at the canonical layout needs no okf_path — the
		// preserved-location rule is for foreign layouts only.
		okfPath := a.Path
		if okfPath == a.ID+"/"+a.Name {
			okfPath = ""
		}
		if _, err := c.Attach(ctx, a.ID, a.Name, okfPath, a.Data); err != nil {
			return fmt.Errorf("attach %s/%s: %w", a.ID, a.Name, err)
		}
		attached++
		fmt.Printf("attached %s/%s\n", a.ID, a.Name)
	}
	fmt.Printf("imported %d entries (%d created, %d updated, %d unchanged, %d attachments, %d skipped)\n",
		created+updated+unchanged, created, updated, unchanged, attached, len(skipped))
	return nil
}

// isInvalid reports a 400: the server understood the request and judged
// this one document invalid — a per-file verdict, not a broken pipeline.
func isInvalid(err error) bool {
	var apiErr *apiclient.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest
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
