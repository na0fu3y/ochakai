<!-- Generated from the CLI's own help. Do not edit by hand:
     go test ./cmd/ochakai -run TestCLIReferenceIsCurrent -update -->

# CLI reference

`ochakai` is a thin client of the REST API — every client command
below is one HTTP call against the server named by `--url`, `$OCHAKAI_URL`,
or the `ochakai use` selection, in that order (design docs
[0004](design/0004-cli.md), [0007](design/0007-api-only-cli.md)). `serve`
and `serve-ui` are the exception: they are the deployed services, and they
take their configuration from the environment rather than from flags — the
README's [Configuration](../README.md#configuration) table lists it.

The same text is what `ochakai <command> -h` prints, so nothing here can
disagree with the binary you are running.

## Commands

```
ochakai — context provider for data agents

Client commands (talk to a server; --url > $OCHAKAI_URL > "use" selection):
  use [name | url]        pick the server for later commands (saved locally)
  whoami                  print target server, identity, and reachability
  search [query]          search knowledge; verified entries rank higher
  browse [prefix]         list one level of the ID hierarchy (folder view)
  context <question>      the one-call read before a data question (full entries)
  get <id>                print one entry as an OKF document
  create [id] [-f file]   create an entry from OKF markdown or JSON
  update <id>             replace an entry (every change kept as a revision)
  verify <id>             record a verification (re-affirms a verified entry too)
  delete <id>             soft-delete an entry (history retained)
  purge <id>              hard-delete a soft-deleted entry, freeing its id
  reembed                 embed entries missing a vector for the current model
  move <id> <new-id>      move (rename) an entry; references are rewritten
  attach <id> <file...>   attach files to an entry (png/jpeg/webp/pdf/text)
  detach <id> <name>      remove an attachment
  usage <id>              show usage totals (search hits, fetches, outcomes)
  stats                   show the loop for the whole base (review queues, gaps)
  report <id> <outcome>   report an outcome: worked | failed (--note for why)
  revisions <id>          list an entry's change history (newest first)
  log [path]              print the history under a path as OKF's log.md
  backlinks <id>          list entries whose links point at this one
  export <dir | ->        download the knowledge base as an OKF bundle
  import <dir | tgz | ->  upload an OKF bundle (any producer's, not just ours)
  ui                      serve the web UI locally, acting as you (no deploy)
  mcp-stdio               speak MCP on stdin/stdout, forwarding to the server
                          (for clients that cannot open an HTTP MCP endpoint)

Server commands (run as deployed services, configured by environment):
  serve                   start the MCP + REST server (runs next to the database)
  serve-ui                serve the team web UI, proxying to $OCHAKAI_URL as the
                          service identity (same image as serve: --args=serve-ui)

  version                 print the version
  completion <shell>      print a completion script (zsh, bash, fish)

Run "ochakai <command> -h" for flags and examples.
```

## ochakai attach

```
Usage: ochakai attach [flags] <id> <file...>

Attach files to a knowledge entry (any file, up to 5 MiB each — the
media type is sniffed from the bytes). A file of the same name is
replaced (the change is kept as a revision). Reference the file from the entry's body so its
caption is searchable and it survives OKF export/import — the hint
printed after attaching shows the canonical relative link. Requires
the server to have GCS configured (OCHAKAI_GCS_BUCKET).

Flags:
  -json
    	print the attachment metadata as JSON
  -name string
    	attachment name (default: the file's basename; single file only)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai attach insights/reading-revenue weekly.png
  ochakai attach tables/orders seeds.txt
  ochakai attach tables/orders er-diagram.png --name schema.png
```

## ochakai backlinks

```
Usage: ochakai backlinks [flags] <id>

List live entries whose links point at this entry, most recently
updated first — the reverse edge the web UI shows as "linked from"
(context already follows it when packing companions).
Output: uri, status, title — description (one entry per line).

Flags:
  -json
    	print the raw JSON response
  -limit int
    	max entries (server default 20, max 100)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai backlinks metrics/revenue
  ochakai backlinks metrics/revenue --json | jq '.entries[].id'
```

## ochakai browse

```
Usage: ochakai browse [flags] [prefix]

List one level of the ID hierarchy (the folder view of design docs
0014 and 0017, the CLI counterpart of the web UI's Browse tab).
Without an argument, the top-level directories with their entry
counts; with a prefix, the subdirectories and entries directly under
it. Directories print as "name/	count", entries as
"segment	type	status	title". Rejected entries are hidden, as in search.

Flags:
  -json
    	print the raw JSON response
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai browse
  ochakai browse queries
  ochakai browse ga4/tables
```

## ochakai completion

```
Usage: ochakai completion <zsh|bash|fish>

Print a shell completion script. Load it with:

  zsh:   source <(ochakai completion zsh)    # ~/.zshrc, after compinit
  bash:  source <(ochakai completion bash)   # ~/.bashrc
  fish:  ochakai completion fish | source    # ~/.config/fish/config.fish

Or install it as a file the shell picks up by itself (what package
managers do):

Flags:

Examples:
  ochakai completion zsh > "${fpath[1]}/_ochakai"
  ochakai completion fish > ~/.config/fish/completions/ochakai.fish
```

## ochakai context

```
Usage: ochakai context [flags] <question>

Gather what to read before answering a data question, in one call:
the full entries behind the top search hits (verified entries rank
higher), expanded one hop through links so the insight explaining a
metric travels with it. Markdown on stdout, ready for an agent's
context window. No hits print nothing (exit 0).

Flags:
  -budget int
    	cap the response at ~this many bytes (0 = no cap); the rendered output stops printing entries, --json asks the server to cap and list what did not fit under "outline"
  -fm key=value
    	filter by a frontmatter key=value, exactly (repeatable, AND-ed) — for keys with no flag of their own, a producer's or a later OKF version's; a value spelling a number or a boolean matches the typed one too (--fm required=true); refused for type, status, tags, sources and stale_after, which have filters of their own that answer from a column instead
  -json
    	print the raw JSON response
  -limit int
    	max full entries (server default 5, max 20)
  -min-score float
    	drop hits scoring below this; scores depend on the server's search mode (matched-fragment weight plus boosts vs RRF rank fusion), so calibrate before use (0 = off)
  -prefix path
    	only entries under this path, e.g. teams/growth (repeatable, OR-ed); scopes the search, not the links it expands
  -status value
    	filter by status: draft|stable|deprecated (repeatable)
  -tag value
    	filter by tag (repeatable)
  -trust value
    	filter by who confirmed the entry: unverified|machine-confirmed|human-reviewed (repeatable, OR-ed) — independent of --status, which is the lifecycle value
  -type value
    	filter by type: Metric|Attested Computation|Skill|Playbook|Insight|Policy|Glossary Term|BigQuery Dataset|BigQuery Table|API Endpoint|Reference, or any custom type (repeatable)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai context "why did revenue drop in March?"
  ochakai context "monthly revenue" --type 'Attested Computation' --trust human-reviewed --json
  ochakai context "$PROMPT" --budget 4000   # hooks: cap the injected bytes
  ochakai context "activation rate" --prefix teams/growth --prefix company
```

## ochakai create

```
Usage: ochakai create [flags] [id]

Create a knowledge entry from -f or stdin. Input is an OKF document
(--- frontmatter with type, markdown body — the format `ochakai get`
prints; title is optional, the id's last segment is the display name
when it is absent) or JSON (see api/openapi.yaml). The id is the
entry's path; pass it as the argument (it overrides an id in the
input, and OKF documents carry none — the path is the id). Entries
default to draft; provenance is recorded from your Google identity.

Flags:
  -f string
    	input file (default: stdin)
  -json
    	print the created entry as JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai get insights/revenue-seasonality | sed s/40%/45%/ | ochakai create insights/revenue-seasonality-v2
  ochakai create runbook/restore -f entry.md
```

## ochakai delete

```
Usage: ochakai delete [flags] <id>

Soft-delete a knowledge entry (history is retained server-side).

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai delete terms/obsolete-kpi
```

## ochakai detach

```
Usage: ochakai detach [flags] <id> <name>

Remove an attachment from a knowledge entry (the change is kept as a
revision; content-addressed bytes stay referenced by history).

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai detach insights/reading-revenue weekly.png
```

## ochakai export

```
Usage: ochakai export [flags] <dir | ->

Download the whole knowledge base as an OKF bundle (markdown + YAML
frontmatter) into dir, or stream the tar.gz to stdout with "-".
Your knowledge is yours.

Flags:
  -no-attachments
    	export the markdown only, skipping attachment files
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai export ./knowledge
  ochakai export - > ochakai-okf.tar.gz
  ochakai export --no-attachments - > entries.tar.gz   # bytes are in GCS; copy them from there
```

## ochakai get

```
Usage: ochakai get [flags] <id>

Print one knowledge entry as an OKF document (YAML frontmatter +
markdown body), and nothing else, so the output round-trips through
`ochakai update`. Who wrote and confirmed it is an observation rather
than part of the document, so it goes to stderr, as attachment metadata
does; --download saves the attachment files themselves (an agent can
then read them from disk). --json prints the whole read instead: the
document, the projection under .summary, and the provenance under
.observed.

Flags:
  -download string
    	save the entry's attachments into this directory
  -json
    	print the whole read as JSON (document, summary, observed) instead of the document alone
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai get metrics/revenue
  ochakai get queries/sales/monthly-revenue --json | jq -r '.summary.content_hash'
  ochakai get insights/reading-revenue --download ./img
```

## ochakai import

```
Usage: ochakai import [flags] <dir | file.tar.gz | ->

Import an OKF bundle (a directory of markdown + YAML frontmatter, or
a tar.gz of one; "-" reads the tar.gz from stdin). The inverse of
`ochakai export`: each path names its entry (the path minus .md is
the id), the frontmatter type key names the type (required — a
markdown file without one is not a concept, and is kept as a file),
reserved index.md / log.md files are skipped, keys the format does
not define are kept as written, and existing entries are replaced (kept as revisions; entries identical
to what is stored are left untouched and reported as unchanged;
entries the server rejects as invalid — e.g. one whose type is not a
single line — are skipped and reported).
Files referenced by an entry's body markdown links become its
attachments, wherever they sit in the bundle (their location is
preserved for re-export); unreferenced data files inside an entry's
directory (<id>/<name>) attach to that entry. Everything else the
bundle carried is written at the path it arrived at — what enters
leaves, so nothing is dropped for belonging to no entry. The packed shape is
the structure: an archive wrapped in a single directory imports
under that directory — the bundle keeps its own namespace. Works
with any OKF bundle, not just ochakai's own.
A file that cannot be stored at all — empty, oversized, or at a path
ochakai cannot address — is skipped; a value read differently than
it was written is a note and the entry still imports. Both are
reported and neither fails the command, because a consumer takes the
document rather than rejecting it. --strict is the opposite posture,
for a sync nobody watches: a bundle that is not read exactly as
written fails, and the counts land in the summary line either way.

Flags:
  -dry-run
    	parse and list what would be written, write nothing
  -strict
    	refuse a bundle that is not read exactly as written: any note or skip fails the command instead of being reported. Parse-time ones are found before anything is written, so a strict import either lands whole or writes nothing
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai import ./knowledge
  ochakai import ga4-bundle.tar.gz --dry-run
  ochakai import ./knowledge --dry-run --strict   # gate a CI sync on a clean parse
  ochakai export - | OCHAKAI_URL=https://other ochakai import -
```

## ochakai log

```
Usage: ochakai log [flags] [path]

Print the update history under a path as OKF's log.md (SPEC §9):
date-grouped, newest first. With no path, the whole bundle.

It is generated from the revision ledger, so it says the same thing
`ochakai revisions` does — in the format a bundle carries, which is
what makes the history portable.

Flags:
  -limit int
    	max entries (default 1000)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai log
  ochakai log metrics
  ochakai log metrics/revenue --limit 20
```

## ochakai mcp-stdio

```
Usage: ochakai mcp-stdio [flags]

Speak MCP over stdin/stdout, forwarding every message to the
selected server's /mcp. For clients that cannot open an HTTP MCP
endpoint themselves, and for any client talking to Cloud Run, where
the request needs a Google ID token this resolves for you (the same
way every other client command does) — so the client itself is
configured with no credentials.

stdout carries the protocol and nothing else; diagnostics go to
stderr. Run it as the client's command, not by hand.

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai mcp-stdio
  ochakai mcp-stdio --url https://your-service.run.app

  # Claude Desktop / any client taking a command, in its JSON config:
  #   "ochakai": { "command": "ochakai", "args": ["mcp-stdio"] }
  claude mcp add ochakai -- ochakai mcp-stdio
```

## ochakai move

```
Usage: ochakai move [flags] <id> <new-id>

Move (rename) a knowledge entry to a new id. Revisions, usage, and
attachments follow, and inbound references (link targets, and
a `model` key where a document carries one) are rewritten so nothing
breaks.

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai move insights/revenue-seasonality insights/sales/revenue-seasonality
```

## ochakai purge

```
Usage: ochakai purge [flags] <id>

Hard-delete an already soft-deleted entry: the entry, its revisions,
usage, and attachment metadata are erased and the id is freed for a
move. History is gone — `ochakai delete` first, then purge. A live
entry is refused.

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai delete terms/obsolete-kpi
  ochakai purge terms/obsolete-kpi
```

## ochakai reembed

```
Usage: ochakai reembed [flags]

Embed entries that have no vector for the configured model — entries
written before semantic search was enabled, or before the model was
changed. Runs bounded passes until nothing is left, so it can be started
once and left alone.

Flags:
  -json
    	print the raw JSON response of each pass
  -limit int
    	max entries to embed per pass (server default 200)
  -once
    	run a single pass instead of continuing until nothing is left
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai reembed
  ochakai reembed --limit 50   # smaller passes, e.g. behind a short request timeout
  ochakai reembed --once       # one pass, then report what is left
```

## ochakai reject

```
Usage: ochakai reject [flags] <id>

Record that an entry was reviewed and not accepted, with the reason.
Rejected entries are hidden from search unless asked for
(`search --rejected`), which is how an agent checks whether a proposal
was already turned down before making it again.
It does not edit the entry: the lifecycle status and the ETag stay put.
A rejection is this instance's ruling, so an exported bundle carries the
entry's real status rather than folding the ruling onto deprecated.
Use --lift to withdraw one.

Flags:
  -json
    	print the entry as JSON
  -lift
    	withdraw the rejection instead of recording one
  -note string
    	why it was not accepted — the next agent reads this before proposing again
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai reject metrics/bad-revenue --note "double-counts refunds; see policies/revenue-recognition"
  ochakai reject metrics/bad-revenue --lift
```

## ochakai report

```
Usage: ochakai report [flags] <id> <worked|failed>

Report whether acting on an entry gave a correct result — the last
edge of the write-back loop. After running an attested computation or SQL you
wrote from an entry, report worked or failed (say what went wrong with
--note); failed counts against verified entries flag them for
re-verification. Prints the entry's updated usage totals.

Flags:
  -json
    	print the updated usage totals as JSON
  -note string
    	context recorded with the report: what was run, what went wrong (max 2000 bytes)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai report queries/sales/monthly-revenue worked
  ochakai report queries/sales/monthly-revenue failed --note "joins dropped 2024 rows after schema change"
```

## ochakai revisions

```
Usage: ochakai revisions [flags] <id>

List an entry's change history, newest first: who changed it, how,
and when — the audit surface behind "every change kept as a
revision". Works for soft-deleted entries too. The whole entry as it
stood, as an OKF document, is in the JSON output (--json), so a diff
between two revisions is a text diff.

Flags:
  -json
    	print the raw JSON response (includes each revision's document)
  -limit int
    	max revisions (server default 50, max 200)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai revisions metrics/revenue
  ochakai revisions queries/sales/monthly-revenue --json | jq -r '.revisions[0].document'
```

## ochakai search

```
Usage: ochakai search [flags] [query]

Search the knowledge base; verified entries rank higher.
Output: score, uri, status, title — description (one hit per line).
With --sort verified_at the command lists by verification age instead
of searching (oldest first, never-verified last — the
canary feed); output leads with verified_at. With --sort usage it lists
by demand (most search_hits first, never-used oldest-first at the bottom
— the draft review feed); output leads with the search_hits count.
With --sort failed it lists entries whose failure reports (report_outcome
failed) are still unanswered, worst first — the re-verification feed;
output leads with the failed count. `ochakai verify` takes an entry out
of it, so a base that is kept up shows an empty feed.
With --sort stale_after it lists entries whose declared stale_after has
passed, most overdue first; output leads with that date. Verifying does
not empty this one — the date is the writer's declaration, so clearing it
means editing the entry to re-declare an expiry.
--source and --prefix are filters, not modes: both combine with a query
or with any --sort. --source narrows to the entries citing one resource
(the reverse of sources[].resource); --prefix narrows to the entries
living under a path, which is how a team's own knowledge is told apart
from the company-wide vocabulary.

Flags:
  -fm key=value
    	filter by a frontmatter key=value, exactly (repeatable, AND-ed) — for keys with no flag of their own, a producer's or a later OKF version's; a value spelling a number or a boolean matches the typed one too (--fm required=true); refused for type, status, tags, sources and stale_after, which have filters of their own that answer from a column instead
  -json
    	print the raw JSON response
  -limit int
    	max results (server default 10, max 50; with --sort: 100, max 1000)
  -prefix path
    	only entries under this path, e.g. teams/growth — matched on segment boundaries, so it does not reach teams/growth-archive (repeatable, OR-ed)
  -rejected
    	only entries a human turned down — how you check whether a proposal was already rejected. Without it, rejected entries stay out of results
  -sort string
    	list instead of search: "verified_at" = by verification age (oldest first), "usage" = by demand (most search_hits first), "failed" = by failed outcome reports (re-verification feed), "stale_after" = past their declared expiry, most overdue first
  -source resource
    	only entries citing this resource (exact match against sources[].resource) — what derives from one piece of material
  -status value
    	filter by status: draft|stable|deprecated (repeatable)
  -tag value
    	filter by tag (repeatable)
  -trust value
    	filter by who confirmed the entry: unverified|machine-confirmed|human-reviewed (repeatable, OR-ed) — independent of --status, which is the lifecycle value
  -type value
    	filter by type: Metric|Attested Computation|Skill|Playbook|Insight|Policy|Glossary Term|BigQuery Dataset|BigQuery Table|API Endpoint|Reference, or any custom type (repeatable)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai search "gross margin" --type Metric --type 'Glossary Term' --trust human-reviewed
  ochakai search churn --json | jq -r '.hits[] | .id'
  ochakai search --sort verified_at --type 'Attested Computation' --trust human-reviewed --limit 100
  ochakai search --sort usage --status draft --limit 50   # review queue
  ochakai search --sort failed --trust human-reviewed     # re-verification queue
  ochakai search --sort stale_after                         # past their declared expiry
  ochakai search --source https://wiki.example/finance/revenue-recognition  # what cites this
  ochakai search 活性化 --prefix teams/growth --prefix company   # our scope and the shared one
```

## ochakai stats

```
Usage: ochakai stats [flags]

Show the improvement loop as the instance sees it: what the knowledge
base is made of now, what review did lately, what callers reported, and
what they searched for and did not find. `usage` measures one entry;
this measures the base.

One line per number, so it composes: cron it and diff the output, or
grep one line out of it for a prompt or a dashboard. The gap lines are
the questions that came back empty, most-asked first — the list of what
to write next.

Flags:
  -days int
    	how far back the flow numbers reach, 1-180 (default: 30; raw events are pruned after 180 days)
  -json
    	print JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai stats
  ochakai stats --days 7
  ochakai stats --json | jq .misses.queries
```

## ochakai ui

```
Usage: ochakai ui [flags]

Serve the web UI at http://127.0.0.1:<port> against the selected
server. API calls are proxied with your own Google identity (resolved
the same way as every other client command), so no deployment is
needed and your edits are recorded as human:<you>. The proxy also
exposes /mcp, so it doubles as an authenticated local MCP endpoint.
For a team-shared UI on Cloud Run, deploy `ochakai serve-ui`.

Flags:
  -port int
    	port to listen on (always bound to 127.0.0.1: whoever reaches the proxy acts as you) (default 8098)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai ui
  ochakai ui --port 9000
  claude mcp add --transport http ochakai http://127.0.0.1:8098/mcp
```

## ochakai update

```
Usage: ochakai update [flags] <id>

Replace a knowledge entry from -f or stdin (OKF document or JSON;
the id comes from the argument, the type from the input). Every
change is kept as a revision server-side. With --if-match the update
is conditional: it lands only if the entry still has the version you
read, and fails instead of overwriting someone else's edit.

Flags:
  -f string
    	input file (default: stdin)
  -if-match version
    	update only if the entry still has this version — its content hash (`ochakai get <id> --json` prints it as .summary.content_hash; a REST GET returns it as the ETag header); a stale version fails with a conflict instead of overwriting. Verifying or rejecting an entry does not move it: only an edit does
  -json
    	print the updated entry as JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai get metrics/revenue | $EDITOR /dev/stdin | ochakai update metrics/revenue
  ochakai update metrics/revenue -f revenue.md
  ochakai update metrics/revenue -f revenue.md --if-match "$(ochakai get metrics/revenue --json | jq -r .summary.content_hash)"
```

## ochakai usage

```
Usage: ochakai usage [flags] <id>

Show how often an entry was actually used: appeared in search results,
fetched individually, reported worked or failed — and when it was last
used. The measure of the write-back loop: evidence for promoting a
draft, and a staleness signal for verified entries nobody uses.

Flags:
  -json
    	print JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai usage queries/sales/monthly-revenue
  ochakai usage metrics/revenue --json
```

## ochakai use

```
Usage: ochakai use [flags] [name | url]

Pick the server later client commands talk to, saved to
~/.config/ochakai/config.json ($XDG_CONFIG_HOME honored;
%AppData%\ochakai on Windows).
With a URL: save it (named by --name, default its host) and switch.
With a name: switch to a saved server. With no argument: list.
--url and $OCHAKAI_URL always override the saved selection.

Flags:
  -name string
    	name to save the URL under (default: its host)

Examples:
  ochakai use http://localhost:8080 --name local
  ochakai use https://ochakai-prod.run.app --name prod
  ochakai use prod
```

## ochakai verify

```
Usage: ochakai verify [flags] <id>

Append a verification against the entry as it stands: you and the time
are added to its ledger. The first confirmation and the tenth re-check
are the same command, and re-checking is what takes an entry out of
both review feeds (--sort verified_at, --sort failed).
It does not edit the entry: the lifecycle status and the ETag stay put,
because confirming knowledge and publishing it are different acts. Use
`update` to move a draft to stable.
Verifying a rejected entry lifts the rejection.

Flags:
  -json
    	print the verified entry as JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai verify metrics/revenue
  ochakai verify metrics/revenue --json | jq -r '.observed.verified[-1].at'
```

## ochakai whoami

```
Usage: ochakai whoami [flags]

Print which server client commands target and where that choice came
from (--url / $OCHAKAI_URL / `ochakai use`), the identity your
credentials present (the server's actor resolution is authoritative),
and whether the server is reachable.

Flags:
  -json
    	print JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai whoami
  ochakai whoami --json
```
