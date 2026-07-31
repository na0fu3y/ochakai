// Package mcpserver exposes ochakai's MCP tools over streamable HTTP.
// Tool names follow verb_object so they stay unambiguous next to other MCP
// servers' tools (design doc §4). The REST API (internal/restapi) is a
// superset of these tools: the same operations plus the archive of a
// bundle path, the derived index.md and log.md, the reverse lookup
// (search's links_to=), the human's ruling and file writes (design doc
// 0015 §3.1 keeps those off MCP — agents use search/get_context; tool
// schemas cost agent context, so the tool count is a budget).
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
	server := newServer(svc, version)
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
// It is a fallback, not an override: a caller that sent X-Ochakai-Producer
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

func newServer(svc *service.Service, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: "ochakai is a context provider for data agents: metric definitions, " +
			"attested computations (sanctioned SQL and other computations, including verified " +
			"question-and-answer queries), interpretation knowledge (how to read a metric), " +
			"glossary terms, dataset and table catalog entries, and references " +
			"(mirrors of external material such as enum definitions or schema docs) — those " +
			"types are recommendations, and any single-line string works as a type for your own " +
			"document kinds. " +
			"A concept's id is its path: slash-separated segments (e.g. metrics/revenue, " +
			"ga4/tables/orders) forming directories. Place together what should be read " +
			"together; the type is metadata, not a location. " +
			"It executes no SQL and uses no LLM. " +
			"Before answering a data question, call get_context once — it returns the relevant " +
			"concepts in full, links expanded. " +
			"Prefer verified knowledge and judge trust from provenance (created_by / updated_by / verifications), " +
			"and treat a concept whose stale_after has passed as due for re-checking, not as wrong. " +
			"After acting on knowledge (running an attested computation, writing SQL from a metric definition), report " +
			"whether it actually worked with report_outcome — failed reports are how stale " +
			"verified knowledge gets caught. " +
			"Write learnings back with put_concept; leave new concepts as drafts for a human to " +
			"confirm — status is how ready a concept is (draft, stable, deprecated), and whether " +
			"anyone checked it is recorded separately, by whoever checked it. Knowledge that was " +
			"reviewed and not accepted is marked rejected by a human, with the reason; " +
			"rejected concepts are hidden from search unless you ask for them (rejected=true) — check before " +
			"re-proposing similar knowledge. Knowledge is co-owned by humans and agents.",
	})

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
		Description: "Search the knowledge base across all types (recommended: " + domain.TypesHint() + "; custom types welcome). " +
			"Verified concepts rank higher. Filter with types/statuses/tags. Returns scored hits. " +
			"Attachments count too — filenames and file contents — and a hit is always the owning concept. " +
			"Rejected concepts are excluded unless you pass rejected=true — ask for them " +
			"to check whether a proposal was already rejected before creating similar knowledge. " +
			"With sort=\"verified_at\" the tool lists concepts by verification age instead of searching " +
			"(oldest first, never-verified last; omit query, scores are 0) — the feed for " +
			"canary runs and for finding stale verified knowledge. With sort=\"usage\" it " +
			"lists by demand (most search_hits first, never-used drafts oldest-first at the bottom) and " +
			"each hit carries its usage totals — the draft review/promotion feed. With sort=\"failed\" it " +
			"lists concepts callers reported wrong (report_outcome failed), worst first — the re-verification " +
			"feed; empty when nothing was reported wrong. With sort=\"stale_after\" it lists concepts whose " +
			"author declared an expiry that has now passed, most overdue first — re-check them and, if they " +
			"still hold, update stale_after; verifying alone does not clear this feed, because the date is " +
			"the writer's declaration and only an edit changes it. Exactly one of query / sort is required. " +
			"source and prefixes are filters rather than modes, and combine with a query or with any sort: " +
			"source narrows to the concepts citing one resource — use it when a source document changed and " +
			"you need everything derived from it — while prefixes narrows to the concepts living under given " +
			"paths, which is how you separate a team's own knowledge from the company-wide vocabulary. " +
			"A listing (any sort, or source alone) answers with a cursor when more concepts follow: pass it " +
			"back to read the next page, and walk a feed in pages rather than asking for one huge one. " +
			"A search has no cursor — it is bounded by limit, and a ranking has no page two.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		f := store.Filter{
			Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses),
			Tags: in.Tags, Source: in.Source, Prefixes: in.Prefixes,
			Trust: domain.ToTrusts(in.Trust), Rejected: in.Rejected,
		}
		page, err := svc.SearchOrList(ctx, in.Query, in.Sort, in.Cursor, f, in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: page.Hits, Cursor: page.Cursor}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_context",
		Annotations: readOnly,
		Description: "The one call to make before answering a data question: searches the knowledge " +
			"base (verified concepts rank higher), returns the full concepts behind the top hits, " +
			"and expands one hop through links so the insight explaining a metric and the " +
			"computation answering the question arrive together. Prefer this over search+get chains; " +
			"fall back to search_concepts/get_concept for precise lookups. \"concepts\" is the " +
			"knowledge; \"hits\" is only the ranking behind it. Concepts that do not fit the byte " +
			"budget are listed under \"outline\" — fetch any of them by id with get_concept.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in contextIn) (*mcp.CallToolResult, contextOut, error) {
		budget := in.Budget
		if budget <= 0 {
			budget = defaultContextBudget
		}
		res, err := svc.Context(ctx, service.ContextRequest{
			Query: in.Query,
			Filter: store.Filter{
				Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses), Tags: in.Tags,
				Prefixes: in.Prefixes, Trust: domain.ToTrusts(in.Trust),
			},
			Limit: in.Limit, Budget: budget,
		})
		if err != nil {
			return nil, contextOut{}, err
		}
		return nil, contextOut{
			Hits: res.Hits, Entries: res.Entries, Outline: res.Outline,
			Truncated: res.Truncated, Hint: contextHint(res.Truncated),
		}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_concept",
		Annotations: readOnly,
		Description: "Get one knowledge concept by id, including its full markdown body, structured attrs, " +
			"links, and attachment metadata (files the body references: images, PDFs, plain-text data — " +
			"fetch bytes with get_attachment).",
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
		Description: "Write a knowledge concept: create it if the id is free, replace it if it is taken. " +
			"Write back what you learned: metric caveats, confirmed answers, glossary terms. " +
			"Write status: draft unless you are recording something already agreed — a document that " +
			"names no status reads as stable (OKF SPEC §5.4). Your identity is recorded as created_by, " +
			"and the concept stays unverified until a person confirms it. " +
			"The document you send is the whole concept: a key you leave out is cleared, so on a replace, " +
			"get_concept first, change what you mean to change, and send the rest back as it came. " +
			"Every change is kept as a revision; a write identical to the stored content writes nothing. " +
			"Before writing something new, search with rejected=true to avoid re-proposing knowledge " +
			"that was already rejected (the ruling records why). " +
			"Concepts a human has ruled on — verified, rejected, or deprecated — cannot be replaced from " +
			"this surface: if a verified concept is wrong, report_outcome failed; otherwise put_concept " +
			"a better draft at a different id and let a human promote it. " +
			"status is the lifecycle only — draft, stable, deprecated (OKF SPEC §5.4). Whether anyone " +
			"has confirmed the concept is not yours to set: it comes from the verification ledger, and " +
			"this surface does not verify or reject. Put the reason for deprecating in status_note. " +
			"For BigQuery Table/BigQuery Dataset/Reference concepts, set resource to the asset's canonical URI and favor " +
			"the conventional body sections: # Schema, # Common query patterns. " +
			"An \"Attested Computation\" concept records a sanctioned computation others must run instead of " +
			"improvising: put the computation in a # Computation fence in body and the contract in the " +
			"runtime, parameters, executor and attester fields. ochakai stores it and never runs it. " +
			"A confirmed question-and-SQL pair is one of these: runtime says where the SQL runs, and " +
			"attrs.question carries the natural-language question it answers. " +
			"Cite the material a concept derives from in sources — each needs a resource, and an id lets " +
			"markdown footnotes in body attribute single claims to it. " +
			"Set stale_after when the knowledge has a known expiry date. " +
			"Give each metric its own \"Metric\" concept (last id segment = the metric name, e.g. " +
			"metrics/<name>) so definitions are searchable on their own, and have table concepts link " +
			"to it from their body. " +
			"Links are never a field: write a markdown link to the other concept's path in body — " +
			"[revenue](/metrics/revenue.md) — and it becomes a link both ways (the other concept gains a backlink). " +
			"An id whose concept was deleted can be reused, which revives it as your draft — unless a human had " +
			"ruled on it (verified, rejected, deprecated), in which case this surface refuses: propose at a " +
			"different id instead.",
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

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_concept",
		Annotations: destructive,
		Description: "Soft-delete a knowledge concept. History is retained as revisions; " +
			"put_concept on the same id revives it. Concepts a human has ruled on — verified, " +
			"rejected, or deprecated — cannot be deleted from this surface; deleting a rejected " +
			"concept and recreating it would erase the record of why it was turned down.",
	}, tool(svc, func(ctx context.Context, actor domain.Actor, in getIn) (*mcp.CallToolResult, deleteOut, error) {
		// Delete has no precondition channel in the store, so a curation
		// landing in the window between check and delete still wins. The
		// window is a single round trip and the delete is revivable.
		if _, err := svc.RefuseIfCurated(ctx, in.ID, "delete"); err != nil {
			return nil, deleteOut{}, err
		}
		if err := svc.Delete(ctx, in.ID, actor); err != nil {
			return nil, deleteOut{}, err
		}
		return nil, deleteOut{Deleted: true, URI: uriScheme + in.ID}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_attachment",
		Annotations: readOnly,
		Description: "Fetch one file of the bundle by its path (get_concept lists the files an " +
			"concept shows under \"attachments\", each with its path: images, PDFs, plain-text data " +
			"files, anything a producer put there). Returns the file as content plus its metadata. Attachments are context-heavy — fetch them " +
			"deliberately, when the concept's body references one you need to see (a dashboard's " +
			"normal shape, an ER diagram, a seeds file). ochakai never interprets attachments; " +
			"if you learn something from one, write it back into the concept's body with " +
			"put_concept so the knowledge becomes searchable text.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in attachmentIn) (*mcp.CallToolResult, attachmentOut, error) {
		att, data, err := svc.GetFile(ctx, in.Path)
		if err != nil {
			return nil, attachmentOut{}, err
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
		return res, attachmentOut{Attachment: *att}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_concept_usage",
		Annotations: readOnly,
		Description: "Usage totals for one knowledge concept: how often it appeared in search results, " +
			"was fetched individually, and how it was reported to have worked, with last_used_at. " +
			"The measure of the write-back loop — evidence when deciding to promote a draft, " +
			"and a staleness signal for verified concepts that stopped being used.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in getIn) (*mcp.CallToolResult, usageOut, error) {
		u, err := svc.Usage(ctx, in.ID)
		if err != nil {
			return nil, usageOut{}, err
		}
		return nil, usageOut{Usage: *u}, nil
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "report_outcome",
		Annotations: nonDestructive,
		Description: "Report whether knowledge you acted on actually worked — the last edge of the " +
			"write-back loop. After running an attested computation or SQL you wrote from a concept, report worked " +
			"(the result was correct) or failed (wrong or unusable; say what went wrong in note). " +
			"Reports feed the concept's usage totals (get_concept_usage), where failed counts " +
			"against verified concepts flag them for re-verification. Your identity is recorded " +
			"with each report. Returns the concept's updated usage totals.",
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
var writeTools = []string{"put_concept", "delete_concept", "report_outcome"}

// Tool annotations let clients apply auto-approval policies without reading
// prose. readOnlyHint here describes the knowledge domain: search/get
// never change a concept (they may bump usage counters — telemetry the hint
// deliberately ignores). Writes are non-destructive because history is kept as
// revisions; only delete is flagged destructive. The values are immutable and
// shared across tools.
var (
	readOnly       = &mcp.ToolAnnotations{ReadOnlyHint: true}
	nonDestructive = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}
	destructive    = &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}
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
type searchIn struct {
	// Query drives the search. Optional in the schema because sort mode
	// rejects it — exactly one of query / sort must be set (the service
	// rejects an empty search, the handler rejects the combination).
	Query    string   `json:"query,omitempty" jsonschema:"search text; required unless sort is set (omit it then)"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (Metric, Attested Computation, Skill, Insight, Policy, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type); matched case-insensitively"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by lifecycle status: draft, stable, deprecated. Whether anyone confirmed a concept is a separate question — use verified"`
	Trust    []string `json:"trust,omitempty" jsonschema:"filter by who confirmed the concept: unverified, machine-confirmed, human-reviewed (OKF SPEC §5.3); repeat to OR them, omit to not ask. Independent of status: a draft can be human-reviewed and a stable concept unverified"`
	Rejected *bool    `json:"rejected,omitempty" jsonschema:"true to list only concepts a human turned down — how you check whether a proposal was already rejected before making it again. Omit and rejected concepts stay out of results"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Source   string   `json:"source,omitempty" jsonschema:"only concepts citing this resource, matched exactly against sources[].resource — the reverse lookup for \"this material changed, what derives from it?\"; a filter, so it combines with query or sort"`
	Prefixes []string `json:"prefixes,omitempty" jsonschema:"only concepts addressed under these paths, e.g. [\"teams/growth\", \"company\"] — an id is a path, so this scopes the search to a subtree (\"metrics\" covers metrics and everything under metrics/, but not metrics-legacy/); listing several ORs them, which is how you ask your own scope and the shared one in one call; a filter, so it combines with query or sort"`
	Sort     string   `json:"sort,omitempty" jsonschema:"omit to search; \"verified_at\" lists by verification age, \"usage\" lists by demand (draft review feed), \"failed\" lists concepts reported wrong (re-verification feed), \"stale_after\" lists concepts past their declared expiry (most overdue first) — all mutually exclusive with query"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max results: searching default 10, max 50; with sort default 100, max 1000 (out-of-range falls back to the default)"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"resume a listing where the last page ended: pass back the cursor that page returned, with the same sort and filters. Only for listings (any sort, or source alone) — a search is bounded by limit and refuses it"`
}

type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
	// Cursor is present only when a listing has more behind this page;
	// its absence is the end (design doc 0050 §2.3 — no total is given).
	Cursor string `json:"cursor,omitempty"`
}

// contextIn omits fm: a frontmatter key filter is the vocabulary of
// somebody who already knows which key they mean, and the schema that
// teaches an agent the eleven askable spellings costs more context than
// the filter has ever saved (design doc 0058 §2.2). It stays on REST and
// the CLI, where the caller is a program somebody wrote or a person at a
// shell. What bounds a response for an agent is budget.
//
// min_score is not omitted here — it no longer exists anywhere (0058
// §2.1).
type contextIn struct {
	Query    string   `json:"query" jsonschema:"the data question to gather context for"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (Metric, Attested Computation, Skill, Insight, Policy, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type); matched case-insensitively"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by lifecycle status: draft, stable, deprecated. Whether anyone confirmed a concept is a separate question — use verified"`
	Trust    []string `json:"trust,omitempty" jsonschema:"filter by who confirmed the concept: unverified, machine-confirmed, human-reviewed (OKF SPEC §5.3); repeat to OR them, omit to not ask. Independent of status: a draft can be human-reviewed and a stable concept unverified"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Prefixes []string `json:"prefixes,omitempty" jsonschema:"only concepts addressed under these paths, e.g. [\"teams/growth\", \"company\"] — an id is a path, so this scopes the search to a subtree; listing several ORs them, which is how you ask your own scope and the shared one in one call. It scopes the search, not the link expansion: a concept in scope that cites a term outside it still arrives with that term"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max primary concepts: default 5, max 20 (out-of-range falls back to the default); linked companions share a 2x limit total cap"`
	Budget   int      `json:"budget,omitempty" jsonschema:"max bytes of the knowledge in the response — the concepts plus the outline rows naming the rest (default 12000); nothing else carries a body, since \"hits\" is the ranking only. Concepts past it are listed under \"outline\" with their size, fetchable by id with get_concept, and those rows count against the same budget. Raise it when you need whole concepts, lower it when context is tight"`
}

type contextOut struct {
	Hits      []domain.ContextRank    `json:"hits"`
	Entries   []domain.View           `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
	Hint      string                  `json:"hint"`
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
func contextHint(truncated int) string {
	hint := "After acting on this knowledge, call report_outcome (worked/failed) on the concepts you " +
		"used — failed reports are how stale verified knowledge gets caught. If this session " +
		"produced reusable knowledge, write it back with put_concept as a draft; search " +
		"statuses=[\"rejected\"] first so you do not re-propose something already turned down."
	if truncated > 0 {
		hint += fmt.Sprintf(" %d concepts did not fit the budget and are listed under \"outline\" — "+
			"fetch any of them by id with get_concept.", truncated)
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

type attachmentIn struct {
	Path string `json:"path" jsonschema:"the file's bundle path, from the path field of a concept's attachments metadata (e.g. metrics/revenue/chart.png)"`
}

type attachmentOut struct {
	Attachment domain.Attachment `json:"attachment"`
}

type usageOut struct {
	Usage domain.Usage `json:"usage"`
}

type outcomeIn struct {
	Target  string `json:"target" jsonschema:"the id of the concept the outcome is about (e.g. queries/monthly-revenue; an ochakai:// prefix is tolerated)"`
	Outcome string `json:"outcome" jsonschema:"\"worked\" = acting on the concept gave a correct result; \"failed\" = it gave a wrong or unusable one"`
	Note    string `json:"note,omitempty" jsonschema:"optional context recorded with the report: what was run, what went wrong (max 2000 bytes)"`
}

type deleteOut struct {
	Deleted bool   `json:"deleted"`
	URI     string `json:"uri"`
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
	ID       string `json:"id" jsonschema:"where the concept lives: its full path, segments separated by / (e.g. metrics/revenue, 用語/売上); place together what should be read together; the last segment must not be \"index\" or \"log\""`
	Document string `json:"document" jsonschema:"the concept as an OKF document: YAML frontmatter, then markdown. Frontmatter: type (required, one line — recommended: Metric, Attested Computation, Skill, Insight, Policy, Glossary Term, BigQuery Dataset, BigQuery Table, Reference; any custom type works), title (optional — the id's last segment is the name without one), description, tags, resource (the underlying asset's URI), status (draft, stable or deprecated; defaults to draft — whether anyone confirmed the concept is recorded separately and is not yours to set), status_note, stale_after (YYYY-MM-DD), sources (list of {resource, id, title, author, usage_count, last_modified} — the material this derives from), usage_window ({from, to}), and for an Attested Computation runtime (required), parameters (list of {name, type, required}), computation, executor ({resource, receipt}), attester ({resource}). Producer-defined keys go at the top level beside these and are kept as written. Link to other concepts with a markdown link to their path in the body — [revenue](/metrics/revenue.md) — and those links become the concept's links. Keys the server owns (generated, verified, created_by, rejected_by, rejected_at) never set who created or confirmed the concept, so a document read back from ochakai can be edited and returned as-is; a document from elsewhere keeps them as its own claim under received, which nothing derives trust from"`
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
