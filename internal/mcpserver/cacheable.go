package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listTTL is how long a client may keep the answer to a listing before
// asking again (design doc 0118 §7).
//
// The lists this covers are decided once, in newServer, from the
// deployment's posture: six tools or four, one resource template, and no
// prompts. Nothing registers a tool afterwards, so within one process the
// answer is a constant — the number below is not "how long the list stays
// true" but **how long a client may go on believing a process that has
// since been replaced**, because a release is the only thing that changes
// it.
//
// Five minutes is short against that and long against a connection. A
// stateless transport (design doc 0118) gives a client no session to hang
// a list on, so the ones that re-list per connection re-list constantly;
// five minutes covers a working session's worth of those while keeping a
// deploy visible inside a rollout. It also sits well inside the one
// release a renamed name keeps answering for (design doc 0088), so a
// client holding a stale list through an upgrade is holding names that
// still work.
const listTTL = 5 * time.Minute

// answerHowLongAListingHolds sets the TTL hint on the three listings
// whose content is fixed when the server is built, and leaves everything
// else alone.
//
// It is middleware because the SDK has no server option for this and
// fills the field in itself on the way out — with zero, which the
// protocol reads as "immediately stale". That default is an answer
// ochakai was giving without having decided it, and this is the decision.
//
// resources/read is deliberately not here. It carries the same field and
// must keep answering zero: a concept is knowledge, it changes when
// somebody writes it, and a client that cached one would be showing a
// reader a document the base no longer holds.
func answerHowLongAListingHolds(ttl time.Duration) mcp.Middleware {
	ms := int(ttl.Milliseconds())
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			// A paginated listing is not a whole answer, so it does not
			// get a lifetime — the cursor is the client's place in a
			// walk, not something to hold. ochakai's listings fit in one
			// page, so this is a guard rather than a case that arises.
			switch r := res.(type) {
			case *mcp.ListToolsResult:
				if r.NextCursor == "" {
					r.TTLMs = ms
				}
			case *mcp.ListResourcesResult:
				if r.NextCursor == "" {
					r.TTLMs = ms
				}
			case *mcp.ListResourceTemplatesResult:
				if r.NextCursor == "" {
					r.TTLMs = ms
				}
			}
			return res, err
		}
	}
}
