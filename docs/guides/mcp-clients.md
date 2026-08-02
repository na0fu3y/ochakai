# Connecting an MCP client

ochakai serves MCP at `/mcp` over streamable HTTP. Two facts decide what
you put in a client's config, and neither is a preference:

**Where the server is.** Against Cloud Run, every request has to carry a
Google ID token that no MCP client mints, so the connection goes through
`ochakai mcp-stdio` — it speaks MCP on stdin/stdout, resolves your
identity the way every other client command does, and forwards each
JSON-RPC message untouched (design doc
[0039](../design/0039-mcp-stdio-bridge.md)). The client ends up
configured with no credentials, because there are none to configure.
Locally that requirement disappears and the URL works directly.

**What the client can open.** Some clients only launch a command and talk
over stdio. For those, the bridge is the answer whatever the server is.

So: **local server, capable client → the URL. Anything else → the
bridge** — which has prerequisites of its own, listed
[below](#what-the-bridge-needs) before any of the configs.

| Client | Local `docker compose` | Cloud Run | Config lives in |
|---|---|---|---|
| [Claude Code](#claude-code)* | URL | bridge | `.mcp.json`, or `claude mcp add` |
| [Claude Desktop](#claude-desktop) | bridge | bridge | `claude_desktop_config.json` |
| [Cursor](#cursor) | URL | bridge | `~/.cursor/mcp.json` or `.cursor/mcp.json` |
| [VS Code](#vs-code) | URL | bridge | `.vscode/mcp.json` |
| [Windsurf](#windsurf) | URL | bridge | `~/.codeium/windsurf/mcp_config.json` |
| [Cline](#cline) | URL | bridge | its own settings file — use the panel |
| [Zed](#zed) | URL | bridge | `~/.config/zed/settings.json` |
| [Gemini CLI](#gemini-cli) | URL | bridge | `~/.gemini/settings.json` |
| [Hosted assistants](#clients-that-cannot-reach-your-deployment) | — | — | not reachable, by design |

The local URL below is `http://localhost:8080/mcp`, which is what
`deploy/compose.yaml` gives you.

\* Claude Code is the one exception to this table: it also has a shell,
and the [recommended path there is the CLI](#claude-code), not MCP at
all.

Claude Code and the bridge are what this project exercises. The rest is
transcribed from each client's own documentation as of July 2026 and
marked where nobody here has run it — a config that turns out to be wrong
is worth an issue.

## What an agent gets

| Tool | Description |
|---|---|
| `get_context` | The one call before answering a data question: full concepts behind the top hits, links expanded both ways |
| `search_concepts` | Cross-type search; verified concepts rank higher |
| `get_concept` | Fetch one concept as an OKF document, with its links and file metadata |
| `get_file` | Fetch a file attached to a concept (dashboard screenshots, ER diagrams, seeds files) |
| `put_concept` | Write learnings back — creates if the id is free, replaces if it is taken; every change is kept as a revision |
| `delete_concept` | Soft-delete (history retained) |
| `get_concept_usage` | Usage totals per concept — draft-promotion evidence, staleness signal |
| `report_outcome` | Report worked/failed after acting on knowledge — failed reports flag verified concepts for re-verification |

Every one of these is a knowledge operation: ochakai never executes SQL and
never calls an LLM. `compile_sql` — deterministic SQL generation from a
semantic model — existed until 0.13.0 and was retired (design doc
[0028](../design/0028-retire-compile-sql.md)): what an agent actually needs
is the verified query and the caveat around it, and both arrive from
`get_context`.

Every concept is also an **MCP resource** addressable by its canonical URI —
`ochakai://` plus its id (the concept's path), e.g. `ochakai://metrics/revenue`
or `ochakai://queries/sales/top-customers`. Clients that support resource
references (`@`-mentions) can pull a concept in as an OKF document —
frontmatter and body — without a tool call; discovery stays with
`get_context`/`search_concepts`. Read tools carry `readOnly` annotations and
`delete_concept` a `destructive` one, so client auto-approval policies work
without parsing descriptions.

The tool count is a budget, not a mirror of REST: schemas are paid for out of
the agent's context window, so the REST API is a superset — bulk export, the
human-facing reads, file writes, and no bulk import at all (design doc
[0015 §3.1-3.2](../design/0015-surface-consistency.md)). For an agent with a
shell, the [CLI](../cli.md) covers everything and costs no schema.

## What the bridge needs

Every config below that launches a command needs `ochakai` on the
client's `PATH`. Against Cloud Run it needs two more things, and neither
is present on a machine by default:

- **The `gcloud` CLI**, which is where the ID token comes from. The
  bridge tries service-account ADC first and otherwise shells out to
  `gcloud auth print-identity-token` — a user's application-default
  credentials cannot mint the audience-bound token Cloud Run wants, so
  on a personal machine it is the `gcloud` path that runs.
- **`gcloud auth login` having happened**, as a principal holding
  `roles/run.invoker` on the service.

The CLI resolves the same token the same way when you point it straight
at a Cloud Run URL (`ochakai use https://your-service.run.app`) — this
list is really what reaching Cloud Run needs, bridge or not.

"The client is configured with no credentials" is a claim about the
config file, not about the machine. The credential exists; it lives in
your gcloud session instead of in JSON, which is what keeps it out of
the client's config and off disk in any form ochakai has to rotate.

`ochakai whoami` is the check. It prints which server the CLI targets,
as whom, and whether it answers — so run it in a terminal before editing
any config below, because a client that launches a bridge with no
identity usually reports it as "the server has no tools".

None of this applies to a local `docker compose` server, which takes no
token at all.

## Claude Code

**Recommended: the CLI, not MCP.** Claude Code has a shell, so the CLI's
tool schemas cost the agent no context — `--help` is read on demand —
and against Cloud Run it needs no proxy or bridge process, because it
resolves the ID token itself. See the README's
[Connect an agent](../../README.md#connect-an-agent).

### If you want MCP tools instead

Against a local server:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

Against Cloud Run, the bridge:

```sh
claude mcp add ochakai -- ochakai mcp-stdio
```

`-s user` puts it in your user config instead of the project's; the
default scope is local to the project directory. `-s project` writes
`.mcp.json`, which is the committed form — this repository's own
`.mcp.json` targets its local `docker compose` server and connects
automatically once that's running.

## Claude Desktop

The desktop app's config file takes a command, not a URL, so the bridge
is the route in both cases:

```json
{
  "mcpServers": {
    "ochakai": { "command": "ochakai", "args": ["mcp-stdio"] }
  }
}
```

The file is at `~/Library/Application Support/Claude/claude_desktop_config.json`
on macOS and `%APPDATA%\Claude\claude_desktop_config.json` on Windows —
Settings → Developer → Edit Config opens it. Restart the app after
editing.

Use an absolute path for `command` (`which ochakai`). A desktop app does
not inherit the `PATH` your shell has, which is the usual reason a config
that looks right produces no tools.

Against Cloud Run this config does nothing until the machine has gcloud
and a login — [what the bridge needs](#what-the-bridge-needs). There is
no way to supply that from inside the app, and no bundle or installer
that removes it.

Add `--url` when the server is not the one `ochakai use` selected:

```json
{
  "mcpServers": {
    "ochakai": {
      "command": "/usr/local/bin/ochakai",
      "args": ["mcp-stdio", "--url", "https://your-service.run.app"]
    }
  }
}
```

The app's **custom connector** UI takes a URL rather than a command, but
that connection is opened from Anthropic's infrastructure, not your
machine — see [below](#clients-that-cannot-reach-your-deployment).

## Cursor

`~/.cursor/mcp.json` for every project, `.cursor/mcp.json` for one:

```json
{
  "mcpServers": {
    "ochakai": { "url": "http://localhost:8080/mcp" }
  }
}
```

Cloud Run: replace the `url` entry with the bridge form from
[Claude Desktop](#claude-desktop) above — Cursor uses the same
`mcpServers` shape.

For a local server there is also an install link, which encodes exactly
the config above:

```markdown
[![Add to Cursor](https://cursor.com/deeplink/mcp-install-dark.svg)](https://cursor.com/install-mcp?name=ochakai&config=eyJ1cmwiOiJodHRwOi8vbG9jYWxob3N0OjgwODAvbWNwIn0%3D)
```

To point one at your own deployment, base64 the server object alone —
`{"url":"https://your-service.run.app/mcp"}` — and note that this only
helps for a server the client can reach unauthenticated.

## VS Code

`.vscode/mcp.json` in the workspace, or the user-level file that
**MCP: Open User Configuration** opens. The key is `servers`, not
`mcpServers`:

```json
{
  "servers": {
    "ochakai": { "type": "http", "url": "http://localhost:8080/mcp" }
  }
}
```

Cloud Run, same file:

```json
{
  "servers": {
    "ochakai": { "type": "stdio", "command": "ochakai", "args": ["mcp-stdio"] }
  }
}
```

Install links for the local case (the second is for Insiders):

```markdown
[![Install in VS Code](https://img.shields.io/badge/VS_Code-0098FF?style=flat-square&logo=visualstudiocode&logoColor=white)](https://vscode.dev/redirect/mcp/install?name=ochakai&config=%7B%22type%22%3A%22http%22%2C%22url%22%3A%22http%3A%2F%2Flocalhost%3A8080%2Fmcp%22%7D)
[![Install in VS Code Insiders](https://img.shields.io/badge/VS_Code_Insiders-24bfa5?style=flat-square&logo=visualstudiocode&logoColor=white)](https://vscode.dev/redirect/mcp/install?name=ochakai&config=%7B%22type%22%3A%22http%22%2C%22url%22%3A%22http%3A%2F%2Flocalhost%3A8080%2Fmcp%22%7D&quality=insiders)
```

MCP configuration moved out of `settings.json` in VS Code 1.102 and
existing concepts were migrated; if you find a `"mcp"` key in your
settings, that is where it came from.

## Windsurf

`~/.codeium/windsurf/mcp_config.json`. The URL field is `serverUrl`, not
`url`:

```json
{
  "mcpServers": {
    "ochakai": { "serverUrl": "http://localhost:8080/mcp" }
  }
}
```

Cloud Run: the bridge form, as in [Claude Desktop](#claude-desktop).
Untested here.

## Cline

Cline's own documentation and its source disagree about where the
settings file lives, so open it from the panel — **MCP Servers →
Configure MCP Servers** — rather than by path. `type` is required and
misspelling it silently falls back to SSE:

```json
{
  "mcpServers": {
    "ochakai": {
      "type": "streamableHttp",
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

Cloud Run: the bridge form. Untested here.

## Zed

`~/.config/zed/settings.json`, under `context_servers`:

```json
{
  "context_servers": {
    "ochakai": { "url": "http://localhost:8080/mcp" }
  }
}
```

Cloud Run: `{ "command": "ochakai", "args": ["mcp-stdio"] }` in the same
place. Untested here.

## Gemini CLI

`~/.gemini/settings.json`, or `.gemini/settings.json` per project. The
field for streamable HTTP is **`httpUrl`** — `url` in this client means
SSE, and pointing it at `/mcp` fails in a way that does not say so:

```json
{
  "mcpServers": {
    "ochakai": { "httpUrl": "http://localhost:8080/mcp" }
  }
}
```

Or `gemini mcp add -t http ochakai http://localhost:8080/mcp`. Cloud Run:
the bridge form. Untested here.

## A URL that works against Cloud Run

If a client insists on a URL and the server is on Cloud Run, `ochakai ui`
is the local proxy that gives it one. It authenticates with your identity
and exposes `/mcp` alongside the web UI, bound to loopback:

```sh
ochakai ui        # http://127.0.0.1:8098/mcp
```

`gcloud run services proxy` is the third way — a plain HTTP tunnel with
no MCP-specific behavior. The deploy guide uses it for verifying a
deployment over `curl` ([§3](../../deploy/cloudrun/README.md)), not for
connecting a client; prefer the bridge or `ochakai ui` above for that.

## Clients that cannot reach your deployment

ChatGPT's connectors, the OpenAI Responses API, and Claude Desktop's
custom-connector UI all open the connection **from the vendor's
infrastructure**, not from your machine. Neither `localhost` nor an
IAM-restricted Cloud Run service is reachable from there, and no config
syntax changes that.

This is the access model working, not a gap: reachability *is* the
authorization (design doc [0002](../design/0002-authn-authz.md)), so
making a deployment reachable by a third party's servers means making it
reachable — running ochakai publicly invokable is a misconfiguration
rather than a deployment mode. A hosted assistant that also runs local
code (Claude Desktop through the bridge, Claude Code, Cursor) is the
supported path.

## When it does not connect

- **No tools appear, and no error.** The client launched the bridge and
  the bridge could not reach a server, or `ochakai` was not on the
  client's `PATH`. Run `ochakai whoami` in a terminal — it prints which
  server the CLI is talking to, as whom, and whether it answers. Then use
  an absolute path in `command`.
- **Tools appear and every call fails.** Usually identity: against Cloud
  Run the bridge needs `gcloud auth login` (or ADC) to have happened, and
  the caller needs `roles/run.invoker`. The deploy guide's
  [§7](../../deploy/cloudrun/README.md) covers the Google-side symptoms.
- **The server answers curl but not the client.** Check the path — `/mcp`,
  not the root. `GET /` on an ochakai server prints the endpoints it
  serves.
- **Searches come back empty on a base that has concepts.** Not a
  connection problem. Japanese knowledge bases want embeddings on; see
  [architecture's search section](../architecture.md#search).

`ochakai mcp-stdio` writes diagnostics to stderr and keeps stdout for the
protocol, so a client that surfaces server logs will show you what it
saw.
