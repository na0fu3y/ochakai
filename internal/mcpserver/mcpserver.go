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
			"Prefer verified knowledge and judge trust from provenance (created_by / verified_by). " +
			"After acting on knowledge (running a golden query, using a compiled SQL), report " +
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
		Description: "Search the knowledge base across all types (recommended: metrics, queries, insights, terms, datasets, tables, references; custom types welcome). " +
			"Verified entries rank higher. Filter with types/statuses/tags. Returns scored hits. " +
			"Rejected entries are excluded unless statuses includes \"rejected\" — filter for them " +
			"to check whether a proposal was already rejected before creating similar knowledge. " +
			"With sort=\"verified_at\" the tool lists entries by verification age instead of searching " +
			"(oldest first, never-verified last; omit query, scores are 0) — the feed for " +
			"golden-query canary runs and for finding stale verified knowledge. With sort=\"usage\" it " +
			"lists by demand (most search_hits first, never-used drafts oldest-first at the bottom) and " +
			"each hit carries its usage totals — the draft review/promotion feed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		f := store.Filter{Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses), Tags: in.Tags}
		if in.Sort != "" {
			if in.Sort != "verified_at" && in.Sort != "usage" {
				return nil, searchOut{}, fmt.Errorf("invalid sort %q (valid: verified_at, usage)", in.Sort)
			}
			if in.Query != "" {
				return nil, searchOut{}, fmt.Errorf("sort=%s lists entries; it cannot be combined with a search query", in.Sort)
			}
			list := svc.ListByVerifiedAt
			if in.Sort == "usage" {
				list = svc.ListByUsage
			}
			hits, err := list(ctx, f, in.Limit)
			if err != nil {
				return nil, searchOut{}, err
			}
			return nil, searchOut{Hits: hits}, nil
		}
		hits, err := svc.Search(ctx, in.Query, f, in.Limit)
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
			"fall back to search_knowledge/get_knowledge for precise lookups.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in contextIn) (*mcp.CallToolResult, contextOut, error) {
		res, err := svc.Context(ctx, in.Query, store.Filter{
			Types: domain.ToTypes(in.Types), Statuses: domain.ToStatuses(in.Statuses), Tags: in.Tags,
		}, in.Limit, in.MinScore)
		if err != nil {
			return nil, contextOut{}, err
		}
		return nil, contextOut{Hits: res.Hits, Entries: res.Entries}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_knowledge",
		Annotations: readOnly,
		Description: "Get one knowledge entry by id, including its full markdown body, structured attrs, " +
			"links, and attachment metadata (files the body references: images, PDFs, plain-text data — " +
			"fetch bytes with get_attachment).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, knowledgeOut, error) {
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
			"For tables/datasets/references, set resource to the asset's canonical URI and favor " +
			"the conventional body sections: # Schema, # Common query patterns, # Citations. " +
			"A semantic model is a models entry with the Apache Ossie model object in attrs.spec " +
			"(one entry per model; validated on write). After creating one, create a metrics/<name> " +
			"entry per metric with attrs.model naming the models entry's id and attrs.expression " +
			"holding the expression, so compile_sql can resolve the model and the definitions are " +
			"searchable; give table entries a defined_in link to the models entry.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Create(ctx, in.toKnowledge(), httpauth.Actor(ctx))
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_knowledge",
		Annotations: nonDestructive,
		Description: "Update a knowledge entry (full replacement of title/description/resource/tags/status/links/attrs/body). " +
			"Every change is kept as a revision; an update identical to the stored content writes nothing. " +
			"Setting status=verified records you as verified_by — " +
			"do it only for knowledge you have actually validated. Setting status=rejected records you " +
			"as rejected_by; put the reason in status_note (also useful when deprecating).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, _, err := svc.Update(ctx, in.toKnowledge(), httpauth.Actor(ctx))
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_knowledge",
		Annotations: destructive,
		Description: "Soft-delete a knowledge entry. History is retained as revisions; " +
			"create_knowledge on the same id revives it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, deleteOut, error) {
		if err := svc.Delete(ctx, in.ID, httpauth.Actor(ctx)); err != nil {
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
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in attachmentIn) (*mcp.CallToolResult, attachmentOut, error) {
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
			"was fetched individually, or was referenced by compile_sql, with last_used_at. " +
			"The measure of the write-back loop — evidence when deciding to promote a draft, " +
			"and a staleness signal for verified entries that stopped being used.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, usageOut, error) {
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
			"write-back loop. After running a golden query or a compiled SQL, report worked " +
			"(the result was correct) or failed (wrong or unusable; say what went wrong in note). " +
			"Reports feed the entry's usage totals (get_knowledge_usage), where failed counts " +
			"against verified entries flag them for re-verification. Your identity is recorded " +
			"with each report. Returns the entry's updated usage totals.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in outcomeIn) (*mcp.CallToolResult, usageOut, error) {
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

	mcp.AddTool(s, &mcp.Tool{
		Name:        "compile_sql",
		Annotations: readOnly,
		Description: "Deterministically compile metrics + dimensions + filters + time_grain into SQL from a " +
			"semantic model — a models entry holding the Ossie model object in attrs.spec (no LLM " +
			"involved). model is the entry's id, resolved from the first metric's attrs.model when " +
			"omitted; the result names the models entry and its status — judge trust from its " +
			"provenance. Output is always BigQuery SQL. " +
			"ochakai does not execute SQL — run the result with your own warehouse tool. " +
			"Requests outside the supported subset fail with a reason; prefer any returned verified_queries.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.CompileRequest) (*mcp.CallToolResult, service.CompileResult, error) {
		res, err := svc.Compile(ctx, in)
		if err != nil {
			return nil, service.CompileResult{}, err
		}
		return nil, *res, nil
	})

	return s
}

// Tool annotations let clients apply auto-approval policies without reading
// prose. readOnlyHint here describes the knowledge domain: search/get/compile
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
	// rejects it — one of query / sort must be set.
	Query    string   `json:"query,omitempty" jsonschema:"search text; omit when sort is set"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (metrics, queries, insights, terms, datasets, tables, references, or any custom slug)"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by status: draft, verified, deprecated, rejected"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Sort     string   `json:"sort,omitempty" jsonschema:"omit to search; \"verified_at\" lists by verification age, \"usage\" lists by demand (draft review feed) — both mutually exclusive with query"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max results: searching default 10, max 50; with sort default 100, max 1000 (out-of-range falls back to the default)"`
}

type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
}

type contextIn struct {
	Query    string   `json:"query" jsonschema:"the data question to gather context for"`
	Types    []string `json:"types,omitempty" jsonschema:"filter by type (metrics, queries, insights, terms, datasets, tables, references, or any custom slug)"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"filter by status: draft, verified, deprecated, rejected"`
	Tags     []string `json:"tags,omitempty" jsonschema:"filter by tag"`
	Limit    int      `json:"limit,omitempty" jsonschema:"max primary entries: default 5, max 20 (out-of-range falls back to the default); linked companions share a 2x limit total cap"`
	// MinScore drops hits below it; scores are search-mode dependent
	// (trigram vs hybrid RRF), so leave it 0 unless calibrated.
	MinScore float64 `json:"min_score,omitempty" jsonschema:"drop hits scoring below this; scores are search-mode dependent and uncalibrated, so leave 0 (off) unless calibrated against your corpus"`
}

type contextOut struct {
	Hits    []domain.SearchHit `json:"hits"`
	Entries []domain.Knowledge `json:"entries"`
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
	Type        string         `json:"type" jsonschema:"what the entry is: one slug segment; recommended: metrics, queries, insights, terms, datasets, tables, references — any custom slug works"`
	ID          string         `json:"id" jsonschema:"where the entry lives: its full path, slug segments separated by / (e.g. metrics/revenue, sales/orders); place together what should be read together; the last segment must not be \"index\" or \"log\""`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Resource    string         `json:"resource,omitempty" jsonschema:"canonical URI of the underlying asset (the table/dataset URI, or for references the external source URL); omit for abstract concepts"`
	Tags        []string       `json:"tags,omitempty"`
	Status      string         `json:"status,omitempty" jsonschema:"draft, verified, deprecated, or rejected; defaults to draft"`
	StatusNote  string         `json:"status_note,omitempty" jsonschema:"free-form reason for the current status (why rejected/deprecated)"`
	Links       []domain.Link  `json:"links,omitempty" jsonschema:"typed edges to other entries, e.g. {rel: about, target: metrics/revenue}"`
	Attrs       map[string]any `json:"attrs,omitempty" jsonschema:"type-specific structured attributes, e.g. question/sql for a query"`
	Body        string         `json:"body,omitempty" jsonschema:"markdown body"`
}

func (in writeIn) toKnowledge() *domain.Knowledge {
	return &domain.Knowledge{
		Type:        domain.Type(in.Type),
		ID:          in.ID,
		Title:       in.Title,
		Description: in.Description,
		Resource:    in.Resource,
		Tags:        in.Tags,
		Status:      domain.Status(in.Status),
		StatusNote:  in.StatusNote,
		Links:       in.Links,
		Attrs:       in.Attrs,
		Body:        in.Body,
	}
}
