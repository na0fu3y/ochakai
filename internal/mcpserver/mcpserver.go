// Package mcpserver exposes ochakai's MCP tools over streamable HTTP.
// Tool names follow verb_object so they stay unambiguous next to other MCP
// servers' tools (design doc §4). The REST API (internal/restapi) is a
// superset of these tools: the same operations plus the archive of a
// bundle path, the derived index.md and log.md, the reverse lookup
// (search's links_to=), the human's ruling, deletion, the usage totals and
// file writes (design doc 0067 §5.1 keeps those off MCP — agents use
// search/get_context; tool schemas cost agent context, so the tool count
// is a budget, and design doc 0076 returned two of it).
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

const (
	serverName = "ochakai"
	uriScheme  = "ochakai://"
)

// Handler returns the streamable HTTP handler serving the MCP endpoint.
func Handler(svc *service.Service, version string) http.Handler {
	server := newServer(svc, version, RetiredToolNames)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}

// extraProvider is the part of an MCP request the actor comes from. Tool
// calls and resource reads carry it alike, and both record provenance, so
// neither is a special case.
//
// The session comes along for the producer: MCP already asks every client
// to name itself and its version at initialize, which is SPEC §7's
// "<producer>/<version>" arriving without anyone having to add a header
// (design doc 0052 §3.3).
type extraProvider interface {
	GetExtra() *mcp.RequestExtra
	GetSession() mcp.Session
}

// sessionProducer is the initialize-time clientInfo as a producer string,
// or "" when the client named nothing usable.
//
// It is a fallback, not an override: a caller that sent Ochakai-Producer
// said what it is on this call, while clientInfo says what opened the
// session — and a host that proxies several agents through one session
// distinguishes them only by the header. Silently preferring the session's
// name would be the misattribution 0027 §5.2 refuses, one level down.
//
// A clientInfo that does not fit SPEC §7's form is dropped rather than
// refused. The client did not claim to be sending provenance — the
// protocol asked it for a name — so failing its tool call over a version
// string ochakai cannot spell would turn a courtesy into an outage.
func sessionProducer(req extraProvider) string {
	ss, ok := req.GetSession().(*mcp.ServerSession)
	if !ok || ss == nil {
		return ""
	}
	params := ss.InitializeParams()
	if params == nil || params.ClientInfo == nil {
		return ""
	}
	p := params.ClientInfo.Name + "/" + params.ClientInfo.Version
	if !domain.ValidProducer(p) {
		return ""
	}
	return p
}

// requestActor resolves the actor to record for a tool call from the
// HTTP headers that carried the call. httpauth.Middleware puts the
// actor on the request context, but a streamable session is connected
// once — at initialize — and every later call runs on a context derived
// from that first request, so the context value is pinned to whoever
// opened the session. A delegating host (design doc 0027) that reuses
// one session across end users would then have every write silently
// misattributed to the first user — the exact failure 0027 §5.2
// refuses. The middleware has already vetted these headers (a rejected
// delegation never reaches a handler), so resolution here cannot newly
// fail; if it somehow does, refuse the write rather than misattribute
// it. In-process transports (tests, embedding without HTTP) carry no
// request headers and fall back to the context actor.
func requestActor(ctx context.Context, cfg *config.Config, req extraProvider) (domain.Actor, error) {
	actor := httpauth.Actor(ctx)
	if extra := req.GetExtra(); cfg != nil && extra != nil && extra.Header != nil {
		resolved, _, err := httpauth.ActorFromHeader(cfg, extra.Header)
		if err != nil {
			return domain.Actor{}, fmt.Errorf("resolving actor: %w", err)
		}
		actor = resolved
	}
	if actor.Producer == "" {
		actor.Producer = sessionProducer(req)
	}
	return actor, nil
}

// requestCtx resolves the per-call actor and puts it on the context the
// service runs under. Not every provenance path takes an actor argument:
// report_outcome's reporter and the usage events the read tools record
// are read from the context by the service, and that context is the
// session's, so they carried the session opener until this call replaced
// it. Every tool handler goes through here rather than only the ones that
// record something today — the failure is silent misattribution, so a
// service change that starts recording on a path this skipped would
// regress without a symptom.
func requestCtx(ctx context.Context, cfg *config.Config, req extraProvider) (context.Context, domain.Actor, error) {
	actor, err := requestActor(ctx, cfg, req)
	if err != nil {
		return nil, domain.Actor{}, err
	}
	return httpauth.WithActor(ctx, actor), actor, nil
}

// tool adapts a handler to the shape mcp.AddTool takes, running requestCtx
// first so the context the handler receives already carries the per-call
// actor. Registering every tool through here is what makes the "handler
// forgot requestCtx" failure impossible instead of merely convention.
func tool[In, Out any](svc *service.Service,
	fn func(ctx context.Context, actor domain.Actor, in In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		ctx, actor, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return fn(ctx, actor, in)
	}
}

// instructions is what a client holds for the whole conversation,
// beside the tool list. It is counted with the tools rather than treated
// as free (TestMCPStaysUnderItsResidentByteCap).
const instructions = "ochakai serves human-curated knowledge for data work: metric definitions, " +
	"attested computations (sanctioned SQL and other computations), interpretation " +
	"knowledge (how to read a metric), glossary terms, dataset and table catalog " +
	"entries, and references. Those types are recommendations; any single-line string " +
	"is a type. It runs no SQL and no LLM.\n" +
	"Before answering a data question, call get_context once — it returns the relevant " +
	"concepts in full, links expanded.\n" +
	"A concept's id is its path (metrics/revenue, ga4/tables/orders). Place together " +
	"what should be read together; the type is metadata, not a location.\n" +
	"Judge trust from provenance, not from status: status is the lifecycle (draft, " +
	"stable, deprecated) and whether anyone confirmed a concept is recorded separately, " +
	"by whoever confirmed it. A passed stale_after means due for re-checking, not wrong.\n" +
	"After acting on knowledge, call report_outcome. Write learnings back with " +
	"put_concept as drafts for a human to confirm. Knowledge is co-owned by humans and agents."

func newServer(svc *service.Service, version string, retired []RetiredToolName) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	// A name this release renamed still answers, for this release only
	// (design doc 0088). It goes on before any tool is registered because
	// it decides which registration a call reaches.
	s.AddReceivingMiddleware(answerForRetiredNames(retired))

	// Expose concepts as MCP resources so clients can @-mention them by their
	// canonical ochakai:// URI. Only the template is advertised — enumerating
	// every concept in resources/list would flood the client, so discovery stays
	// with search_concepts/get_context and the URI is the addressing scheme.
	// {+id} (RFC 6570 reserved expansion) lets the slash-separated id match;
	// a plain {id} would stop at the first slash.
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:     "concept",
		Title:    "Concept",
		MIMEType: "text/markdown",
		Description: "A single knowledge concept as an OKF document: YAML frontmatter (title, " +
			"status, provenance, type-specific attrs) followed by the markdown body and its " +
			"links. Address by canonical URI — the scheme plus the concept's id (its path), " +
			"e.g. ochakai://metrics/revenue or ochakai://queries/sales/top-customers. Discover " +
			"URIs with search_concepts or get_context; get_concept returns the same concept " +
			"as JSON.",
		URITemplate: "ochakai://{+id}",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, ok := parseKnowledgeURI(req.Params.URI)
		if !ok {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, err
		}
		k, err := svc.Get(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return nil, err
		}
		doc, err := okf.Document(k)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     string(doc),
			}},
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_concepts",
		Annotations: readOnly,
		Description: "Search the knowledge base; verified concepts rank higher. Filenames and file " +
			"contents count too, and a hit is always the owning concept. Rejected concepts are " +
			"excluded unless you pass rejected=true — ask for them to check whether a proposal " +
			"was already turned down.\n" +
			"Ranking, so it ends at limit and has no page two: a search that needs more is one " +
			"that needs narrowing. To walk a whole feed instead, use list_concepts.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		page, err := svc.SearchOrList(ctx, in.Query, "", "", in.filter(), in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: page.Hits, Cursor: page.Cursor}, nil
	}))

	// The other half of the same wire operation, as its own tool: a
	// listing is not a search (design doc 0068 §2), and one schema could
	// not say so — query was required unless sort was set, cursor was
	// refused unless it was, and limit meant two different things either
	// way. None of that is expressible in JSON Schema, so it lived in
	// prose the agent had to obey unaided.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_concepts",
		Annotations: readOnly,
		Description: "Walk a review feed in full, oldest work first. Scores nothing and ranks " +
			"nothing: pass the cursor from the last page to continue, and no cursor back means " +
			"the end.\n" +
			"- verified_at: by verification age, oldest first — stale verified knowledge.\n" +
			"- usage: by how often the concept was read — the draft review feed.\n" +
			"- failed: reported wrong via report_outcome, worst first — the re-verification feed.\n" +
			"- stale_after: past the author's declared expiry, most overdue first — verifying " +
			"alone does not clear this feed, because the date is the writer's declaration and " +
			"only an edit changes it.\n" +
			"Omit sort with source set to list what derives from that material instead.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in listIn) (*mcp.CallToolResult, searchOut, error) {
		f := in.filter()
		if in.Sort == "" && f.Source == "" {
			return nil, searchOut{}, service.Invalidf(
				"list_concepts needs sort (%s) or source; to search by text, use search_concepts",
				strings.Join(domain.ListSorts, ", "))
		}
		page, err := svc.SearchOrList(ctx, "", in.Sort, in.Cursor, f, in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: page.Hits, Cursor: page.Cursor}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_context",
		Annotations: readOnly,
		Description: "The one call to make before answering a data question: searches (verified " +
			"concepts rank higher), returns the full concepts behind the top hits, and expands " +
			"one hop through links so the insight explaining a metric and the computation " +
			"answering the question arrive together. Prefer it over search+get chains; fall " +
			"back to search_concepts/get_concept for precise lookups. \"concepts\" is the " +
			"knowledge, \"hits\" only the ranking behind it; concepts past the byte budget are " +
			"named under \"outline\", fetchable by id with get_concept.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in contextIn) (*mcp.CallToolResult, contextOut, error) {
		budget := in.Budget
		if budget <= 0 {
			budget = defaultContextBudget
		}
		res, err := svc.Context(ctx, service.ContextRequest{
			Query: in.Query,
			Filter: store.Filter{
				Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses), Tags: in.Tags,
				Prefixes: in.Prefixes, Trust: domain.ToTrusts(in.Trusts),
			},
			Limit: in.Limit, Budget: budget,
		})
		if err != nil {
			return nil, contextOut{}, err
		}
		return nil, contextOut{
			Hits: res.Hits, Concepts: res.Concepts, Outline: res.Outline,
			Hint: contextHint(res.Outline),
		}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_concept",
		Annotations: readOnly,
		Description: "Get one concept by id: its full markdown body, structured attrs, links, and " +
			"the files its body references (fetch their bytes with get_file).",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in getIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Get(ctx, in.ID)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		v, err := okf.ViewOf(k)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: v}, nil
	}))

	// put_concept is the one write face (design doc 0046 §3.14). It was
	// create_knowledge and update_knowledge, which asked the agent a
	// question the document does not answer: a write states what the
	// concept should say, and whether an id was already taken is not part
	// of that statement (0043 §3.5). Two tools also cost more of the
	// agent's context than the three this surface was going to drop put
	// together — the merge is where the saving actually was.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "put_concept",
		Annotations: nonDestructive,
		Description: "Write a concept: created if the id is free, replaced if it is taken.\n" +
			"The document you send is the whole concept — a key you leave out is cleared, so on a " +
			"replace, get_concept first and send back what you are not changing. Every change is " +
			"kept as a revision; an identical write does nothing.\n" +
			"Write status: draft unless you are recording something already agreed. You are " +
			"recorded as the author and the concept stays unverified until a person confirms it; " +
			"confirming and rejecting are not on this surface.\n" +
			"A concept a human has ruled on — verified, rejected, deprecated — cannot be " +
			"replaced here: if a verified concept is wrong, report_outcome failed, otherwise put " +
			"a better draft at a different id, linking the concept it would replace from its " +
			"body so the reviewer sees both, and let a human promote it. An id whose concept " +
			"was deleted can be reused, which revives it as your draft — unless a human had " +
			"ruled on it (verified, rejected, or deprecated), and then this surface refuses too. " +
			"Search rejected=true first so you do not re-propose what was already turned down.\n" +
			"Links are never a field: a markdown link to another concept's path in body — " +
			"[revenue](/metrics/revenue.md) — becomes a link both ways.\n" +
			"Pick the type by what the concept holds; any other single-line type works too:\n" + domain.TypesGuide(),
	}, tool(svc, func(ctx context.Context, actor domain.Actor, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		write, notes, claimed, err := in.toKnowledge()
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		// Whether the id is taken decides which guard applies, not which
		// tool the agent should have called. Creating on a soft-deleted
		// id revives it, which is the one way past the curation guards,
		// so the create path has its own check; replacing a live concept
		// is refused for a curated one and otherwise carries the version
		// read by the guard as its precondition, since this surface has
		// no If-Match channel of its own.
		version, err := svc.RefuseIfCurated(ctx, in.ID, "replace")
		if errors.Is(err, store.ErrNotFound) {
			k, err := svc.CreateKeepingCurated(ctx, write, actor)
			if err != nil {
				return nil, knowledgeOut{}, err
			}
			out, err := view(k, okf.NoteClaim(notes, claimed, k))
			return nil, out, err
		}
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		k, _, err := svc.Update(ctx, write, actor, version)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		out, err := view(k, okf.NoteClaim(notes, claimed, k))
		return nil, out, err
	}))

	// delete_concept is not a tool (design doc 0076). Deleting knowledge is
	// a ruling, and rulings stay off this surface: MCP withholds the
	// reversible ones (verify, reject — POST /api/v1/review/{id} is not a
	// tool) so it cannot offer the destructive one. An agent that wrote a
	// bad draft leaves it for a human to rule on. REST, the CLI
	// (ochakai delete) and the web UI keep the capability.

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_file",
		Annotations: readOnly,
		Description: "Fetch one file of the bundle by its path (get_concept lists a concept's files " +
			"under \"files\": images, PDFs, plain-text data). Files are context-heavy — fetch one " +
			"deliberately, when the body references something you need to see. ochakai never " +
			"interprets files; if you learn something from one, write it into the concept's body " +
			"with put_concept so the knowledge becomes searchable text.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in fileIn) (*mcp.CallToolResult, fileOut, error) {
		att, data, err := svc.GetFile(ctx, in.Path)
		if err != nil {
			return nil, fileOut{}, err
		}
		// The content block matches the media type (design doc 0013):
		// images as image content, plain text inline, PDFs as an embedded
		// blob resource.
		var content mcp.Content
		switch {
		case strings.HasPrefix(att.MediaType, "image/"):
			content = &mcp.ImageContent{Data: data, MIMEType: att.MediaType}
		case att.MediaType == "text/plain":
			content = &mcp.TextContent{Text: string(data)}
		default:
			content = &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
				URI:      "ochakai://" + att.Path,
				MIMEType: att.MediaType,
				Blob:     data,
			}}
		}
		res := &mcp.CallToolResult{Content: []mcp.Content{content}}
		return res, fileOut{File: *att}, nil
	}))

	// get_concept_usage is not a tool (design doc 0076). Usage totals are
	// the human half of the loop — a curator reads them to promote a draft
	// or retire a concept nobody uses. What an agent needs in order to
	// decide whether to rely on a concept is its trust tier and
	// verified_at, and both ride on every search hit and every
	// get_concept. The agent keeps report_outcome, the edge of the loop it
	// owns; the totals stay on REST, the CLI and the web UI.

	mcp.AddTool(s, &mcp.Tool{
		Name:        "report_outcome",
		Annotations: nonDestructive,
		Description: "Report whether knowledge you acted on actually worked — the last edge of the " +
			"write-back loop. After running an attested computation, or SQL you wrote from a " +
			"metric definition, report worked, or failed with what went wrong in note: failed " +
			"reports against verified concepts flag them for re-verification. Returns the " +
			"concept's updated usage totals.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in outcomeIn) (*mcp.CallToolResult, usageOut, error) {
		id := strings.TrimPrefix(in.Target, uriScheme)
		if !domain.ValidID(id) {
			return nil, usageOut{}, fmt.Errorf("invalid target %q (want the concept's id, e.g. queries/monthly-revenue)", in.Target)
		}
		u, err := svc.ReportOutcome(ctx, id, in.Outcome, in.Note)
		if err != nil {
			return nil, usageOut{}, err
		}
		return nil, usageOut{Usage: *u}, nil
	}))

	// A read-only deployment does not offer the write tools at all
	// (design doc 0040 §2.3). Offering one that always fails would spend
	// the agent's context on a schema and then refuse the call — the
	// opposite of what the tool budget in design doc 0015 §3.1 is for.
	// MCP already has a way to say what a server can do, so read-only is
	// said in that vocabulary rather than announced separately.
	if svc.Config != nil && svc.Config.ReadOnly {
		s.RemoveTools(writeTools...)
	}

	return s
}

// writeTools are the tools that change knowledge, removed on a read-only
// deployment. The service refuses these operations anyway (that is the
// guarantee); removing them here is what stops an agent from wasting a
// turn discovering it.
var writeTools = []string{"put_concept", "report_outcome"}

// Tool annotations let clients apply auto-approval policies without reading
// prose. readOnlyHint here describes the knowledge domain: search/get
// never change a concept (they may bump usage counters — telemetry the hint
// deliberately ignores). Writes are non-destructive because history is kept
// as revisions, and no tool on this surface is destructive — the one that
// was, delete_concept, left with design doc 0076. The values are immutable
// and shared across tools.
var (
	readOnly       = &mcp.ToolAnnotations{ReadOnlyHint: true}
	nonDestructive = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}
)

func boolPtr(b bool) *bool { return &b }

// parseKnowledgeURI extracts the concept id from a canonical knowledge URI
// (ochakai://<id>). ok is false when the URI lacks the scheme or what
// follows is not a valid id (empty, or with an empty/invalid segment) —
// malformed URIs are rejected before any store lookup.
func parseKnowledgeURI(uri string) (id string, ok bool) {
	id, found := strings.CutPrefix(uri, uriScheme)
	if !found || !domain.ValidID(id) {
		return "", false
	}
	return id, true
}

// The jsonschema tags spell out the numeric contracts (defaults, maxima,
// out-of-range fallback) that api/openapi.yaml and the CLI help already
// document — MCP agents only see the tool schema.
// filters are the six every read tool takes, in one embedded struct
// rather than repeated per tool: they are the same question everywhere,
// and a schema written twice is a schema that starts differing.
type filters struct {
	Types    []string `json:"types,omitempty" jsonschema:"filter by type, case-insensitive: Metric, Attested Computation, Skill, Insight, Policy, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by lifecycle status: draft, stable, deprecated — confirmation is a separate question, ask it with trusts"`
	Trusts   []string `json:"trusts,omitempty" jsonschema:"filter by who confirmed it: unverified, machine-confirmed, human-reviewed (OKF SPEC §5.3); several are OR-ed, omit to not ask. Independent of status"`
	Rejected *bool    `json:"rejected,omitempty" jsonschema:"true to ask only for concepts a human turned down — how you check whether a proposal was already rejected. Omit and they stay out"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Source   string   `json:"source,omitempty" jsonschema:"only concepts citing this resource, matched exactly against sources[].resource — \"this material changed, what derives from it?\""`
	Prefixes []string `json:"prefixes,omitempty" jsonschema:"only concepts under these paths, e.g. [\"teams/growth\", \"company\"] — an id is a path, so this scopes to a subtree (\"metrics\" covers metrics/ but not metrics-legacy/); several are OR-ed"`
}

func (f filters) filter() store.Filter {
	return store.Filter{
		Types: domain.ToTypes(f.Types), Statuses: domain.ToStatuses(f.Statuses),
		Tags: f.Tags, Source: f.Source, Prefixes: f.Prefixes,
		Trust: domain.ToTrusts(f.Trusts), Rejected: f.Rejected,
	}
}

type searchIn struct {
	Query string `json:"query" jsonschema:"the text to search for"`
	filters
	Limit int `json:"limit,omitempty" jsonschema:"max results: default 10, max 50 (out-of-range falls back to the default)"`
}

// listIn is the same filters with a feed and a position instead of a
// query. sort is not required in the schema, because a source lookup is
// a listing with no feed to name; the handler says so when neither is
// there.
type listIn struct {
	Sort string `json:"sort,omitempty" jsonschema:"which feed: verified_at | usage | failed | stale_after (the description says what each is)"`
	filters
	Limit  int    `json:"limit,omitempty" jsonschema:"max results per page: default 100, max 1000 (out-of-range falls back to the default)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"resume: pass back the cursor the last page returned, with the same sort and filters"`
}

type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
	// Cursor is present only when a listing has more behind this page;
	// its absence is the end (design doc 0050 §2.3 — no total is given).
	Cursor string `json:"cursor,omitempty"`
}

// contextIn omits fm: a frontmatter key filter is the vocabulary of
// somebody who already knows which key they mean, and the schema that
// teaches an agent the nine askable spellings costs more context than
// the filter has ever saved (design doc 0058 §2.2). It stays on REST and
// the CLI, where the caller is a program somebody wrote or a person at a
// shell. What bounds a response for an agent is budget.
//
// min_score is not omitted here — it no longer exists anywhere (0058
// §2.1).
type contextIn struct {
	Query    string   `json:"query" jsonschema:"the data question to gather context for"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type, case-insensitive: Metric, Attested Computation, Skill, Insight, Policy, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by lifecycle status: draft, stable, deprecated — confirmation is a separate question, ask it with trusts"`
	Trusts   []string `json:"trusts,omitempty" jsonschema:"filter by who confirmed it: unverified, machine-confirmed, human-reviewed; several are OR-ed, omit to not ask. Independent of status"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Prefixes []string `json:"prefixes,omitempty" jsonschema:"only concepts under these paths, e.g. [\"teams/growth\", \"company\"] — an id is a path, so this scopes to a subtree; several are OR-ed. It scopes the search, not the link expansion: a concept in scope still arrives with the term it cites outside"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max primary concepts: default 5, max 20 (out-of-range falls back to the default); linked companions share a 2x total cap"`
	Budget   int      `json:"budget,omitempty" jsonschema:"max bytes of the whole response — hits, outline and concepts together (default 12000). The ranking and the outline always come back, so what this buys is whole concepts. Raise it when you need them, lower it when context is tight"`
}

type contextOut struct {
	Hits     []domain.ContextRank    `json:"hits"`
	Concepts []domain.View           `json:"concepts"`
	Outline  []domain.ContextOutline `json:"outline,omitempty"`
	Hint     string                  `json:"hint"`
}

// defaultContextBudget bounds an unparameterized get_context. It is on by
// default because the callers that most need it — hosts that embed ochakai
// and never touch the tool arguments — are exactly the ones that will not
// set it. Roughly 3000 tokens of concepts, English or Japanese.
const defaultContextBudget = 12000

// contextHint rides along with every context pack. Claude Code sites can
// install the write-back habit with a Stop hook; a browser-based host has
// no shell to hang one on, so the server supplies it — same words for
// every host, no LLM involved (the MCP server's Instructions field carries
// the same habit, but hosts are free to drop instructions and many do).
func contextHint(outline []domain.ContextOutline) string {
	hint := "After acting on this knowledge, call report_outcome (worked/failed) on the concepts you " +
		"used — failed reports are how stale verified knowledge gets caught. If this session " +
		"produced reusable knowledge, write it back with put_concept as a draft; search " +
		"rejected=true first so you do not re-propose something already turned down."
	if len(outline) == 0 {
		return hint
	}
	hint += fmt.Sprintf(" %d concepts did not fit the budget and are listed under \"outline\" — "+
		"fetch any of them by id with get_concept.", len(outline))
	// Said here rather than in the tool schema, because it is true of one
	// response rather than of every one: the schema is resident in the
	// agent's context for the whole conversation, this sentence is not.
	if outline[0].Excerpt != "" {
		hint += " The first of them was your best match and was too large for any budget," +
			" so its row carries an \"excerpt\" of its opening — read that before deciding" +
			" whether to fetch the rest."
	}
	return hint
}

type getIn struct {
	ID string `json:"id" jsonschema:"the concept's id — its path: slug segments separated by / (e.g. metrics/revenue, ga4/tables/orders)"`
}

type knowledgeOut struct {
	Knowledge domain.View `json:"knowledge"`
	// Notes carry what the server read differently than the document
	// wrote it — a reinterpretation is never silent (design doc 0036
	// §3.4), and an agent that gets one back can fix the document rather
	// than discover the change on the next read.
	Notes []string `json:"notes,omitempty"`
}

type fileIn struct {
	Path string `json:"path" jsonschema:"the file's bundle path, from the path field of a concept's files metadata (e.g. metrics/revenue/chart.png)"`
}

type fileOut struct {
	File domain.File `json:"file"`
}

type usageOut struct {
	Usage domain.Usage `json:"usage"`
}

type outcomeIn struct {
	Target  string `json:"target" jsonschema:"the concept's id (e.g. queries/monthly-revenue; an ochakai:// prefix is tolerated)"`
	Outcome string `json:"outcome" jsonschema:"worked = acting on it gave a correct result; failed = a wrong or unusable one"`
	Note    string `json:"note,omitempty" jsonschema:"what was run, what went wrong (max 2000 bytes)"`
}

// writeIn is an id and a document. It was a field-by-field mirror of the
// envelope until 0.16 — a fourth place the field list was written out,
// which had to be found and extended every time OKF gained a key, and
// which an agent that trusted the enumeration could use to silently drop
// whatever the list had fallen behind on. A document has no list to fall
// behind (design doc 0043 §3.11).
//
// It is also the shape an LLM handles best: frontmatter and markdown is
// what the concepts it reads look like, so editing one means returning it
// changed rather than translating it into a schema and back.
// view renders one concept as a tool's answer, with any notes the parse
// produced. Every write path ends the same way, so it is written once.
func view(k *domain.Knowledge, notes []string) (knowledgeOut, error) {
	v, err := okf.ViewOf(k)
	if err != nil {
		return knowledgeOut{}, err
	}
	return knowledgeOut{Knowledge: v, Notes: notes}, nil
}

type writeIn struct {
	ID       string `json:"id" jsonschema:"where the concept lives: its full path, / separated (e.g. metrics/revenue, 用語/売上). The last segment must not be \"index\" or \"log\""`
	Document string `json:"document" jsonschema:"the concept as an OKF document: YAML frontmatter, then markdown.\nFrontmatter: type (required, one line; the description lists the recommended ones), title (the id's last segment names it without one), description, tags, resource (the underlying asset's URI), status (draft | stable | deprecated; omitted reads as stable), status_note, stale_after (YYYY-MM-DD), sources (list of {resource, id, title, ...} — the material this derives from), usage_window ({from, to}). An Attested Computation also takes runtime (required), parameters, computation, executor, attester.\nProducer-defined keys sit beside these and are kept as written. Server-owned keys (generated, verified, created_by, rejected_by, rejected_at) are never read as truth: a document read back from ochakai can be returned as-is, and one from elsewhere keeps them as its own claim under received, which nothing derives trust from"`
}

// toKnowledge parses the document. Notes — values read differently than
// written — come back with it, for the tool to hand to the agent: a
// reinterpretation is never silent (design doc 0036 §3.4).
//
// claimed names the server-owned keys the document carried, which became
// a claim rather than reaching a ledger (design doc 0046 §2.2). Whether
// the claim survives is the write's answer, not the parse's, so the note
// is added afterwards (okf.NoteClaim).
func (in writeIn) toKnowledge() (k *domain.Knowledge, notes, claimed []string, err error) {
	d, notes, err := okf.Parse([]byte(in.Document))
	if err != nil {
		return nil, nil, nil, service.Invalidf("%s", err.Error())
	}
	out := d.Knowledge
	out.ID = in.ID
	return &out, notes, d.Claimed, nil
}
