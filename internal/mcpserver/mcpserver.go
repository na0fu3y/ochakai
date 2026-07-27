// Package mcpserver exposes ochakai's MCP tools over streamable HTTP.
// Tool names follow verb_object so they stay unambiguous next to other MCP
// servers' tools (design doc §4). The REST API (internal/restapi) is a
// superset of these tools: same operations plus bulk export/import and
// the human-facing browse/revisions/backlinks endpoints (design doc
// 0015 keeps those off MCP — agents use search/get_context; tool
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
type extraProvider interface {
	GetExtra() *mcp.RequestExtra
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
	if extra := req.GetExtra(); cfg != nil && extra != nil && extra.Header != nil {
		actor, _, err := httpauth.ActorFromHeader(cfg, extra.Header)
		if err != nil {
			return domain.Actor{}, fmt.Errorf("resolving actor: %w", err)
		}
		return actor, nil
	}
	return httpauth.Actor(ctx), nil
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

func newServer(svc *service.Service, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: "ochakai is a context provider for data agents: semantic models, " +
			"metric definitions, verified golden queries, interpretation knowledge (how to " +
			"read a metric), glossary terms, dataset and table catalog entries, and references " +
			"(mirrors of external material such as enum definitions or schema docs) — those " +
			"types are recommendations, and any slug works as a type for your own document kinds. " +
			"An entry's id is its path: slash-separated segments (e.g. metrics/revenue, " +
			"ga4/tables/orders) forming directories. Place together what should be read " +
			"together; the type is metadata, not a location. " +
			"It executes no SQL and uses no LLM. " +
			"Before answering a data question, call get_context once — it returns the relevant " +
			"entries in full, links expanded. " +
			"Prefer verified knowledge and judge trust from provenance (created_by / updated_by / verified_by), " +
			"and treat an entry whose stale_after has passed as due for re-checking, not as wrong. " +
			"After acting on knowledge (running a golden query, writing SQL from a metric definition), report " +
			"whether it actually worked with report_outcome — failed reports are how stale " +
			"verified knowledge gets caught. " +
			"Write learnings back with create_knowledge; set status=verified only for knowledge " +
			"you have actually validated — who verified is always recorded. Knowledge that was " +
			"reviewed and not accepted is status=rejected (with status_note explaining why); " +
			"rejected entries are hidden from search unless you filter for them — check before " +
			"re-proposing similar knowledge. Knowledge is co-owned by humans and agents.",
	})

	// Expose entries as MCP resources so clients can @-mention them by their
	// canonical ochakai:// URI. Only the template is advertised — enumerating
	// every entry in resources/list would flood the client, so discovery stays
	// with search_knowledge/get_context and the URI is the addressing scheme.
	// {+id} (RFC 6570 reserved expansion) lets the slash-separated id match;
	// a plain {id} would stop at the first slash.
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:     "knowledge",
		Title:    "Knowledge entry",
		MIMEType: "text/markdown",
		Description: "A single knowledge entry as an OKF document: YAML frontmatter (title, " +
			"status, provenance, type-specific attrs) followed by the markdown body and its " +
			"links. Address by canonical URI — the scheme plus the entry's id (its path), " +
			"e.g. ochakai://metrics/revenue or ochakai://queries/sales/top-customers. Discover " +
			"URIs with search_knowledge or get_context; get_knowledge returns the same entry " +
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
		Name:        "search_knowledge",
		Annotations: readOnly,
		Description: "Search the knowledge base across all types (recommended: Metric, Golden Query, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference; custom types welcome). " +
			"Verified entries rank higher. Filter with types/statuses/tags. Returns scored hits. " +
			"Attachments count too — filenames and file contents — and a hit is always the owning entry. " +
			"Rejected entries are excluded unless statuses includes \"rejected\" — filter for them " +
			"to check whether a proposal was already rejected before creating similar knowledge. " +
			"With sort=\"verified_at\" the tool lists entries by verification age instead of searching " +
			"(oldest first, never-verified last; omit query, scores are 0) — the feed for " +
			"golden-query canary runs and for finding stale verified knowledge. With sort=\"usage\" it " +
			"lists by demand (most search_hits first, never-used drafts oldest-first at the bottom) and " +
			"each hit carries its usage totals — the draft review/promotion feed. With sort=\"failed\" it " +
			"lists entries callers reported wrong (report_outcome failed), worst first — the re-verification " +
			"feed; empty when nothing was reported wrong. With sort=\"stale_after\" it lists entries whose " +
			"author declared an expiry that has now passed, most overdue first — re-check them and, if they " +
			"still hold, update stale_after; verifying alone does not clear this feed, because the date is " +
			"the writer's declaration and only an edit changes it. Exactly one of query / sort is required. " +
			"source is a filter rather than a mode: it narrows to the entries citing one resource — use it " +
			"when a source document changed and you need everything derived from it — and combines with a " +
			"query or with any sort.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, searchOut{}, err
		}
		f := store.Filter{
			Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses),
			Tags: in.Tags, Source: in.Source,
		}
		hits, err := svc.SearchOrList(ctx, in.Query, in.Sort, f, in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: hits}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_context",
		Annotations: readOnly,
		Description: "The one call to make before answering a data question: searches the knowledge " +
			"base (verified entries rank higher), returns the full entries behind the top hits, " +
			"and expands one hop through links so the insight explaining a metric and the golden " +
			"query answering the question arrive together. Prefer this over search+get chains; " +
			"fall back to search_knowledge/get_knowledge for precise lookups. \"entries\" is the " +
			"knowledge; \"hits\" is only the ranking behind it. Entries that do not fit the byte " +
			"budget are listed under \"outline\" — fetch any of them by id with get_knowledge.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in contextIn) (*mcp.CallToolResult, contextOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, contextOut{}, err
		}
		budget := in.Budget
		if budget <= 0 {
			budget = defaultContextBudget
		}
		res, err := svc.Context(ctx, service.ContextRequest{
			Query: in.Query,
			Filter: store.Filter{
				Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses), Tags: in.Tags,
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
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_knowledge",
		Annotations: readOnly,
		Description: "Get one knowledge entry by id, including its full markdown body, structured attrs, " +
			"links, and attachment metadata (files the body references: images, PDFs, plain-text data — " +
			"fetch bytes with get_attachment).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, knowledgeOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		k, err := svc.Get(ctx, in.ID)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_knowledge",
		Annotations: nonDestructive,
		Description: "Create a knowledge entry. Write back what you learned: metric caveats, verified answers, " +
			"glossary terms. Entries default to draft; your identity is recorded as created_by. " +
			"Before creating, search existing entries including statuses=[\"rejected\"] to avoid " +
			"re-proposing knowledge that was already rejected (status_note records why). " +
			"For BigQuery Table/BigQuery Dataset/Reference entries, set resource to the asset's canonical URI and favor " +
			"the conventional body sections: # Schema, # Common query patterns. " +
			"An \"Attested Computation\" entry records a sanctioned computation others must run instead of " +
			"improvising: put the computation in a # Computation fence in body and the contract in the " +
			"runtime, parameters, executor and attester fields. ochakai stores it and never runs it. " +
			"Cite the material an entry derives from in sources — each needs a resource, and an id lets " +
			"markdown footnotes in body attribute single claims to it. " +
			"Set stale_after when the knowledge has a known expiry date. " +
			"A semantic model is a \"Semantic Model\" entry with the Apache Ossie model object in attrs.spec " +
			"(one entry per model). Give each metric its own entry too (last id segment = the metric " +
			"name, e.g. metrics/<name>) with attrs.expression holding the expression, so the " +
			"definitions are searchable on their own; have table entries link to the Semantic Model entry from their body. " +
			"Links are never a field: write a markdown link to the other entry's path in body — " +
			"[revenue](/metrics/revenue.md) — and it becomes a link both ways (the other entry gains a backlink). " +
			"An id whose entry was deleted can be reused, which revives it as your draft — unless a human had " +
			"ruled on it (verified, rejected, deprecated), in which case this surface refuses: propose at a " +
			"different id instead.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		ctx, actor, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		k, err := svc.CreateKeepingCurated(ctx, in.toKnowledge(), actor)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_knowledge",
		Annotations: nonDestructive,
		Description: "Update a knowledge entry (full replacement of title/description/resource/tags/sources/usage_window/" +
			"status/stale_after/runtime/parameters/computation/executor/attester/attrs/body — " +
			"an omitted title clears it, making the filename the name, and omitted sources clear the citations). " +
			"Links are not a field: they come from the markdown links in body, so keep the ones you want to keep. " +
			"Every change is kept as a revision; an update identical to the stored content writes nothing. " +
			"Entries a human has ruled on — verified, rejected, or deprecated — cannot be updated from " +
			"this surface: if a verified entry is wrong, report_outcome failed; otherwise create_knowledge " +
			"a better draft and let a human promote it. " +
			"Setting status=verified records you as verified_by — " +
			"do it only for knowledge you have actually validated. Setting status=rejected records you " +
			"as rejected_by; put the reason in status_note (also useful when deprecating).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		ctx, actor, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		// MCP has no conditional-update channel, so it cannot opt into the
		// If-Match precondition (nil) and writes are last-write-wins. That
		// is tolerable for drafts and not for curated knowledge, so
		// verified entries are refused here rather than clobbered.
		version, err := svc.RefuseIfCurated(ctx, in.ID, "update")
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		// The version read by the guard becomes the precondition, so an
		// entry curated in the window between the two is a conflict rather
		// than a clobber — the surface has no If-Match channel of its own,
		// but the check can supply one.
		k, _, err := svc.Update(ctx, in.toKnowledge(), actor, version)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_knowledge",
		Annotations: destructive,
		Description: "Soft-delete a knowledge entry. History is retained as revisions; " +
			"create_knowledge on the same id revives it. Entries a human has ruled on — verified, " +
			"rejected, or deprecated — cannot be deleted from this surface; deleting a rejected " +
			"entry and recreating it would erase the record of why it was turned down.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, deleteOut, error) {
		ctx, actor, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, deleteOut{}, err
		}
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
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_attachment",
		Annotations: readOnly,
		Description: "Fetch one file attached to a knowledge entry (get_knowledge lists attachment " +
			"metadata under \"attachments\": images, PDFs, plain-text data files). Returns the " +
			"file as content plus its metadata. Attachments are context-heavy — fetch them " +
			"deliberately, when the entry's body references one you need to see (a dashboard's " +
			"normal shape, an ER diagram, a seeds file). ochakai never interprets attachments; " +
			"if you learn something from one, write it back into the entry's body with " +
			"update_knowledge so the knowledge becomes searchable text.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in attachmentIn) (*mcp.CallToolResult, attachmentOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, attachmentOut{}, err
		}
		att, data, err := svc.Attachment(ctx, in.ID, in.Name)
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
				URI:      fmt.Sprintf("ochakai://%s/attachments/%s", in.ID, att.Name),
				MIMEType: att.MediaType,
				Blob:     data,
			}}
		}
		res := &mcp.CallToolResult{Content: []mcp.Content{content}}
		return res, attachmentOut{Attachment: *att}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_knowledge_usage",
		Annotations: readOnly,
		Description: "Usage totals for one knowledge entry: how often it appeared in search results, " +
			"was fetched individually, and how it was reported to have worked, with last_used_at. " +
			"The measure of the write-back loop — evidence when deciding to promote a draft, " +
			"and a staleness signal for verified entries that stopped being used.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, usageOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, usageOut{}, err
		}
		u, err := svc.Usage(ctx, in.ID)
		if err != nil {
			return nil, usageOut{}, err
		}
		return nil, usageOut{Usage: *u}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "report_outcome",
		Annotations: nonDestructive,
		Description: "Report whether knowledge you acted on actually worked — the last edge of the " +
			"write-back loop. After running a golden query or SQL you wrote from an entry, report worked " +
			"(the result was correct) or failed (wrong or unusable; say what went wrong in note). " +
			"Reports feed the entry's usage totals (get_knowledge_usage), where failed counts " +
			"against verified entries flag them for re-verification. Your identity is recorded " +
			"with each report. Returns the entry's updated usage totals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in outcomeIn) (*mcp.CallToolResult, usageOut, error) {
		ctx, _, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, usageOut{}, err
		}
		id := strings.TrimPrefix(in.Target, uriScheme)
		if !domain.ValidID(id) {
			return nil, usageOut{}, fmt.Errorf("invalid target %q (want the entry's id, e.g. queries/monthly-revenue)", in.Target)
		}
		u, err := svc.ReportOutcome(ctx, id, in.Outcome, in.Note)
		if err != nil {
			return nil, usageOut{}, err
		}
		return nil, usageOut{Usage: *u}, nil
	})

	return s
}

// Tool annotations let clients apply auto-approval policies without reading
// prose. readOnlyHint here describes the knowledge domain: search/get
// never change an entry (they may bump usage counters — telemetry the hint
// deliberately ignores). Writes are non-destructive because history is kept as
// revisions; only delete is flagged destructive. The values are immutable and
// shared across tools.
var (
	readOnly       = &mcp.ToolAnnotations{ReadOnlyHint: true}
	nonDestructive = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}
	destructive    = &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}
)

func boolPtr(b bool) *bool { return &b }

// parseKnowledgeURI extracts the entry id from a canonical knowledge URI
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
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (Metric, Golden Query, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type); matched case-insensitively"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by status: draft, verified, deprecated, rejected"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Source   string   `json:"source,omitempty" jsonschema:"only entries citing this resource, matched exactly against sources[].resource — the reverse lookup for \"this material changed, what derives from it?\"; a filter, so it combines with query or sort"`
	Sort     string   `json:"sort,omitempty" jsonschema:"omit to search; \"verified_at\" lists by verification age, \"usage\" lists by demand (draft review feed), \"failed\" lists entries reported wrong (re-verification feed), \"stale_after\" lists entries past their declared expiry (most overdue first) — all mutually exclusive with query"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max results: searching default 10, max 50; with sort default 100, max 1000 (out-of-range falls back to the default)"`
}

type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
}

// contextIn deliberately omits min_score, which the REST surface still
// carries. A score floor is only usable once calibrated against a corpus
// in a known search mode (lexical vs hybrid RRF), and an agent cannot
// calibrate anything — offering it here spends tool-schema context on a
// parameter whose own documentation says to leave it alone. What an agent
// actually needs to bound a response is budget.
type contextIn struct {
	Query    string   `json:"query" jsonschema:"the data question to gather context for"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (Metric, Golden Query, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference, or any custom type); matched case-insensitively"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by status: draft, verified, deprecated, rejected"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max primary entries: default 5, max 20 (out-of-range falls back to the default); linked companions share a 2x limit total cap"`
	Budget   int      `json:"budget,omitempty" jsonschema:"max bytes of the knowledge in the response — the entries plus the outline rows naming the rest (default 12000); nothing else carries a body, since \"hits\" is the ranking only. Entries past it are listed under \"outline\" with their size, fetchable by id with get_knowledge, and those rows count against the same budget. Raise it when you need whole entries, lower it when context is tight"`
}

type contextOut struct {
	Hits      []domain.ContextRank    `json:"hits"`
	Entries   []domain.Knowledge      `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
	Hint      string                  `json:"hint"`
}

// defaultContextBudget bounds an unparameterized get_context. It is on by
// default because the callers that most need it — hosts that embed ochakai
// and never touch the tool arguments — are exactly the ones that will not
// set it. Roughly 3000 tokens of entries, English or Japanese.
const defaultContextBudget = 12000

// contextHint rides along with every context pack. Claude Code sites can
// install the write-back habit with a Stop hook; a browser-based host has
// no shell to hang one on, so the server supplies it — same words for
// every host, no LLM involved (the MCP server's Instructions field carries
// the same habit, but hosts are free to drop instructions and many do).
func contextHint(truncated int) string {
	hint := "After acting on this knowledge, call report_outcome (worked/failed) on the entries you " +
		"used — failed reports are how stale verified knowledge gets caught. If this session " +
		"produced reusable knowledge, write it back with create_knowledge as a draft; search " +
		"statuses=[\"rejected\"] first so you do not re-propose something already turned down."
	if truncated > 0 {
		hint += fmt.Sprintf(" %d entries did not fit the budget and are listed under \"outline\" — "+
			"fetch any of them by id with get_knowledge.", truncated)
	}
	return hint
}

type getIn struct {
	ID string `json:"id" jsonschema:"the entry's id — its path: slug segments separated by / (e.g. metrics/revenue, ga4/tables/orders)"`
}

type knowledgeOut struct {
	Knowledge domain.Knowledge `json:"knowledge"`
}

type attachmentIn struct {
	ID   string `json:"id" jsonschema:"the entry's id (its path, e.g. metrics/revenue)"`
	Name string `json:"name" jsonschema:"attachment filename, from the entry's attachments metadata"`
}

type attachmentOut struct {
	Attachment domain.Attachment `json:"attachment"`
}

type usageOut struct {
	Usage domain.Usage `json:"usage"`
}

type outcomeIn struct {
	Target  string `json:"target" jsonschema:"the id of the entry the outcome is about (e.g. queries/monthly-revenue; an ochakai:// prefix is tolerated)"`
	Outcome string `json:"outcome" jsonschema:"\"worked\" = acting on the entry gave a correct result; \"failed\" = it gave a wrong or unusable one"`
	Note    string `json:"note,omitempty" jsonschema:"optional context recorded with the report: what was run, what went wrong (max 2000 bytes)"`
}

type deleteOut struct {
	Deleted bool   `json:"deleted"`
	URI     string `json:"uri"`
}

type writeIn struct {
	Type        string              `json:"type" jsonschema:"what the entry is: the OKF type, one line; recommended: Metric, Golden Query, Attested Computation, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference — any custom type works"`
	ID          string              `json:"id" jsonschema:"where the entry lives: its full path, segments separated by / (e.g. metrics/revenue, 用語/売上); place together what should be read together; the last segment must not be \"index\" or \"log\""`
	Title       string              `json:"title,omitempty" jsonschema:"display name; optional — when omitted, the id's last segment (the filename) is the name; set one only when the filename isn't enough"`
	Description string              `json:"description,omitempty"`
	Resource    string              `json:"resource,omitempty" jsonschema:"canonical URI of the underlying asset (the table/dataset URI, or for references the external source URL); omit for abstract concepts"`
	Tags        []string            `json:"tags,omitempty"`
	Sources     []domain.Source     `json:"sources,omitempty" jsonschema:"the material this entry derives from: a list of {resource, id, title, author, usage_count, last_modified}; resource is required and names the artifact; give each an id so footnotes in body can attribute single claims to it"`
	UsageWindow *domain.UsageWindow `json:"usage_window,omitempty" jsonschema:"the date range the sources' usage_count values were counted over: {from, to} as YYYY-MM-DD"`
	Status      string              `json:"status,omitempty" jsonschema:"draft, verified, deprecated, or rejected; defaults to draft"`
	StatusNote  string              `json:"status_note,omitempty" jsonschema:"free-form reason for the current status (why rejected/deprecated)"`
	StaleAfter  string              `json:"stale_after,omitempty" jsonschema:"absolute date (YYYY-MM-DD) after which this entry should be re-checked, e.g. when the quarter it describes closes; omit when nothing dates it"`
	Runtime     string              `json:"runtime,omitempty" jsonschema:"for an Attested Computation, the execution environment (bigquery, postgres, dbt, python...); required for that type because it is what tells a consumer how to read the parameters"`
	Parameters  []domain.Parameter  `json:"parameters,omitempty" jsonschema:"an Attested Computation's declared inputs: a list of {name, type, required}; you supply values for these, never edits to the computation itself"`
	Computation string              `json:"computation,omitempty" jsonschema:"path to the file holding the computation; omit to put it inline in a # Computation fence in body"`
	Executor    *domain.Executor    `json:"executor,omitempty" jsonschema:"how the computation is run and what a run must return as evidence: {resource, receipt}; ochakai records this and never runs it"`
	Attester    *domain.Attester    `json:"attester,omitempty" jsonschema:"the deterministic checker for a run: {resource}; ochakai records the path and never executes it"`
	Attrs       map[string]any      `json:"attrs,omitempty" jsonschema:"producer-defined extension keys, e.g. question/sql for a query; the keys OKF itself defines are fields of their own above and are rejected here"`
	Body        string              `json:"body,omitempty" jsonschema:"markdown body; link to other entries by writing a markdown link to their path — [revenue](/metrics/revenue.md) — and those links become the entry's links"`
}

func (in writeIn) toKnowledge() *domain.Knowledge {
	return &domain.Knowledge{
		Type:        domain.Type(in.Type),
		ID:          in.ID,
		Title:       in.Title,
		Description: in.Description,
		Resource:    in.Resource,
		Tags:        in.Tags,
		Sources:     in.Sources,
		UsageWindow: in.UsageWindow,
		Status:      domain.Status(in.Status),
		StatusNote:  in.StatusNote,
		StaleAfter:  in.StaleAfter,
		Runtime:     in.Runtime,
		Parameters:  in.Parameters,
		Computation: in.Computation,
		Executor:    in.Executor,
		Attester:    in.Attester,
		Attrs:       in.Attrs,
		Body:        in.Body,
	}
}
