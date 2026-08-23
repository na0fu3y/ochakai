// Package mcpserver exposes ochakai's MCP tools over streamable HTTP.
// Tool names follow verb_object so they stay unambiguous next to other MCP
// servers' tools (design doc §4). The REST API (internal/restapi) is a
// superset of these tools: the same operations plus the archive of a
// bundle path, the derived index.md and log.md, the reverse lookup
// (search's links_to=), the human's ruling, deletion, the usage totals and
// file writes (design doc 0067 §5.1 keeps those off MCP — agents use
// search_concepts/get_concept; tool schemas cost agent context, so the
// tool count is a budget, and design doc 0076 returned two of it).
package mcpserver

import (
	"context"
	"encoding/json"
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
//
// Stateless, which is what protocol version 2026-07-28 asks for and what
// the SDK requires before it will speak it at all: a stateful handler
// caps every client at 2025-11-25 (design doc 0118). A request carries
// its own protocol version, client identity and capabilities, so there is
// no Mcp-Session-Id to issue, nothing to expire, and GET and DELETE are
// 405. ochakai has nothing to keep between calls — no roots, no sampling,
// no server-initiated request, and its own state is the knowledge base —
// so the mode it loses is one it never used.
func Handler(svc *service.Service, version string) http.Handler {
	server := newServer(svc, version, RetiredToolNames)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true})
}

// extraProvider is the part of an MCP request the actor comes from. Tool
// calls and resource reads carry it alike, and both record provenance, so
// neither is a special case.
//
// The client's name comes along for the producer: MCP asks every client
// to name itself and its version, which is SPEC §7's
// "<producer>/<version>" arriving without anyone having to add a header
// (design doc 0065 §4).
type extraProvider interface {
	GetExtra() *mcp.RequestExtra
	ClientInfo() *mcp.Implementation
}

// clientProducer is the calling client's name as a producer string, or ""
// when it named nothing usable.
//
// Read off the request rather than off the session: at protocol version
// 2026-07-28 the name rides on every call in `_meta`, and there is no
// session to hold it (design doc 0118 §3). The SDK's accessor falls back
// to the initialize-time name for an older client that still has a
// session, so both eras answer here — what the fallback cannot reach is
// an older client on a stateless deployment, whose name is stated in the
// initialize its later calls no longer travel with. That is the cost 0118
// §3 names, and the Ochakai-Producer header is what covers it.
//
// It is a fallback, not an override: a caller that sent Ochakai-Producer
// said what it is on this call, while clientInfo says what the connection
// is — and a host that proxies several agents distinguishes them only by
// the header. Silently preferring the client's own name would be the
// misattribution 0027 §5.2 refuses, one level down.
//
// A clientInfo that does not fit SPEC §7's form is dropped rather than
// refused. The client did not claim to be sending provenance — the
// protocol asked it for a name — so failing its tool call over a version
// string ochakai cannot spell would turn a courtesy into an outage.
func clientProducer(req extraProvider) string {
	info := req.ClientInfo()
	if info == nil {
		return ""
	}
	p := info.Name + "/" + info.Version
	if !domain.ValidProducer(p) {
		return ""
	}
	return p
}

// requestActor resolves the actor to record for a tool call from the
// HTTP headers that carried the call. httpauth.Middleware puts the actor
// on the request context, and under a stateless handler (design doc 0118)
// that context is this call's own, so the two now agree. They did not
// have to: a stateful session is connected once — at initialize — and
// every later call ran on a context derived from that first request, so
// the context value was pinned to whoever opened the session, and a
// delegating host (design doc 0027) reusing one session across end users
// had every write silently misattributed to the first user — the exact
// failure 0027 §5.2 refuses.
//
// The resolution stays because the header is the authority either way and
// the guarantee should not rest on which transport mode a deployment
// happens to be in. The middleware has already vetted these headers (a
// rejected delegation never reaches a handler), so resolution here cannot
// newly fail; if it somehow does, refuse the write rather than
// misattribute it. In-process transports (tests, embedding without HTTP)
// carry no request headers and fall back to the context actor.
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
		actor.Producer = clientProducer(req)
	}
	return actor, nil
}

// requestCtx resolves the per-call actor and puts it on the context the
// service runs under. Not every provenance path takes an actor argument:
// report_outcome's reporter and the usage events the read tools record
// are read from the context by the service, so what this puts there is
// what they record. Every tool handler goes through here rather than only
// the ones that record something today — the failure is silent
// misattribution, so a service change that starts recording on a path
// this skipped would regress without a symptom.
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
//
// It is also where the answer becomes bytes, and it hands the SDK `any`
// rather than the handler's own type so that no output schema is derived
// from it (design doc 0103). Two things follow from that, and both are
// the point: nothing declares a schema this deployment does not send —
// the SDK derives one from the type parameter, so leaving it typed was
// enough to publish 15KB nobody counted — and the result travels once,
// because structuredContent is what the SDK fills in when there is a
// schema, and the text copy beside it is its own compatibility fallback.
func tool[In, Out any](svc *service.Service,
	fn func(ctx context.Context, actor domain.Actor, in In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		ctx, actor, err := requestCtx(ctx, svc.Config, req)
		if err != nil {
			return nil, nil, err
		}
		res, out, err := fn(ctx, actor, in)
		if err != nil {
			return nil, nil, err
		}
		return answer(res, out)
	}
}

// answer renders a handler's own result as the one copy the wire carries:
// JSON in a text block, which is the content type every MCP client can
// read. get_file is the reason it appends rather than replaces — the
// bytes it fetched are already a content block of their own, and the
// metadata belongs beside them rather than instead of them.
func answer(res *mcp.CallToolResult, out any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(out)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling %T: %w", out, err)
	}
	if res == nil {
		res = &mcp.CallToolResult{}
	}
	res.Content = append(res.Content, &mcp.TextContent{Text: string(b)})
	// nil is what stops the SDK filling in structuredContent — with a
	// non-nil value it marshals a second copy of exactly these bytes.
	return res, nil, nil
}

// instructions is what a client holds for the whole conversation,
// beside the tool list. It is counted with the tools rather than treated
// as free (TestMCPStaysUnderItsResidentByteCap).
const instructions = "ochakai serves human-curated knowledge for data work: metric definitions, " +
	"attested computations (sanctioned SQL and other computations), interpretation " +
	"knowledge (how to read a metric), glossary terms, dataset and table catalog " +
	"entries, and references. Those types are recommendations; any single-line string " +
	"is a type. It runs no SQL and no LLM.\n" +
	"Before answering a data question, search_concepts, then get_concept the hits worth " +
	"reading. A fetched concept carries linked_from — the concepts pointing at it, which " +
	"is where the insight that says how to read a metric lives — so fetch those too " +
	"before trusting a number.\n" +
	"A concept's id is its path (metrics/revenue, ga4/tables/orders). Place together " +
	"what should be read together; the type is metadata, not a location.\n" +
	"Judge trust from provenance, not from status: status is the lifecycle (draft, " +
	"stable, deprecated) and whether anyone confirmed a concept is recorded separately, " +
	"by whoever confirmed it. A passed stale_after means due for re-checking, not wrong.\n" +
	"After acting on knowledge, call report_outcome. Write learnings back with " +
	"put_concept as drafts for a human to confirm. Knowledge is co-owned by humans and agents."

// sandboxNotice is what a sandbox owes the agent writing into it. Design
// doc 0087 §3 requires the deployment to say what it is, because a
// sandbox that does not takes the work of whoever wrote there — and an
// agent writes there under instruction, which makes the loss a person's.
//
// It rides on the instructions rather than a tool of its own: MCP's
// default is no (design doc 0067 §4) because a tool schema is resident in
// every agent's context on every turn, and this is a sentence that only
// the deployment it is true of ever sends. An ordinary deployment's
// resident bytes do not move, which is what
// TestMCPStaysUnderItsResidentByteCap measures.
//
// A `dev` deployment is deliberately not announced here. What it costs is
// that no ruling names anybody, which is the operator's problem and the
// CLI says it there; nothing an agent writes to it is lost.
const sandboxNotice = "\nThis deployment is a sandbox: anyone may write, nobody is identified, " +
	"and the whole base is restored on a schedule — a draft you write here will be erased. " +
	"Write to it to try the loop, and say so when you report having written."

// instructionsFor returns what this deployment tells a client to hold for
// the whole conversation. Everything but the posture is the same
// everywhere.
func instructionsFor(cfg *config.Config) string {
	if cfg != nil && cfg.Sandbox {
		return instructions + sandboxNotice
	}
	return instructions
}

func newServer(svc *service.Service, version string, retired []RetiredToolName) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, &mcp.ServerOptions{
		Instructions: instructionsFor(svc.Config),
	})

	// A name this release renamed still answers, for this release only
	// (design doc 0088). It goes on before any tool is registered because
	// it decides which registration a call reaches.
	s.AddReceivingMiddleware(answerForRetiredNames(retired))

	// Expose concepts as MCP resources so clients can @-mention them by their
	// canonical ochakai:// URI. Only the template is advertised — enumerating
	// every concept in resources/list would flood the client, so discovery stays
	// with search_concepts and the URI is the addressing scheme.
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
			"URIs with search_concepts; get_concept returns the same concept " +
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
			"that needs narrowing. To walk a whole feed instead, use list_concepts.\n" +
			"degraded=true in the answer means the query could not be embedded and this ranking " +
			"is lexical only: thin or odd results are the deployment, not your question.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		// SearchOrList routes an empty query with a source filter to a
		// listing before it ever checks that search needs a query (0096
		// §5 put that reverse lookup on list_concepts, which has a cursor
		// parameter and a listing-sized limit — this schema has neither).
		// Reject it here so this tool stays one capability, not two
		// behind one schema (issue #602).
		if strings.TrimSpace(in.Query) == "" {
			return nil, searchOut{}, service.Invalidf(
				"search_concepts needs a query; to list what derives from a source, use list_concepts")
		}
		page, err := svc.SearchOrList(ctx, in.Query, "", "", in.filter(), in.Limit)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Hits: page.Hits, Degraded: page.Degraded}, nil
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
		Description: "Walk a review feed in full; each feed's own priority order (below). Scores " +
			"nothing and ranks nothing: pass the cursor from the last page to continue, and no " +
			"cursor back means the end.\n" +
			"- verified_at: by verification age, oldest first — stale verified knowledge.\n" +
			"- usage: by how often the concept was read — the draft review feed.\n" +
			"- failed: reported wrong via report_outcome, worst first — the re-verification feed.\n" +
			"- stale_after: past the author's declared expiry, most overdue first — verifying " +
			"alone does not clear this feed, because the date is the writer's declaration and " +
			"only an edit changes it.\n" +
			"Omit sort with source set to list what derives from that material instead.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in listIn) (*mcp.CallToolResult, listOut, error) {
		f := in.filter()
		if in.Sort == "" && f.Source == "" {
			return nil, listOut{}, service.Invalidf(
				"list_concepts needs sort (%s) or source; to search by text, use search_concepts",
				strings.Join(domain.ListSorts, ", "))
		}
		page, err := svc.SearchOrList(ctx, "", in.Sort, in.Cursor, f, in.Limit)
		if err != nil {
			return nil, listOut{}, err
		}
		return nil, listOut{Hits: page.Hits, Cursor: page.Cursor}, nil
	}))

	// get_context is not a tool (design doc 0108). The one-call pack —
	// search, fetch, one hop through links, all inside a byte budget —
	// retired from every surface at once: an agent's read is
	// search_concepts → get_concept, and the reverse hop the pack
	// bundled now arrives as linked_from rows on the concept itself
	// (design doc 0106).

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_concept",
		Annotations: readOnly,
		Description: "Get one concept by id: its full markdown body, structured attrs, links, the " +
			"files its body references (fetch their bytes with get_file), and linked_from — " +
			"rows for the concepts whose bodies link at this one, which is where the caveat " +
			"that says how to read a metric lives. Fetch the linked_from rows that matter " +
			"before trusting a number.",
	}, tool(svc, func(ctx context.Context, _ domain.Actor, in getIn) (*mcp.CallToolResult, knowledgeOut, error) {
		k, err := svc.Get(ctx, in.ID)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		v, err := okf.ViewOf(k)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		return nil, knowledgeOut{Knowledge: v, Hint: conceptHint}, nil
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
		Annotations: replacingWrite,
		Description: "Write a concept: created if the id is free, replaced if it is taken.\n" +
			"The document you send is the whole concept — a key you leave out is cleared, so on a " +
			"replace, get_concept first and send back what you are not changing. Every change is " +
			"kept as a revision, and the answer's plan says which of created / updated / " +
			"unchanged this write turned out to be.\n" +
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
			out, err := view(k, okf.NoteClaim(notes, claimed, k), domain.PlanCreated)
			return nil, out, err
		}
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		k, changed, err := svc.Update(ctx, write, actor, version)
		if err != nil {
			return nil, knowledgeOut{}, err
		}
		out, err := view(k, okf.NoteClaim(notes, claimed, k), domain.PlanOf(false, changed))
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
		Annotations: recordingWrite,
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
// was, delete_concept, left with design doc 0076. The values are
// immutable: readOnly is shared by the four reads, and the two writes
// each have their own, for the reason below.
//
// openWorldHint is false on all six, and it is the one hint whose default
// is wrong here: the spec's own example for a closed world is a memory
// tool, and every tool on this surface reads or writes this deployment's
// own base and reaches nothing else. Left unsaid it defaults to true,
// which would tell a client these calls go somewhere unbounded — the
// opposite of what ochakai refuses to do (design doc 0081: no LLM, no
// SQL, and the only hosts it contacts are Google Cloud APIs in the
// caller's own project).
//
// idempotentHint is where the two writes stop being one thing, so they no
// longer share a value. put_concept is a replace at an address: the same
// document written twice lands the same concept, and an identical write
// is recorded as no revision at all. report_outcome is not — every call
// is another outcome, which is the point (design doc 0069: the loop is
// measured by how often knowledge was used and how often it failed), so a
// client that retried it would move a counter the curator reads. The
// hint says which of the two a retry is safe on, and that is exactly the
// question a retry has to answer.
var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
	// A write whose repeat lands the same concept.
	replacingWrite = &mcp.ToolAnnotations{
		DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false),
	}
	// A write whose repeat is a second record.
	recordingWrite = &mcp.ToolAnnotations{
		DestructiveHint: new(false), OpenWorldHint: new(false),
	}
)

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
// out-of-range refusal) that api/openapi.yaml and the CLI help already
// document — MCP agents only see the tool schema.
// filters are the seven fields search_concepts and list_concepts both
// take, in one embedded struct rather than repeated per tool: they are
// the same question everywhere, and a schema written twice is a schema
// that starts differing. get_context has its own contextIn instead,
// without rejected or source.
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
	Limit int `json:"limit,omitempty" jsonschema:"max results: default 10, max 50 (out-of-range is an error)"`
}

// listIn is the same filters with a feed and a position instead of a
// query. sort is not required in the schema, because a source lookup is
// a listing with no feed to name; the handler says so when neither is
// there.
type listIn struct {
	Sort string `json:"sort,omitempty" jsonschema:"which feed: verified_at | usage | failed | stale_after (the description says what each is)"`
	filters
	Limit  int    `json:"limit,omitempty" jsonschema:"max results per page: default 100, max 1000 (out-of-range is an error)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"resume: pass back the cursor the last page returned, with the same sort and filters"`
}

// searchOut is a ranking's answer and listOut is a listing's. They are
// two types because the fields are not the same ones — the split 0096
// made of the tools, finished on the answers. A search never pages
// (design doc 0050 §2.2), so a cursor on its schema was a field the
// server could not write there; a listing embeds nothing, so `degraded`
// on its schema would be the same mistake in the other direction.
type searchOut struct {
	Hits []domain.SearchHit `json:"hits"`
	// Degraded is design doc 0114: this deployment embeds and this
	// ranking did not, so the caller holds a worse answer than the same
	// question ordinarily gets here.
	//
	// No jsonschema tag, and the word is named in the tool's description
	// instead. This server declares no output schema (residentbytes_test
	// counts both halves precisely because one day it might), so a tag
	// here would define the field for nobody: the description is the only
	// text about an answer that reaches the agent before it reads one.
	Degraded bool `json:"degraded,omitempty"`
}

type listOut struct {
	Hits []domain.SearchHit `json:"hits"`
	// Cursor is present only when a listing has more behind this page;
	// its absence is the end (design doc 0050 §2.3 — no total is given).
	Cursor string `json:"cursor,omitempty"`
}

// conceptHint rides along with every get_concept answer — the point where
// knowledge is actually delivered. Claude Code sites can install the
// write-back habit with a Stop hook; a browser-based host has no shell to
// hang one on, so the server supplies it — same words for every host, no
// LLM involved (the MCP server's Instructions field carries the same
// habit, but hosts are free to drop instructions and many do; design docs
// 0069, 0096 §3). Per-response, so it costs no resident bytes.
const conceptHint = "After acting on this knowledge, call report_outcome (worked/failed) on the concepts you " +
	"used — failed reports are how stale verified knowledge gets caught. If this session " +
	"produced reusable knowledge, write it back with put_concept as a draft; search " +
	"rejected=true first so you do not re-propose something already turned down. " +
	// The other half of the write-back, and the one this surface never
	// said. A relationship between concepts is a markdown link in the
	// body and nothing else (design doc 0024) — so a draft that does not
	// carry one is reachable by search and by nothing else, and never
	// appears under linked_from when somebody reads the concept it is
	// about (design doc 0106). The instructions already tell the reading
	// half to follow linked_from; without this the writing half was
	// producing concepts that half would never reach.
	"Link a draft back to the concept it came from — an ordinary markdown link to that " +
	"concept's path in the body, [revenue](/metrics/revenue.md) — because that link is " +
	"the relationship, and without it nobody reading that concept meets what you wrote."

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
	// Hint is the write-back habit, on reads only (conceptHint): a write
	// is already the habit being exercised.
	Hint string `json:"hint,omitempty"`
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
// produced and the one word the write turned out to be. Every write path
// ends the same way, so it is written once.
//
// plan rides on the view rather than beside it, which is where REST puts
// it too — REST's body *is* the view (design doc 0097). An agent that
// wrote something has no headers to read on this surface, so without it
// there was no answer to "did that change anything" short of a second
// call.
func view(k *domain.Knowledge, notes []string, plan string) (knowledgeOut, error) {
	v, err := okf.ViewOf(k)
	if err != nil {
		return knowledgeOut{}, err
	}
	v.Plan = plan
	return knowledgeOut{Knowledge: v, Notes: notes}, nil
}

type writeIn struct {
	ID       string `json:"id" jsonschema:"where the concept lives: its full path, / separated (e.g. metrics/revenue, 用語/売上). The last segment must not be \"index\" or \"log\""`
	Document string `json:"document" jsonschema:"the concept as an OKF document: YAML frontmatter, then markdown.\nFrontmatter: type (required, one line; the description lists the recommended ones), title (the id's last segment names it without one), description, tags, resource (the underlying asset's URI), status (draft | stable | deprecated; omitted reads as stable), status_note, stale_after (YYYY-MM-DD), sources (list of {resource, id, title, ...} — the material this derives from), usage_window ({from, to}). An Attested Computation also takes runtime (required), parameters, computation, executor, attester.\nProducer-defined keys sit beside these and are kept as written. Server-owned keys (generated, verified, created_by, rejected_*) are never read as truth: a document read back from ochakai can be returned as-is, and one from elsewhere keeps them as its own claim under received, which nothing derives trust from"`
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
