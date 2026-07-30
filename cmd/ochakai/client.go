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
	"put":       cmdPut,
	"verify":    cmdVerify,
	"reject":    cmdReject,
	"delete":    cmdDelete,
	"purge":     cmdPurge,
	"reembed":   cmdReembed,
	"move":      cmdMove,
	"attach":    cmdAttach,
	"detach":    cmdDetach,
	"usage":     cmdUsage,
	"stats":     cmdStats,
	"report":    cmdReport,
	"revisions": cmdRevisions,
	"log":       cmdLog,
	"export":    cmdExport,
	"import":    cmdImport,
	"use":       cmdUse,
	"whoami":    cmdWhoami,
	"ui":        cmdUI,
	"mcp-stdio": cmdMCPStdio,

	"completion": cmdCompletion,
}

// runClient dispatches a client command and maps errors to exit codes:
// 0 success, 1 error, 2 "the command ran and the answer is no"
// (`queues --exit-code` with work waiting). Two codes rather than one
// because a scheduled job must not read "the server is unreachable" as
// "the queues are empty" — both are non-zero, and only one of them means
// somebody has reviewing to do.
func runClient(name string, args []string) int {
	err := clientCommands[name](context.Background(), args)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errWorkPending):
		return 2
	case errors.Is(err, errReported):
		return 1
	}
	fmt.Fprintf(os.Stderr, "ochakai %s: %v\n", name, err)
	return 1
}

// errReported means the FlagSet already printed the problem.
var errReported = errors.New("usage error")

// errWorkPending is not a failure: the command did what it was asked and
// is reporting the answer through the exit status, the way `git diff
// --exit-code` and `grep -q` do. Nothing is printed for it.
var errWorkPending = errors.New("work is waiting")

// helpOutput is where a command's own help goes. It is os.Stderr, which
// is where the flag package would have put it anyway; naming it is what
// lets docs/cli.md be generated from the help itself rather than written
// beside it (cmd/ochakai/clidocs_test.go).
var helpOutput io.Writer = os.Stderr

// commandFlagSets holds the FlagSet each command built, keyed by the
// command name it was created under. Nothing at runtime reads it: it is
// how the completion test enumerates what flags a command really has.
// Every command registers its flags inside its own body, so they exist
// only once the command has run — and a hand-copied list of them is
// exactly what let `ochakai search --source` ship missing from all three
// completion scripts.
var commandFlagSets = map[string]*flag.FlagSet{}

func newBareFlagSet(name, synopsis, examples string) *flag.FlagSet {
	fs := flag.NewFlagSet("ochakai "+name, flag.ContinueOnError)
	fs.SetOutput(helpOutput)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "%s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
		if examples != "" {
			fmt.Fprintf(fs.Output(), "\nExamples:\n%s", examples)
		}
	}
	commandFlagSets[name] = fs
	return fs
}

func newFlagSet(name, synopsis, examples string) (*flag.FlagSet, *string) {
	fs := newBareFlagSet(name, synopsis, examples)
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

// exactArgs parses flags and requires exactly n positional arguments. A
// wrong count prints the command's own usage and returns errReported: the
// help text says what the command takes, so repeating it in an error
// message would only be a second, shorter description to keep in sync.
func exactArgs(fs *flag.FlagSet, args []string, n int) ([]string, error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != n {
		fs.Usage()
		return nil, errReported
	}
	return pos, nil
}

// idArgs is exactArgs for the commands whose first positional argument is
// a concept id: it returns that id parsed, and the arguments after it.
func idArgs(fs *flag.FlagSet, args []string, n int) (id string, rest []string, err error) {
	pos, err := exactArgs(fs, args, n)
	if err != nil {
		return "", nil, err
	}
	if id, err = parseRef(pos[0]); err != nil {
		return "", nil, err
	}
	return id, pos[1:], nil
}

func newClient(ctx context.Context, url string) (*apiclient.Client, error) {
	if url == "" {
		return nil, errors.New("server URL required: run `ochakai use <url>`, set OCHAKAI_URL, or pass --url")
	}
	c, err := apiclient.New(ctx, url)
	if err != nil {
		return nil, err
	}
	// An environment variable rather than a flag: the caller that has
	// something to declare here is an agent shelling out to the CLI, and
	// it sets the variable once for the process instead of remembering a
	// flag on every subcommand (design doc 0052 §3.3). A person running
	// `ochakai` by hand is running no producer and sets nothing.
	if p := os.Getenv("OCHAKAI_PRODUCER"); p != "" {
		if !domain.ValidProducer(p) {
			return nil, fmt.Errorf(
				`OCHAKAI_PRODUCER: want "<producer>/<version>" — one slash, no whitespace, no colon, at most %d bytes (OKF SPEC §7), got %q`,
				domain.MaxProducer, p)
		}
		c.SetProducer(p)
	}
	return c, nil
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

// optBool turns an opt-in boolean flag into the tri-state the filters
// take: unset stays unasked rather than becoming an explicit false.
func optBool(v bool) *bool {
	if !v {
		return nil
	}
	return &v
}

func statusList() string {
	ss := make([]string, len(domain.Statuses))
	for i, s := range domain.Statuses {
		ss[i] = string(s)
	}
	return strings.Join(ss, "|")
}

// trustList renders SPEC §5.3's tiers for the flag's help, from the one
// place the vocabulary is spelled (design doc 0038 §4.4's rule, applied
// to the second vocabulary OKF gives ochakai).
func trustList() string { return strings.ReplaceAll(domain.TrustsHint(), ", ", "|") }

// fmUsage describes --fm from the vocabulary the server enforces, so the
// help cannot promise a key a request would be refused for (design doc
// 0047 §2).
var fmUsage = "filter by an OKF frontmatter `key=value`, exactly (repeatable, AND-ed) — the OKF keys with no flag of their own (" +
	strings.Join(domain.AskableFrontmatterKeys(), ", ") + "); a value spelling a number or a boolean matches the typed one too " +
	"(--fm required=true). A producer's own key is kept and handed back as written but is not part of the query vocabulary, and " +
	"type, status, tags, sources and stale_after have filters of their own that answer from a column instead"

// fmPairs turns repeated --fm key=value flags into the map the API takes.
// A flag with no "=" is a mistake worth naming: silently ignoring it
// would answer a narrower question than the caller asked, without saying
// so.
func fmPairs(vals []string) (map[string]string, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(vals))
	for _, v := range vals {
		key, value, ok := strings.Cut(v, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("--fm wants key=value, got %q", v)
		}
		out[key] = value
	}
	return out, nil
}

// parseRef parses a concept id (an "ochakai://" prefix is tolerated).
func parseRef(s string) (string, error) {
	id := strings.TrimPrefix(s, "ochakai://")
	if id == "" {
		return "", fmt.Errorf("want a concept id (e.g. metrics/revenue), got %q", s)
	}
	return id, nil
}

func cmdSearch(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"search",
		"Usage: ochakai search [flags] [query]\n\nSearch the knowledge base; verified concepts rank higher.\nOutput: score, uri, status, title — description (one hit per line).\nWith --sort verified_at the command lists by verification age instead\nof searching (oldest first, never-verified last — the\ncanary feed); output leads with verified_at. With --sort usage it lists\nby demand (most search_hits first, never-used oldest-first at the bottom\n— the draft review feed); output leads with the search_hits count.\nWith --sort failed it lists concepts whose failure reports (report_outcome\nfailed) are still unanswered, worst first — the re-verification feed;\noutput leads with the failed count. `ochakai verify` takes a concept out\nof it, so a base that is kept up shows an empty feed.\nWith --sort stale_after it lists concepts whose declared stale_after has\npassed, most overdue first; output leads with that date. Verifying does\nnot empty this one — the date is the writer's declaration, so clearing it\nmeans editing the concept to re-declare an expiry.\n--source, --links-to and --prefix are filters, not modes: they combine\nwith a query or with any --sort. --source narrows to the concepts citing\none resource (the reverse of sources[].resource); --links-to narrows to\nthe concepts whose body links at one concept — its backlinks, the reverse\nof its inbound edges; --prefix narrows to\nthe concepts living under a path, which is how a team's own knowledge is\ntold apart from the company-wide vocabulary.\nA listing that has more behind it prints the way on to stderr; pass it\nback with --cursor to read the next page. A search prints none: it is\nbounded by --limit, and a ranking has no page two.",
		"  ochakai search \"gross margin\" --type Metric --type 'Glossary Term' --trust human-reviewed\n  ochakai search churn --json | jq -r '.hits[] | .id'\n  ochakai search --sort verified_at --type 'Attested Computation' --trust human-reviewed --limit 100\n  ochakai search --sort usage --status draft --limit 50   # review queue\n  ochakai search --sort failed --trust human-reviewed     # re-verification queue\n  ochakai search --sort stale_after                         # past their declared expiry\n  ochakai search --source https://wiki.example/finance/revenue-recognition  # what cites this\n  ochakai search --links-to metrics/revenue --type Insight   # which insights read this metric\n  ochakai search 活性化 --prefix teams/growth --prefix company   # our scope and the shared one\n")
	var types, statuses, tags, prefixes repeated
	fs.Var(&types, "type", "filter by type: "+typeList()+", or any custom type (repeatable)")
	fs.Var(&statuses, "status", "filter by status: "+statusList()+" (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	fs.Var(&prefixes, "prefix", "only concepts under this `path`, e.g. teams/growth — matched on segment boundaries, so it does not reach teams/growth-archive (repeatable, OR-ed)")
	source := fs.String("source", "", "only concepts citing this `resource` (exact match against sources[].resource) — what derives from one piece of material")
	linksTo := fs.String("links-to", "", "only concepts whose body links at this `id` — what points at one concept, in address order (its backlinks)")
	var trust repeated
	fs.Var(&trust, "trust", "filter by who confirmed the concept: "+trustList()+" (repeatable, OR-ed) — independent of --status, which is the lifecycle value")
	var fm repeated
	fs.Var(&fm, "fm", fmUsage)
	rejected := fs.Bool("rejected", false, "only concepts a human turned down — how you check whether a proposal was already rejected. Without it, rejected concepts stay out of results")
	sortBy := fs.String("sort", "", `list instead of search: "verified_at" = by verification age (oldest first), "usage" = by demand (most search_hits first), "failed" = by failed outcome reports (re-verification feed), "stale_after" = past their declared expiry, most overdue first`)
	limit := fs.Int("limit", 0, "max results (server default 10, max 50; with --sort: 100, max 1000)")
	cursor := fs.String("cursor", "", "resume a listing where the last page ended: the `cursor` the previous page printed, with the same --sort and filters. Listings only — a search is bounded by --limit")
	asJSON := fs.Bool("json", false, "print the raw JSON response")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *sortBy != "" && len(pos) > 0 {
		return fmt.Errorf("--sort lists concepts; it cannot be combined with a search query")
	}
	if *sortBy == "" && *source == "" && strings.TrimSpace(strings.Join(pos, " ")) == "" {
		return fmt.Errorf("search needs a query; use --sort to list concepts without one, or --source to list what cites a resource")
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	pairs, err := fmPairs(fm)
	if err != nil {
		return err
	}
	page, err := c.Search(ctx, apiclient.SearchParams{
		Query: strings.Join(pos, " "), Types: types, Statuses: statuses, Tags: tags,
		Source: *source, LinksTo: *linksTo, Prefixes: prefixes, Sort: *sortBy, Limit: *limit,
		Trust: trust, Rejected: optBool(*rejected), FM: pairs, Cursor: *cursor,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(page)
	}
	for _, h := range page.Hits {
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
		case "failed":
			lead = "0"
			if h.Usage != nil {
				lead = strconv.FormatInt(h.Usage.Failed, 10)
			}
		case "stale_after":
			lead = h.StaleAfter
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%s", lead, h.URI(), h.Status, h.Title)
		if h.Description != "" {
			line += " — " + h.Description
		}
		fmt.Println(line)
	}
	// The way on, on stderr: stdout stays one hit per line, so a pipe
	// reads the same whether or not there is another page (design doc
	// 0050 §4). The command does not walk the pages itself — --limit is
	// the bound the caller set, and a listing that grew past it on its
	// own would surprise whoever piped it into head.
	if page.Cursor != "" {
		fmt.Fprintf(os.Stderr, "note: more concepts — rerun with --cursor %s\n", page.Cursor)
	}
	return nil
}

func cmdBrowse(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"browse",
		"Usage: ochakai browse [flags] [prefix]\n\nList one level of the ID hierarchy (the folder view of design docs\n0014 and 0017, the CLI counterpart of the web UI's Browse tab).\nWithout an argument, the top-level directories with their concept\ncounts; with a prefix, the subdirectories and concepts directly under\nit. Directories print as \"name/\tcount\", concepts as\n\"segment\ttype\tstatus\ttitle\", and the files in the directory as\n\"name\tfile\tmedia-type\tbytes\". Rejected concepts are hidden, as in\nsearch.",
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
	// The files in the directory, after the concepts and marked as what
	// they are: a file is an object in the bundle (design doc 0046 §3.3)
	// and has no type or status to print, so the column that would hold
	// them holds what it does have.
	for _, f := range res.Files {
		fmt.Printf("%s\tfile\t%s\t%d\n", f.Name, f.MediaType, f.Size)
	}
	if res.Truncated {
		fmt.Fprintln(os.Stderr, "note: showing the first 1000 concepts at this level (server cap)")
	}
	return nil
}

func cmdContext(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"context",
		"Usage: ochakai context [flags] <question>\n\nGather what to read before answering a data question, in one call:\nthe full concepts behind the top search hits (verified concepts rank\nhigher), expanded one hop through links so the insight explaining a\nmetric travels with it. Markdown on stdout, ready for an agent's\ncontext window. No hits print nothing (exit 0).",
		"  ochakai context \"why did revenue drop in March?\"\n  ochakai context \"monthly revenue\" --type 'Attested Computation' --trust human-reviewed --json\n  ochakai context \"$PROMPT\" --budget 4000   # hooks: cap the injected bytes\n  ochakai context \"activation rate\" --prefix teams/growth --prefix company\n")
	var types, statuses, tags, prefixes repeated
	fs.Var(&types, "type", "filter by type: "+typeList()+", or any custom type (repeatable)")
	fs.Var(&statuses, "status", "filter by status: "+statusList()+" (repeatable)")
	fs.Var(&tags, "tag", "filter by tag (repeatable)")
	fs.Var(&prefixes, "prefix", "only concepts under this `path`, e.g. teams/growth (repeatable, OR-ed); scopes the search, not the links it expands")
	var trust repeated
	fs.Var(&trust, "trust", "filter by who confirmed the concept: "+trustList()+" (repeatable, OR-ed) — independent of --status, which is the lifecycle value")
	var fm repeated
	fs.Var(&fm, "fm", fmUsage)
	limit := fs.Int("limit", 0, "max full concepts (server default 5, max 20)")
	budget := fs.Int("budget", 0, "cap the response at ~this many bytes (0 = no cap); the rendered output stops printing concepts, --json asks the server to cap and list what did not fit under \"outline\"")
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
	// The budget goes to the server only for --json: the rendered path
	// asks for everything and caps while printing, where it can name what
	// it dropped. JSON has no such layer, so an unsent budget was silently
	// doing nothing.
	serverBudget := 0
	if *asJSON {
		serverBudget = *budget
	}
	pairs, err := fmPairs(fm)
	if err != nil {
		return err
	}
	res, err := c.Context(ctx, apiclient.ContextParams{
		Query: strings.Join(pos, " "), Types: types, Statuses: statuses, Tags: tags,
		Prefixes: prefixes, Trust: trust, FM: pairs, Limit: *limit, Budget: serverBudget,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(res)
	}
	renderContext(os.Stdout, res, *budget)
	return nil
}

// renderContext prints the pack as compact markdown: per concept a heading
// with URI, status, and title, a provenance line, then the concept's
// document. Once the byte budget is
// spent the remaining concepts are dropped with a note (the first concept
// always renders); hits without a rendered concept become one-line pointers.
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
		fmt.Fprintf(w, "(%d more concepts beyond --budget; raise it or `ochakai get` them)\n", omitted)
	}
	var rest []string
	for i := range res.Hits {
		h := &res.Hits[i]
		if !rendered[h.ID] {
			rest = append(rest, fmt.Sprintf("- %s (%s) — %s", h.URI(), h.Status, h.Title))
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(w, "\nAlso relevant (`ochakai get <id>` for the full concept):\n%s\n", strings.Join(rest, "\n"))
	}
}

// renderEntry writes one concept of a context pack: a heading, what this
// instance observed about it, and then the concept itself — its document.
//
// It used to assemble a curated summary of selected fields, which was the
// only way to show a concept when a concept was a bag of fields. Now that
// a concept is a document, printing the document is both simpler and more
// faithful: the agent reading this pack sees the same shape it will see
// from `ochakai get`, from MCP, and in a git-tracked bundle, and nothing
// the writer wrote is left out because this function did not know to look
// for it.
func renderEntry(v *domain.View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (%s) — %s\n", v.URI(), v.Summary.Status, v.Summary.Title)
	prov := "created by " + v.Observed.CreatedBy.String()
	if lv := v.Observed.LastVerified(); lv != nil {
		verified := fmt.Sprintf("verified by %s on %s", lv.By.String(), lv.At.Format("2006-01-02"))
		if n := len(v.Observed.Verified); n > 1 {
			verified += fmt.Sprintf(" (%d verifications)", n)
		}
		prov = verified + "; " + prov
	}
	fmt.Fprintln(&b, prov)
	if r := v.Observed.Rejection; r != nil {
		fmt.Fprintf(&b, "rejected by %s on %s\n", r.By.String(), r.At.Format("2006-01-02"))
		if r.Note != "" {
			fmt.Fprintf(&b, "rejection note: %s\n", r.Note)
		}
	}
	if doc := strings.TrimSpace(v.Document); doc != "" {
		fmt.Fprintf(&b, "\n%s\n", doc)
	}
	b.WriteString("\n")
	return b.String()
}

func cmdUsage(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"usage",
		"Usage: ochakai usage [flags] <id>\n\nShow how often a concept was actually used: appeared in search results,\nfetched individually, reported worked or failed — and when it was last\nused. The measure of the write-back loop: evidence for promoting a\ndraft, and a staleness signal for verified concepts nobody uses.",
		"  ochakai usage queries/sales/monthly-revenue\n  ochakai usage metrics/revenue --json\n")
	asJSON := fs.Bool("json", false, "print JSON")
	id, _, err := idArgs(fs, args, 1)
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
	fmt.Printf("search_hits\t%d\nfetches\t%d\nworked\t%d\nfailed\t%d\nlast_used_at\t%s\n",
		u.SearchHits, u.Fetches, u.Worked, u.Failed, last)
	return nil
}

func cmdStats(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"stats",
		"Usage: ochakai stats [flags]\n\nShow the improvement loop as the instance sees it: what the knowledge\nbase is made of now, how much each queue is holding, what review did\nlately, what callers reported, and what they searched for and did not\nfind. `usage` measures one concept; this measures the base.\n\nOne line per number, so it composes: cron it and diff the output, or\ngrep one line out of it for a prompt or a dashboard. The gap lines are\nthe questions that came back empty, most-asked first — the list of what\nto write next.\n\nThe three queue lines — drafts waiting to be published or turned down,\nconcepts whose failure reports are unanswered, concepts past the expiry\ntheir author declared — carry the command that lists that queue, with\nthe scope you asked under, so the next step is the text on the line.\nThe verification-age feed is not one of them: it ranks every verified\nconcept rather than holding the ones that need something, so its size is\nthe size of the knowledge base and never reaches zero.\nWith --exit-code the command exits 2 while any of the three is\nnon-empty and 0 when all are, which is how a scheduled job goes red on\nwork nobody has picked up. An error still exits 1, so \"unreachable\"\ncannot be read as \"nothing to do\".",
		"  ochakai stats\n  ochakai stats --days 7\n  ochakai stats --prefix teams/growth       # our subtree only\n  ochakai stats --json | jq .queues.drafts\n  ochakai stats --exit-code                 # in CI: red while somebody owes a review\n")
	days := fs.Int("days", 0, "how far back the flow numbers reach, 1-180 (default: 30; raw events are pruned after 180 days)")
	var prefixes repeated
	fs.Var(&prefixes, "prefix", "measure only concepts under this `path`, e.g. teams/growth — matched on segment boundaries (repeatable, OR-ed). The unanswered questions are not scoped: one that found nothing found it nowhere")
	exitCode := fs.Bool("exit-code", false, "exit 2 while any review queue is non-empty (0 when all are empty, 1 on error) — for cron and CI")
	asJSON := fs.Bool("json", false, "print JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	st, err := c.Stats(ctx, *days, prefixes)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(st); err != nil {
			return err
		}
		return pendingWork(st.Queues, *exitCode)
	}
	// Tab-separated key/value, like `usage`: the vocabularies are printed
	// in their own order (domain.Statuses, domain.Trusts) so a value
	// added to either shows up here without this command being edited.
	fmt.Printf("entries\t%d\n", st.Entries.Total)
	for _, s := range domain.Statuses {
		fmt.Printf("%s\t%d\n", s, st.Entries.Status[string(s)])
	}
	for _, t := range domain.Trusts {
		fmt.Printf("%s\t%d\n", t, st.Entries.Trust[string(t)])
	}
	fmt.Printf("rejected\t%d\n", st.Entries.Rejected)
	// The three queues take a third field: the command that lists that
	// queue, under the scope this call was made with. A number nobody can
	// act on is a statistic, and these three exist to be emptied (design
	// doc 0049) — so the next step is the text on the line. The key and
	// the count stay in the first two fields, so the format still cuts.
	scope := ""
	for _, p := range prefixes {
		scope += " --prefix " + p
	}
	for _, q := range []struct {
		name  string
		count int64
		lists string
	}{
		{"drafts", st.Queues.Drafts, "--sort usage --status draft"},
		{"failed", st.Queues.Failed, "--sort failed"},
		{"stale_after", st.Queues.StaleAfter, "--sort stale_after"},
	} {
		fmt.Printf("%s\t%d\tochakai search %s%s\n", q.name, q.count, q.lists, scope)
	}
	fmt.Printf("window_days\t%d\ncreated\t%d\nverifications\t%d\nworked\t%d\nfailed\t%d\n",
		st.WindowDays, st.Entries.Created, st.Review.Verifications,
		st.Outcomes.Worked, st.Outcomes.Failed)
	if !st.Misses.Recording {
		// Not zero — unknown. A deployment that keeps no questions must
		// not read as one that was asked none.
		fmt.Print("misses\t-\n")
		return pendingWork(st.Queues, *exitCode)
	}
	fmt.Printf("misses\t%d\n", st.Misses.Count)
	for _, q := range st.Misses.Queries {
		fmt.Printf("gap\t%d\t%s\n", q.Count, q.Query)
	}
	return pendingWork(st.Queues, *exitCode)
}

// pendingWork turns "somebody owes a review" into an exit status a
// scheduler can watch: 2 while a queue is holding something, 0 when the
// three are empty. An error is still 1, so an unreachable server cannot
// be read as nothing to do (design doc 0049).
func pendingWork(q domain.QueueCounts, asked bool) error {
	if asked && q.Total() > 0 {
		return errWorkPending
	}
	return nil
}

func cmdReport(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"report",
		"Usage: ochakai report [flags] <id> <worked|failed>\n\nReport whether acting on a concept gave a correct result — the last\nedge of the write-back loop. After running an attested computation or SQL you\nwrote from a concept, report worked or failed (say what went wrong with\n--note); failed counts against verified concepts flag them for\nre-verification. Prints the concept's updated usage totals.",
		"  ochakai report queries/sales/monthly-revenue worked\n  ochakai report queries/sales/monthly-revenue failed --note \"joins dropped 2024 rows after schema change\"\n")
	note := fs.String("note", "", "context recorded with the report: what was run, what went wrong (max 2000 bytes)")
	asJSON := fs.Bool("json", false, "print the updated usage totals as JSON")
	id, rest, err := idArgs(fs, args, 2)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	u, err := c.ReportOutcome(ctx, id, rest[0], *note)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(u)
	}
	fmt.Printf("reported %s ochakai://%s (worked %d, failed %d)\n", rest[0], id, u.Worked, u.Failed)
	return nil
}

func cmdRevisions(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"revisions",
		"Usage: ochakai revisions [flags] <id>\n\nList a concept's change history, newest first: who changed it, how,\nand when — the audit surface behind \"every change kept as a\nrevision\". Works for soft-deleted concepts too. The whole concept as it\nstood, as an OKF document, is in the JSON output (--json), so a diff\nbetween two revisions is a text diff.",
		"  ochakai revisions metrics/revenue\n  ochakai revisions queries/sales/monthly-revenue --json | jq -r '.revisions[0].document'\n")
	limit := fs.Int("limit", 0, "max revisions (server default 50, max 200)")
	asJSON := fs.Bool("json", false, "print the raw JSON response (includes each revision's document)")
	id, _, err := idArgs(fs, args, 1)
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
		fmt.Printf("#%d\t%s\t%s\t%s\n", r.Rev, r.Change,
			r.ChangedBy, r.ChangedAt.Format(time.RFC3339))
	}
	return nil
}

// cmdLog prints the generated log.md for a path: the update history in
// the shape OKF SPEC §9 defines, which is a file somebody else can read
// rather than a listing only ochakai knows how to print (design doc
// 0046 §3.8).
func cmdLog(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"log",
		"Usage: ochakai log [flags] [path]\n\nPrint the update history under a path as OKF's log.md (SPEC §9):\ndate-grouped, newest first. With no path, the whole bundle.\n\nIt is generated from the revision ledger, so it says the same thing\n`ochakai revisions` does — in the format a bundle carries, which is\nwhat makes the history portable.",
		"  ochakai log\n  ochakai log metrics\n  ochakai log metrics/revenue --limit 20\n")
	limit := fs.Int("limit", 0, "max concepts (default 1000)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("log takes one path at most")
	}
	var prefix string
	if len(pos) == 1 {
		prefix = pos[0]
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	doc, err := c.Log(ctx, prefix, *limit)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(doc)
	return err
}

func cmdGet(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"get",
		"Usage: ochakai get [flags] <id>\n\nPrint one knowledge concept as an OKF document (YAML frontmatter +\nmarkdown body), and nothing else, so the output round-trips through\n`ochakai put`. Who wrote and confirmed it is an observation rather\nthan part of the document, so it goes to stderr, as attachment metadata\ndoes; --download saves the attachment files themselves (an agent can\nthen read them from disk). --json prints the whole read instead: the\ndocument, the projection under .summary, and the provenance under\n.observed.",
		"  ochakai get metrics/revenue\n  ochakai get queries/sales/monthly-revenue --json | jq -r '.summary.content_hash'\n  ochakai get insights/reading-revenue --download ./img\n")
	asJSON := fs.Bool("json", false, "print the whole read as JSON (document, summary, observed) instead of the document alone")
	download := fs.String("download", "", "save the concept's attachments into this directory")
	id, _, err := idArgs(fs, args, 1)
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
			data, _, err := c.Attachment(ctx, att.Path)
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
	if _, err := os.Stdout.WriteString(k.Document); err != nil {
		return err
	}
	// stdout stays the document and nothing else, so `ochakai get | …
	// | ochakai put` is one pipe. What this instance observed is not
	// part of the document (design docs 0009, 0043 §3.5), so it goes
	// where the attachment hints already go.
	prov := "created by " + k.Observed.CreatedBy.String()
	if lv := k.Observed.LastVerified(); lv != nil {
		prov = fmt.Sprintf("verified by %s on %s; ", lv.By.String(), lv.At.Format("2006-01-02")) + prov
	}
	fmt.Fprintln(os.Stderr, prov)
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
		"attach",
		"Usage: ochakai attach [flags] <id> <file...>\n\nAttach files to a knowledge concept (any file, up to 5 MiB each — the\nmedia type is sniffed from the bytes). A file of the same name is\nreplaced (the change is kept as a revision). Reference the file from the concept's body so its\ncaption is searchable and it survives OKF export/import — the hint\nprinted after attaching shows the canonical relative link. Requires\nthe server to have GCS configured (OCHAKAI_GCS_BUCKET).",
		"  ochakai attach insights/reading-revenue weekly.png\n  ochakai attach tables/orders seeds.txt\n  ochakai attach tables/orders er-diagram.png --name schema.png\n")
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
	// The canonical body link is relative to the concept's own document:
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
		att, err := c.Attach(ctx, id, attName, data)
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
		"detach",
		"Usage: ochakai detach [flags] <id> <name>\n\nRemove an attachment from a knowledge concept (the change is kept as a\nrevision; content-addressed bytes stay referenced by history).",
		"  ochakai detach insights/reading-revenue weekly.png\n")
	id, rest, err := idArgs(fs, args, 2)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	// The file is at <id>/<name> unless the concept's body puts it
	// elsewhere; read the concept and detach whichever one carries the
	// name, so the command means the same thing for both (design doc
	// 0046 §3.3).
	path := id + "/" + rest[0]
	if k, err := c.Get(ctx, id); err == nil {
		for _, a := range k.Attachments {
			if a.Name == rest[0] && a.Path != "" {
				path = a.Path
				break
			}
		}
	}
	if err := c.Detach(ctx, path); err != nil {
		return err
	}
	fmt.Printf("detached %s\n", path)
	return nil
}

// cmdPut writes a concept, whether or not one was there. The wire has
// been create-or-replace since design doc 0043 §3.5 — a PUT states what
// the concept should say, and whether the id was free is a precondition
// rather than a different act — but the CLI kept `create` and `update`,
// so a caller had to answer a question the format does not ask. Two
// commands, one endpoint, and a writer who guessed wrong got 409 or
// clobbered something.
//
// The preconditions are where existence is expressed, as on the wire:
// --only-if-new is If-None-Match "*" (what `create` always sent), and
// --if-match is the version `update` took. Neither is the default,
// because "write this" is what most callers mean.
//
// Named `put` and not `write` or `set`: it is the method it sends, and
// this CLI is a thin client of the REST API (design docs 0004, 0007).
func cmdPut(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"put",
		"Usage: ochakai put [flags] <id>\n\nWrite a knowledge concept from -f or stdin, creating it or replacing\nwhat is there. Input is an OKF document (--- frontmatter with type,\nmarkdown body — the format `ochakai get` prints; title is optional,\nthe id's last segment is the display name when it is absent) or JSON\n(see api/openapi.yaml). The id is the concept's path; pass it as the\nargument (it overrides an id in the input, and OKF documents carry\nnone — the path is the id). New concepts default to draft; provenance\nis recorded from your Google identity. Every change is kept as a\nrevision server-side.\nWith --only-if-new the write lands only if the id is free, and fails\ninstead of replacing. With --if-match it lands only if the concept still\nhas the version you read, and fails instead of overwriting someone\nelse's edit.",
		"  ochakai put runbook/restore -f concept.md\n  ochakai get insights/revenue-seasonality | sed s/40%/45%/ | ochakai put insights/revenue-seasonality-v2 --only-if-new\n")
	file := fs.String("f", "", "input file (default: stdin)")
	onlyIfNew := fs.Bool("only-if-new", false, "write only if the id is free; a taken id fails instead of being replaced")
	ifMatch := fs.String("if-match", "", "write only if the concept still has this `version` — its content hash (`ochakai get <id> --json` prints it as .summary.content_hash; a REST GET returns it as the ETag header); a stale version fails with a conflict instead of overwriting. Verifying or rejecting a concept does not move it: only an edit does")
	asJSON := fs.Bool("json", false, "print the written concept as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		fs.Usage()
		return errReported
	}
	k, claimed, err := readEntry(*file)
	if err != nil {
		return err
	}
	if len(pos) == 1 {
		if k.ID, err = parseRef(pos[0]); err != nil {
			return err
		}
	}
	// An OKF document carries no id, so the argument is the only place most
	// inputs can get one. Saying so here beats the server's `invalid id ""`,
	// which describes the id format and not the missing argument.
	if k.ID == "" {
		return errors.New("no id: pass the concept's path as the argument (e.g. `ochakai put queries/monthly-revenue -f concept.md`)")
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	doc, err := documentOf(k)
	if err != nil {
		return err
	}
	written, created, changed, notes, err := c.Put(ctx, k.ID, doc, *ifMatch, *onlyIfNew)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusPreconditionFailed {
			return fmt.Errorf("conflict: ochakai://%s changed since the version in --if-match — `ochakai get %s` again, redo the edit, and retry with the new content_hash", k.ID, k.ID)
		}
		return err
	}
	reportNotes(okf.NoteStoredClaim(notes, claimed, written.Document))
	if *asJSON {
		return printJSON(written)
	}
	switch {
	case created:
		fmt.Printf("created %s (%s)\n", written.URI(), written.Summary.Status)
	case !changed:
		fmt.Printf("unchanged %s (%s)\n", written.URI(), written.Summary.Status)
	default:
		fmt.Printf("updated %s (%s)\n", written.URI(), written.Summary.Status)
	}
	return nil
}

// documentOf renders what the caller supplied as the OKF document the
// server takes. Input that was already a document round-trips through
// the same renderer the server stores, so what is sent is what will be
// kept; JSON input is a local convenience that ends here (design doc
// 0043 §5 — the server has one write format).
func documentOf(k *domain.Knowledge) ([]byte, error) {
	return okf.Canonical(k)
}

// reportNotes prints what the server read differently than it was
// written. A reinterpretation is never silent (design doc 0036 §3.4),
// and stderr keeps it out of a piped --json body.
func reportNotes(notes []string) {
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}
}

// cmdVerify records a verification. Re-verifying a concept that is already
// verified is the point as much as promoting a draft: it is how a review
// feed empties (design doc 0025 §6), and `update` cannot express it —
// an unchanged payload writes nothing and verified_at is carried over.
func cmdVerify(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"verify",
		"Usage: ochakai verify [flags] <id>\n\nAppend a verification against the concept as it stands: you and the time\nare added to its ledger. The first confirmation and the tenth re-check\nare the same command, and re-checking is what takes a concept out of\nboth review feeds (--sort verified_at, --sort failed).\nIt does not edit the concept: the lifecycle status and the ETag stay put,\nbecause confirming knowledge and publishing it are different acts. Use\n`put` to move a draft to stable.\nVerifying a rejected concept lifts the rejection.",
		"  ochakai verify metrics/revenue\n  ochakai verify metrics/revenue --json | jq -r '.observed.verified[-1].at'\n")
	asJSON := fs.Bool("json", false, "print the verified concept as JSON")
	id, _, err := idArgs(fs, args, 1)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	k, err := c.Verify(ctx, id)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(k)
	}
	by := "?"
	if v := k.Observed.LastVerified(); v != nil {
		by = v.By.String()
	}
	fmt.Printf("verified ochakai://%s by %s\n", k.ID, by)
	return nil
}

// cmdReject records the ruling that stops an agent re-proposing knowledge
// a human already turned down (design doc 0001 §9.1). Like verify it is a
// ruling and not an edit, so it does not touch the document (design doc
// 0043 §3.3) — which is why it is its own command rather than a status.
func cmdReject(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"reject",
		"Usage: ochakai reject [flags] <id>\n\nRecord that a concept was reviewed and not accepted, with the reason.\nRejected concepts are hidden from search unless asked for\n(`search --rejected`), which is how an agent checks whether a proposal\nwas already turned down before making it again.\nIt does not edit the concept: the lifecycle status and the ETag stay put.\nA rejection is this instance's ruling, so an exported bundle carries the\nconcept's real status rather than folding the ruling onto deprecated.\nUse --withdraw to take one back — the wire calls that ruling\n`withdrawn`, and so does the revision it writes.",
		"  ochakai reject metrics/bad-revenue --note \"double-counts refunds; see policies/revenue-recognition\"\n  ochakai reject metrics/bad-revenue --withdraw\n")
	note := fs.String("note", "", "why it was not accepted — the next agent reads this before proposing again")
	withdraw := fs.Bool("withdraw", false, "withdraw the live rejection instead of recording one")
	asJSON := fs.Bool("json", false, "print the concept as JSON")
	id, _, err := idArgs(fs, args, 1)
	if err != nil {
		return err
	}
	if *withdraw && *note != "" {
		return fmt.Errorf("--withdraw takes back a rejection; it takes no --note")
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	var k *domain.View
	if *withdraw {
		k, err = c.WithdrawRejection(ctx, id)
	} else {
		k, err = c.Reject(ctx, id, *note)
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(k)
	}
	if *withdraw {
		fmt.Printf("rejection withdrawn on ochakai://%s\n", k.ID)
		return nil
	}
	fmt.Printf("rejected ochakai://%s by %s\n", k.ID, k.Observed.Rejection.By.String())
	return nil
}

func cmdDelete(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"delete",
		"Usage: ochakai delete [flags] <id>\n\nSoft-delete a knowledge concept (history is retained server-side).",
		"  ochakai delete terms/obsolete-kpi\n")
	id, _, err := idArgs(fs, args, 1)
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

// cmdPurge is the second half of a two-step destruction: delete makes an
// concept invisible and recoverable, purge makes it gone. It exists because
// a soft-deleted concept still owns its id — Create revives one in place,
// but Move cannot, so without purge a renamed-away id would be blocked
// forever (design doc 0021).
func cmdPurge(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"purge",
		"Usage: ochakai purge [flags] <id>\n\nHard-delete an already soft-deleted concept: the concept, its revisions,\nusage, and attachment metadata are erased and the id is freed for a\nmove. History is gone — `ochakai delete` first, then purge. A live\nconcept is refused.",
		"  ochakai delete terms/obsolete-kpi\n  ochakai purge terms/obsolete-kpi\n")
	id, _, err := idArgs(fs, args, 1)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	if err := c.Purge(ctx, id); err != nil {
		return err
	}
	fmt.Printf("purged ochakai://%s\n", id)
	return nil
}

// cmdReembed exists because vectors are written on concept writes only: a
// base imported before semantic search was configured has none, and
// hybrid search stays quietly lexical-only until every concept happens to
// be rewritten.
func cmdReembed(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"reembed",
		"Usage: ochakai reembed [flags]\n\nEmbed concepts that have no vector for the configured model — concepts\nwritten before semantic search was enabled, or before the model was\nchanged. Runs bounded passes until nothing is left, so it can be started\nonce and left alone.",
		"  ochakai reembed\n  ochakai reembed --limit 50   # smaller passes, e.g. behind a short request timeout\n  ochakai reembed --once       # one pass, then report what is left\n")
	limit := fs.Int("limit", 0, "max concepts to embed per pass (server default 200)")
	once := fs.Bool("once", false, "run a single pass instead of continuing until nothing is left")
	asJSON := fs.Bool("json", false, "print the raw JSON response of each pass")
	if _, err := exactArgs(fs, args, 0); err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	// One pass is one HTTP request and one concept is one call to the
	// embedding provider, so a pass has to stay well inside the server's
	// request timeout — which makes "run it until it is done" the client's
	// job. Looping here also means an operator types one command instead
	// of watching a number and deciding when to stop.
	var embedded, attachments, failed int
	cursor := ""
	for pass := 1; ; pass++ {
		res, err := c.Reembed(ctx, cursor, *limit)
		if err != nil {
			return err
		}
		embedded += res.Embedded
		attachments += res.Attachments
		failed += res.Failed
		if *asJSON {
			if err := printJSON(res); err != nil {
				return err
			}
		} else {
			fmt.Printf("pass %d: embedded %d concepts, %d attachments, failed %d, still missing %d\n",
				pass, res.Embedded, res.Attachments, res.Failed, res.Missing)
		}
		if *once || res.Missing == 0 {
			break
		}
		// The cursor, not the progress count, decides when to stop: a
		// pass whose every item the provider refuses still advances, and
		// the corpus beyond that window deserves its turn. An exhausted
		// cursor with work still missing means those failures are all
		// that is left.
		if res.Cursor == "" || res.Cursor == cursor {
			fmt.Fprintf(os.Stderr,
				"stopping: %d items still missing and the corpus is exhausted; see the server log for why they failed\n",
				res.Missing)
			return errReported
		}
		cursor = res.Cursor
	}
	if !*asJSON {
		fmt.Printf("done: embedded %d concepts, %d attachments, failed %d\n", embedded, attachments, failed)
	}
	return nil
}

func cmdMove(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"move",
		"Usage: ochakai move [flags] <id> <new-id>\n\nMove (rename) a knowledge concept to a new id. Revisions, usage, and\nattachments follow, and inbound references (link targets, and\na `model` key where a document carries one) are rewritten so nothing\nbreaks.",
		"  ochakai move insights/revenue-seasonality insights/sales/revenue-seasonality\n")
	id, rest, err := idArgs(fs, args, 2)
	if err != nil {
		return err
	}
	newID, err := parseRef(rest[0])
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

func cmdExport(ctx context.Context, args []string) error {
	fs, url := newFlagSet(
		"export",
		"Usage: ochakai export [flags] <dir | ->\n\nDownload the whole knowledge base as an OKF bundle (markdown + YAML\nfrontmatter) into dir, or stream the tar.gz to stdout with \"-\".\nYour knowledge is yours.",
		"  ochakai export ./knowledge\n  ochakai export - > ochakai-okf.tar.gz\n  ochakai export --no-attachments - > concepts.tar.gz   # bytes are in GCS; copy them from there\n")
	noAtt := fs.Bool("no-attachments", false, "export the markdown only, skipping attachment files")
	pos, err := exactArgs(fs, args, 1)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	rc, err := c.Export(ctx, !*noAtt)
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
		"import",
		"Usage: ochakai import [flags] <dir | file.tar.gz | ->\n\nImport an OKF bundle (a directory of markdown + YAML frontmatter, or\na tar.gz of one; \"-\" reads the tar.gz from stdin). The inverse of\n`ochakai export`: each path names its concept (the path minus .md is\nthe id), the frontmatter type key names the type (required — a\nmarkdown file without one is not a concept, and is kept as a file),\nreserved index.md / log.md files are skipped, keys the format does\nnot define are kept as written, and existing concepts are replaced (kept as revisions; concepts identical\nto what is stored are left untouched and reported as unchanged;\nconcepts the server rejects as invalid — e.g. one whose type is not a\nsingle line — are skipped and reported).\nFiles referenced by a concept's body markdown links become its\nattachments, wherever they sit in the bundle (their location is\npreserved for re-export); unreferenced data files inside a concept's\ndirectory (<id>/<name>) attach to that concept. Everything else the\nbundle carried is written at the path it arrived at — what enters\nleaves, so nothing is dropped for belonging to no concept. The packed shape is\nthe structure: an archive wrapped in a single directory imports\nunder that directory — the bundle keeps its own namespace. Works\nwith any OKF bundle, not just ochakai's own.\nA file that cannot be stored at all — empty, oversized, or at a path\nochakai cannot address — is skipped; a value read differently than\nit was written is a note and the concept still imports. A document\nthat says who generated or confirmed it is one of those: the keys\nare kept as the document's own claim, under `received`, and never\nbecome this instance's provenance — so a bundle from another\ninstance imports with a note per concept, while one exported from\nhere imports silently. Both are\nreported and neither fails the command, because a consumer takes the\ndocument rather than rejecting it. --strict is the opposite posture,\nfor a sync nobody watches: a bundle that is not read exactly as\nwritten fails, and the counts land in the summary line either way.",
		"  ochakai import ./knowledge\n  ochakai import ga4-bundle.tar.gz --dry-run\n  ochakai import ./knowledge --dry-run --strict   # gate a CI sync on a clean parse\n  ochakai export - | OCHAKAI_URL=https://other ochakai import -\n")
	dryRun := fs.Bool("dry-run", false, "parse and list what would be written, write nothing")
	strict := fs.Bool("strict", false, "refuse a bundle that is not read exactly as written: any note or skip fails the command instead of being reported. Parse-time ones are found before anything is written, so a strict import either lands whole or writes nothing")
	pos, err := exactArgs(fs, args, 1)
	if err != nil {
		return err
	}
	files, err := readBundle(pos[0])
	if err != nil {
		return err
	}
	entries, atts, loose, skipped, notes := okf.FromBundle(files)
	for _, s := range skipped {
		fmt.Fprintln(os.Stderr, "skip:", s)
	}
	// Notes are not skips: the concept is imported, with one field read
	// differently than it was written. Reporting them keeps a foreign
	// bundle's reinterpreted status or dropped stale_after visible without
	// pretending the document was rejected (OKF SPEC §11).
	noted := len(notes)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}
	// Everything the parser reinterpreted is known before the first write,
	// so --strict refuses here and the knowledge base is left untouched.
	// The alternative — writing 65 concepts and failing on the 66th — is
	// the half-applied sync this flag exists to prevent.
	if *strict && (noted > 0 || len(skipped) > 0) {
		return strictErr(noted, len(skipped), "nothing was written")
	}
	if *dryRun {
		for i := range entries {
			fmt.Printf("would import %s\n", entries[i].URI())
		}
		for _, a := range atts {
			fmt.Printf("would attach %s/%s (from %s)\n", a.ID, a.Name, a.Path)
		}
		for _, f := range loose {
			fmt.Printf("would write %s\n", f.Path)
		}
		fmt.Printf("dry run: %d concepts, %d attachments, %d files, %d skipped, %d notes\n",
			len(entries), len(atts), len(loose), len(skipped), noted)
		return nil
	}
	c, err := newClient(ctx, *url)
	if err != nil {
		return err
	}
	// A 400 is the server's judgment on one document (e.g. a models concept
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
	// A document that carried a verified key is confirmed by whoever ran
	// the import — the actor who put it through the review gate (design
	// docs 0009 §3.2, 0043 §3.2). The values inside the key never become
	// this instance's ledger: what the document said stays a claim beside
	// it (design doc 0046 §2.2), and the verification recorded here is
	// the importer's own.
	confirm := func(d *okf.Doc) {
		if !d.Verified {
			return
		}
		if _, err := c.Verify(ctx, d.ID); err != nil {
			noted++
			fmt.Fprintf(os.Stderr, "note: %s imported, but recording its verification failed: %v\n", d.ID, err)
		}
	}
	for i := range entries {
		d := &entries[i]
		k := &d.Knowledge
		doc, err := documentOf(k)
		if err != nil {
			skipEntry(k, err)
			continue
		}
		// One call per document, with no precondition: import deliberately
		// replaces whatever is stored (the bundle is the source of truth
		// for this loop), and whether the id was free is not something the
		// bundle says anything about.
		stored, wasCreated, changed, notes, err := c.Put(ctx, k.ID, doc, "", false)
		if err != nil {
			if isInvalid(err) {
				skipEntry(k, err)
				continue
			}
			return fmt.Errorf("%s: %w", k.URI(), err)
		}
		// The parse above already moved the document's own trust family
		// under `received`, so the server never saw the keys and cannot
		// report them. What it can answer is whether the claim stayed —
		// this instance's own export form coming home is dropped instead
		// (design doc 0046 §2.2), which is what keeps the Git review loop
		// note-free.
		notes = okf.NoteStoredClaim(notes, d.Claimed, stored.Document)
		noted += len(notes)
		reportNotes(notes)
		if wasCreated {
			created++
			confirm(d)
			fmt.Printf("created %s\n", k.URI())
			continue
		}
		if !changed {
			unchanged++
			fmt.Printf("unchanged %s\n", k.URI())
			continue
		}
		updated++
		confirm(d)
		fmt.Printf("updated %s\n", k.URI())
	}
	attached := 0
	for _, a := range atts {
		if rejected[a.ID] {
			skipped = append(skipped, a.Path+": its concept "+a.ID+" was not imported")
			fmt.Fprintln(os.Stderr, "skip:", skipped[len(skipped)-1])
			continue
		}
		// At the path the bundle carried it at, which is the whole of
		// what preserving a foreign location means now (design doc 0046
		// §3.3): the concept claims it because its body points there.
		if err := c.PutBundleFile(ctx, a.Path, a.Data); err != nil {
			return fmt.Errorf("write %s: %w", a.Path, err)
		}
		attached++
		fmt.Printf("attached %s (%s)\n", a.Path, a.ID)
	}
	// The files that belong to no concept, at the paths they arrived at.
	// A bundle carries more than its concepts, and what enters it leaves
	// it (design doc 0046 §3.2): a producer's seed data in a shared
	// directory, a diagram nothing links yet, a markdown file with no
	// type. Whether a concept ever claims one is a question that concept's
	// body answers (§3.3), so nothing is attributed here.
	var written int
	for _, f := range loose {
		if err := c.PutBundleFile(ctx, f.Path, f.Data); err != nil {
			if !isInvalid(err) {
				return fmt.Errorf("write %s: %w", f.Path, err)
			}
			skipped = append(skipped, f.Path+": rejected by the server: "+err.Error())
			fmt.Fprintln(os.Stderr, "skip:", skipped[len(skipped)-1])
			continue
		}
		written++
		fmt.Printf("wrote %s\n", f.Path)
	}
	fmt.Printf("imported %d concepts (%d created, %d updated, %d unchanged, %d attachments, %d files, %d skipped, %d notes)\n",
		created+updated+unchanged, created, updated, unchanged, attached, written, len(skipped), noted)
	// The parse-time gate above cannot see what the server read differently
	// or refused, so --strict asks again with the writes already done. The
	// summary is printed first: the exit code says the sync is not clean,
	// and the line above it says how far it got.
	if *strict && (noted > 0 || len(skipped) > 0) {
		return strictErr(noted, len(skipped), "the concepts above are already written")
	}
	return nil
}

// strictErr is what --strict fails with. It counts rather than repeats:
// every note and skip has already been printed to stderr as it happened,
// and the error's job is to name the exit code's reason and say what the
// knowledge base looks like now.
func strictErr(notes, skipped int, state string) error {
	return fmt.Errorf("--strict: the bundle was not read exactly as written (%d %s, %d %s); %s",
		notes, plural(notes, "note"), skipped, plural(skipped, "skipped file"), state)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
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

// readBundleDir walks root, skipping dot-concepts (.git and friends —
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
		if errors.Is(err, io.EOF) {
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

// readEntry reads a knowledge concept from path ("" or "-" = stdin) in
// either of the interchange formats from design doc 0004 §5: an OKF
// document (leading ---) or JSON.
//
// claimed names the server-owned keys the document carried, which the
// parse kept as its own claim rather than as provenance (design doc 0046
// §2.2). Whether the claim survives the write is the server's answer, so
// it travels to the call site rather than being reported here.
func readEntry(path string) (k *domain.Knowledge, claimed []string, err error) {
	var data []byte
	if path == "" || path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, nil, err
	}
	return decodeEntry(data)
}

func decodeEntry(data []byte) (*domain.Knowledge, []string, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("---")) {
		d, notes, err := okf.Parse(trimmed)
		for _, n := range notes {
			fmt.Fprintln(os.Stderr, "note:", n)
		}
		if err != nil {
			return nil, nil, err
		}
		// A verified key on a single-document write is not acted on: this
		// is an edit, and confirming a concept is a separate call
		// (ochakai verify). Only bundle import turns the key into a
		// verification, because that is the review gate (design doc 0043
		// §3.2).
		return &d.Knowledge, d.Claimed, nil
	}
	var k domain.Knowledge
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, nil, fmt.Errorf("input is neither an OKF document nor valid JSON: %w", err)
	}
	return &k, nil, nil
}

// extractTarGz unpacks the OKF bundle under dir, refusing concepts that
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
		if errors.Is(err, io.EOF) {
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
