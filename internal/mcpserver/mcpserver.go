// Package mcpserver exposes ochakai's seven MCP tools over streamable HTTP.
// Tool names follow verb_object so they stay unambiguous next to other MCP
// servers' tools (design doc §4).
package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

const serverName = "ochakai"

// Handler returns the streamable HTTP handler serving the MCP endpoint.
func Handler(svc *service.Service, version string) http.Handler {
	server := newServer(svc, version)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}

func newServer(svc *service.Service, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: "ochakai is a context provider for data agents: metric definitions, " +
			"verified golden queries, interpretation knowledge (how to read a metric), " +
			"glossary terms, and table catalog entries — those five types are recommendations, " +
			"and any slug works as a type for your own document kinds. IDs may be hierarchical " +
			"(slash-separated, e.g. sales/orders) to organize knowledge into directories. " +
			"It executes no SQL and uses no LLM. " +
			"Before answering a data question, call get_context once — it returns the relevant " +
			"entries in full, links expanded. " +
			"Prefer verified knowledge and judge trust from provenance (created_by / verified_by). " +
			"Write learnings back with create_knowledge; set status=verified only for knowledge " +
			"you have actually validated — who verified is always recorded. Knowledge that was " +
			"reviewed and not accepted is status=rejected (with status_note explaining why); " +
			"rejected entries are hidden from search unless you filter for them — check before " +
			"re-proposing similar knowledge. Knowledge is co-owned by humans and agents.",
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "search_knowledge",
		Description: "Search the knowledge base across all types (recommended: metric, query, insight, term, table; custom types welcome). " +
			"Verified entries rank higher. Filter with types/statuses/tags. Returns scored hits. " +
			"Rejected entries are excluded unless statuses includes \"rejected\" — filter for them " +
			"to check whether a proposal was already rejected before creating similar knowledge.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		hits, err := svc.Search(ctx, in.Query, store.Filter{
			Types: toTypes(in.Types), Statuses: toStatuses(in.Statuses), Tags: in.Tags,
		}, in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: hits}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_context",
		Description: "The one call to make before answering a data question: searches the knowledge " +
			"base (verified entries rank higher), returns the full entries behind the top hits, " +
			"and expands one hop through links so the insight explaining a metric and the golden " +
			"query answering the question arrive together. Prefer this over search+get chains; " +
			"fall back to search_knowledge/get_knowledge for precise lookups.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in contextIn) (*mcp.CallToolResult, contextOut, error) {
		res, err := svc.Context(ctx, in.Query, store.Filter{
			Types: toTypes(in.Types), Statuses: toStatuses(in.Statuses), Tags: in.Tags,
		}, in.Limit, in.MinScore)
		if err != nil {
			return nil, contextOut{}, err
		}
		return nil, contextOut{Hits: res.Hits, Entries: res.Entries}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_knowledge",
		Description: "Get one knowledge entry by type and id, including its full markdown body, structured attrs, and links.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Get(ctx, domain.Type(in.Type), in.ID)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_knowledge",
		Description: "Create a knowledge entry. Write back what you learned: metric caveats, verified answers, " +
			"glossary terms. Entries default to draft; your identity is recorded as created_by. " +
			"Before creating, search existing entries including statuses=[\"rejected\"] to avoid " +
			"re-proposing knowledge that was already rejected (status_note records why).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Create(ctx, in.toKnowledge(), httpauth.Actor(ctx))
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_knowledge",
		Description: "Update a knowledge entry (full replacement of title/description/tags/status/links/attrs/body). " +
			"Every change is kept as a revision. Setting status=verified records you as verified_by — " +
			"do it only for knowledge you have actually validated. Setting status=rejected records you " +
			"as rejected_by; put the reason in status_note (also useful when deprecating).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Update(ctx, in.toKnowledge(), httpauth.Actor(ctx))
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: *k}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_knowledge",
		Description: "Soft-delete a knowledge entry. History is retained as revisions; " +
			"create_knowledge on the same type/id revives it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, deleteOut, error) {
		if err := svc.Delete(ctx, domain.Type(in.Type), in.ID, httpauth.Actor(ctx)); err != nil {
			return nil, deleteOut{}, err
		}
		return nil, deleteOut{Deleted: true, URI: fmt.Sprintf("ochakai://%s/%s", in.Type, in.ID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "compile_sql",
		Description: "Deterministically compile metrics + dimensions + filters + time_grain into SQL from the " +
			"imported Ossie semantic model (no LLM involved). Dialects: bigquery (default), ansi. " +
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

type searchIn struct {
	Query    string   `json:"query"`
	Types    []string `json:"types,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
}

type contextIn struct {
	Query    string   `json:"query"`
	Types    []string `json:"types,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	// MinScore drops hits below it; scores are search-mode dependent
	// (trigram vs hybrid RRF), so leave it 0 unless calibrated.
	MinScore float64 `json:"min_score,omitempty"`
}

type contextOut struct {
	Hits    []domain.SearchHit `json:"hits"`
	Entries []domain.Knowledge `json:"entries"`
}

type getIn struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type knowledgeOut struct {
	Knowledge domain.Knowledge `json:"knowledge"`
}

type deleteOut struct {
	Deleted bool   `json:"deleted"`
	URI     string `json:"uri"`
}

type writeIn struct {
	Type        string         `json:"type"`
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Status      string         `json:"status,omitempty"`
	StatusNote  string         `json:"status_note,omitempty"`
	Links       []domain.Link  `json:"links,omitempty"`
	Attrs       map[string]any `json:"attrs,omitempty"`
	Body        string         `json:"body,omitempty"`
}

func (in writeIn) toKnowledge() *domain.Knowledge {
	return &domain.Knowledge{
		Type:        domain.Type(in.Type),
		ID:          in.ID,
		Title:       in.Title,
		Description: in.Description,
		Tags:        in.Tags,
		Status:      domain.Status(in.Status),
		StatusNote:  in.StatusNote,
		Links:       in.Links,
		Attrs:       in.Attrs,
		Body:        in.Body,
	}
}

func toTypes(ss []string) []domain.Type {
	out := make([]domain.Type, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Type(s))
	}
	return out
}

func toStatuses(ss []string) []domain.Status {
	out := make([]domain.Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Status(s))
	}
	return out
}
