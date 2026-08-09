package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RetiredToolName is a tool name a release renamed away from, together
// with the name that answers instead (design doc 0088).
//
// The window it opens is deliberately one-sided: the old spelling stays
// *callable* without becoming *listed*. tools/list never returns it, so
// no agent learns it and no context window pays for a schema nobody
// should copy — which is the whole reason MCP's default is no
// (0067 §1). What it buys is the agent whose configuration or prompt
// already names the old tool: that call goes through for one release and
// comes back carrying the new name, instead of failing with "unknown
// tool" in the middle of somebody's task.
type RetiredToolName struct {
	// Old is the spelling that no longer exists.
	Old string
	// Instead is the tool the call is forwarded to. It has to be a tool
	// this server registers — an entry pointing at nothing would turn a
	// rename into the same outage with an extra hop.
	Instead string
	// Release is the release that did the renaming, which is the one
	// release this window covers. The entry is deleted when the next
	// release is cut, and TestARetiredNameLastsOneRelease is what makes
	// that something CI insists on rather than something a release PR
	// has to remember.
	Release string
}

// RetiredToolNames is empty between renames, and empty is its resting
// state rather than an oversight: a name that has served its release is
// deleted, not kept. Nothing here is retroactive either — the names MCP
// dropped before 0088 (compile_sql, the five that said knowledge) are
// gone and stay gone, because resurrecting a spelling nobody is still
// calling would only teach it again.
var RetiredToolNames []RetiredToolName

// answerForRetiredNames forwards a call made under a retired tool name to
// the tool that replaced it, and says so in the answer.
//
// It is middleware rather than a second registration because a
// registration is what tools/list reads: an alias added with AddTool
// would be a tool, with a schema, in every agent's context. Here the
// rewrite happens after the client has chosen a name and before the
// server looks one up, which is the only place the two facts — the call
// works, the name is not advertised — can both be true.
func answerForRetiredNames(retired []RetiredToolName) mcp.Middleware {
	byOld := make(map[string]RetiredToolName, len(retired))
	for _, r := range retired {
		byOld[r.Old] = r
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return next(ctx, method, req)
			}
			r, ok := byOld[call.Params.Name]
			if !ok {
				return next(ctx, method, req)
			}
			call.Params.Name = r.Instead
			res, err := next(ctx, method, req)
			out, isCall := res.(*mcp.CallToolResult)
			if err != nil || !isCall {
				return res, err
			}
			// In front of the answer, not behind it. A failing call gets
			// the notice too: "unknown argument" under a name that also
			// moved is a confusing thing to read without it.
			out.Content = append([]mcp.Content{&mcp.TextContent{Text: r.notice()}}, out.Content...)
			return out, nil
		}
	}
}

// notice is what the caller is told. It names the release rather than
// saying "deprecated", because the only question worth answering is how
// long this keeps working.
func (r RetiredToolName) notice() string {
	return fmt.Sprintf("ochakai: %s was renamed to %s in %s, and %s answered this call. "+
		"The old name stops working in the next release.", r.Old, r.Instead, r.Release, r.Instead)
}
