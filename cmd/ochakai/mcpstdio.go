// `ochakai mcp-stdio`: bridge a stdio-only MCP client to the selected
// server's /mcp, carrying the caller's own Google identity (design doc
// 0038). ochakai's MCP is streamable HTTP only, so a client that speaks
// nothing but stdio had no way in — and against Cloud Run no client can
// mint the ID token the request needs. This process is the missing half:
// it listens on no port, holds no state, and copies JSON-RPC messages
// between the two transports without interpreting them.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

func cmdMCPStdio(ctx context.Context, args []string) error {
	fs, target := newFlagSet(
		"Usage: ochakai mcp-stdio [flags]\n\nSpeak MCP over stdin/stdout, forwarding every message to the\nselected server's /mcp. For clients that cannot open an HTTP MCP\nendpoint themselves, and for any client talking to Cloud Run, where\nthe request needs a Google ID token this resolves for you (the same\nway every other client command does) — so the client itself is\nconfigured with no credentials.\n\nstdout carries the protocol and nothing else; diagnostics go to\nstderr. Run it as the client's command, not by hand.",
		"  ochakai mcp-stdio\n  ochakai mcp-stdio --url https://your-service.run.app\n\n  # Claude Desktop / any client taking a command, in its JSON config:\n  #   \"ochakai\": { \"command\": \"ochakai\", \"args\": [\"mcp-stdio\"] }\n  claude mcp add ochakai -- ochakai mcp-stdio\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		fs.Usage()
		return errReported
	}

	c, err := newClient(ctx, *target)
	if err != nil {
		return err
	}
	// Fail fast and say so on stderr. A stdio client usually surfaces a
	// broken bridge as "the server has no tools", which is a long way
	// from "you are not authenticated".
	identity, auth, err := c.Identity()
	if err != nil {
		return err
	}
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("server %s is not reachable: %w", *target, err)
	}
	fmt.Fprintf(os.Stderr, "ochakai mcp-stdio: stdin/stdout → %s/mcp as %s (%s)\n", *target, identity, auth)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return bridgeStdio(ctx, *target, c.TokenSource())
}

// bridgeStdio pumps JSON-RPC messages between this process's stdin/stdout
// and the remote /mcp endpoint until either side ends.
//
// It deliberately copies messages rather than reconstructing them through
// an MCP client and server pair. Enumerating the remote's tools and
// re-registering them locally would put a second copy of every tool
// schema here, to be updated whenever one gains an argument (design doc
// 0038 §2.2). Copying means initialize, notifications, resources and
// anything added later pass through a bridge that never heard of them.
//
// tokens is nil for a plain-http development server, which is then
// addressed without credentials — the same rule `ochakai ui` follows.
func bridgeStdio(ctx context.Context, target string, tokens oauth2.TokenSource) error {
	remote := &mcp.StreamableClientTransport{
		Endpoint:   target + "/mcp",
		HTTPClient: authedHTTPClient(tokens),
	}
	return bridge(ctx, &mcp.StdioTransport{}, remote)
}

// authedHTTPClient returns a client that attaches a fresh Google ID token
// to every request, or the default client when there is no token source.
func authedHTTPClient(tokens oauth2.TokenSource) *http.Client {
	if tokens == nil {
		return http.DefaultClient
	}
	return &http.Client{Transport: &oauth2.Transport{Source: tokens, Base: http.DefaultTransport}}
}

// bridge connects both transports and copies messages between them in
// each direction, returning when either direction ends. Transports rather
// than concrete types so the test can drive it without a process or a
// socket.
func bridge(ctx context.Context, local, remote mcp.Transport) error {
	rconn, err := remote.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to the server's MCP endpoint: %w", err)
	}
	defer rconn.Close()

	lconn, err := local.Connect(ctx)
	if err != nil {
		return fmt.Errorf("opening the stdio transport: %w", err)
	}
	defer lconn.Close()

	// Whichever direction ends first ends the session: a client that
	// closed stdin is gone, and a server that dropped the connection
	// cannot be served from here. Closing both unblocks the other pump.
	errs := make(chan error, 2)
	go func() { errs <- pump(ctx, lconn, rconn) }()
	go func() { errs <- pump(ctx, rconn, lconn) }()

	err = <-errs
	lconn.Close()
	rconn.Close()
	<-errs
	return err
}

// pump copies messages from src to dst until src ends. A clean end of
// input is not an error; neither is the cancellation that Ctrl-C causes.
func pump(ctx context.Context, src, dst mcp.Connection) error {
	for {
		msg, err := src.Read(ctx)
		if err != nil {
			if isEnd(ctx, err) {
				return nil
			}
			return fmt.Errorf("reading from the MCP transport: %w", err)
		}
		if err := dst.Write(ctx, msg); err != nil {
			if isEnd(ctx, err) {
				return nil
			}
			return fmt.Errorf("writing to the MCP transport: %w", err)
		}
	}
}

// isEnd reports whether an error means the session ended rather than
// failed: end of input, a transport already closed by the other pump, or
// the context going away on Ctrl-C.
func isEnd(ctx context.Context, err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.Canceled) ||
		ctx.Err() != nil
}
