<!-- Generated from the CLI's own help. Do not edit by hand:
     go test ./cmd/ochakai -run TestCLIReferenceIsCurrent -update -->

# CLI reference

`ochakai` is a thin client of the REST API — every client command
below is one HTTP call against the server named by `--url`, `$OCHAKAI_URL`,
or the `ochakai use` selection, in that order (design docs
[0067](design/0067-four-faces-and-what-they-decline.md) §2). `serve`
and `serve-ui` are the exception: they are the deployed services, and they
take their configuration from the environment rather than from flags —
[Requirements and configuration](configuration.md) (Japanese) lists it.

Every section is what `ochakai <command> -h` prints, so nothing here
can disagree with the binary you are running — except `search` and
`list`, which narrow the same way (design doc
[0068](design/0068-how-a-face-is-added-and-removed.md) §2) and point at
[Shared filters](#shared-filters) instead of repeating it; `-h` on
either command still prints its own copy in full.

## Commands

```
ochakai — context provider for data agents

Client commands (talk to a server; --url > $OCHAKAI_URL > "use" selection):
  use [name | url]        pick the server for later commands (saved locally)
  whoami                  print target server, identity, and reachability
  search <query>          search knowledge; verified concepts rank higher
  list [feed]             list a review feed or a reverse lookup, page by page
                          (usage, verified_at, failed, stale_after)
  browse [prefix]         list one level of the ID hierarchy (folder view)
  get <id>                print one concept as an OKF document (stderr notes
                          what links at it)
  put <path> [-f file]    write one object of the bundle: a concept from OKF
                          markdown or JSON at <id>, or a file at its own path
                          (every change kept as a revision)
  verify <id>             record a verification (re-affirms a verified concept too)
  reject <id>             record a rejection and why (--withdraw takes it back)
  delete <path>           remove one object: a concept by id (history retained)
                          or a file by the path it lives at
  purge <id>              hard-delete a soft-deleted concept, freeing its id
  reembed                 embed concepts missing a vector for the current model
  move <id> <new-id>      move (rename) a concept; references are rewritten
  usage <id>              show usage totals (search hits, fetches, outcomes)
  stats                   the whole loop: what is stored, what each queue holds,
                          what review did, what came back empty
  access [-f file]        show or replace the access policy: who may read and
                          write under which directory (administrators only)
  report <id> <outcome>   report an outcome: worked | failed (--note for why)
  revisions <id>          list a concept's change history (newest first)
  log [path]              print the history under a path as OKF's log.md
  export <dir | ->        download the knowledge base as an OKF bundle
  seed <file.json | ->    turn a warehouse's own schema listing into a bundle of
                          draft concepts (pipe into import; reads no warehouse)
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

## ochakai access

```
Usage: ochakai access [flags]

Show the access policy, or replace it with the document from -f or
stdin. Only an administrator (OCHAKAI_ADMINS) may do either.

Each rule grants one principal — human:<name>, process:<name>, or *
for every authenticated caller — the right to read under one directory,
to write there when may_write is true, and to edit the rules under it
when may_admin is. A may_admin caller reads and replaces only the rules
at or under the directories it administers; the rest are invisible and
survive its write. may_admin at the root is refused — who may edit the
whole policy is what OCHAKAI_ADMINS answers. Prefixes match on segment
boundaries, so sales covers sales/orders and does not cover
sales-legacy/orders; the empty prefix is the whole bundle. There are no
deny rules: what no rule grants is not readable, and a caller sees a
404 rather than a refusal, because whether something is there is the
boundary itself.

A deployment with no rules has no boundary — every caller reads and
writes everything, which is what ochakai does by default. Writing the
first rule turns the boundary on for everybody at once, so read the
policy back before you close the terminal.

The operations that take the bundle as a whole — export of the whole
base, reembed, and this command — stay with the administrators. Three
others do not. `stats`: a caller with grants reads their own numbers,
and the answer says which subtree it counted, though the unanswered
searches beside them are withheld, having no id to scope by. `export
--prefix`: anyone who may read a directory may take its archive, which
says which directory it is. And `move`, when everything its rewrite
touches — the concept, the destination, and everything that links at
the concept — is yours to write.

The policy is one document, replaced whole, so two editors who both
read it can each drop the other's rules. With --if-match the
replacement lands only if the policy still has the version you read,
and fails instead.

Flags:
  -f -
    	replace the policy with the JSON document in this file (- or unset with a pipe: stdin). Without it, the policy is printed
  -if-match version
    	replace the policy only if it still has this version (`ochakai access --json` prints it as .version; a REST GET returns it as the ETag header); a stale version fails with a conflict instead of dropping the rules somebody else added
  -json
    	print the policy as JSON — the same document -f takes back
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai access
  ochakai access --json > policy.json
  ochakai access -f policy.json
  ochakai access -f policy.json --if-match "$(jq -r .version policy.json)"
  echo '{"rules":[]}' | ochakai access -f -   # remove every boundary
```

## ochakai browse

```
Usage: ochakai browse [flags] [prefix]

List one level of the ID hierarchy (the folder view of design doc
0075 §2, the CLI counterpart of the web UI's Browse tab).
Without an argument, the top-level directories with their concept
counts; with a prefix, the subdirectories and concepts directly under
it. Directories print as "name/	count", concepts as
"segment	type	status	title", and the files in the directory as
"name	file	media-type	bytes". Rejected concepts are hidden, as in
search.

A level is a listing, so it pages: a page with more behind it prints
the way on to stderr, and passing that back with --cursor reads the
next one. The subdirectories ride on the first page only — they are a
grouping rather than a run, so there is no position in them to resume
from.

Flags:
  -cursor cursor
    	resume a level where the last page ended: the cursor the previous page printed, for the same prefix
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

## ochakai delete

```
Usage: ochakai delete [flags] <path>

Remove one object of the bundle: a knowledge concept, named by its id
(soft-delete — history is retained server-side), or a file, named by
the path it lives at (the removal is kept as a revision of the concept
that linked it; content-addressed bytes stay referenced by history).
A concept's address is <id>.md and an id is that path with the .md
filed off, so an argument carrying no filename extension is read as an
id — spell a dotted id with its .md to reach it.
With --if-match a concept is deleted only if it still has the version
you read, and the delete fails instead of removing someone else's edit.

Flags:
  -if-match version
    	delete only if the concept still has this version — its content hash (`ochakai get <id> --json` prints it as .summary.content_hash; a REST GET returns it as the ETag header); a stale version fails with a conflict instead of deleting
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai delete terms/obsolete-kpi
  ochakai delete insights/reading-revenue/weekly.png
```

## ochakai export

```
Usage: ochakai export [flags] <dir | ->

Download the whole knowledge base as an OKF bundle (markdown + YAML
frontmatter) into dir, or stream the tar.gz to stdout with "-".
Your knowledge is yours.

With --prefix it is one directory of the base instead, which anyone
who may read that directory can take. A subtree bundle says so in its
root index.md and in the name it streams under, because a part of a
base and the whole of one are otherwise the same shape — and the day
that matters is the day somebody restores it.

Flags:
  -no-files
    	export the markdown only, skipping file bytes
  -prefix directory
    	export one directory of the bundle rather than the whole base: the archive carries that subtree, says so in its root index.md, and is not a backup of the base. Anyone who may read the directory may take it; the whole base stays with the administrators
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai export ./knowledge
  ochakai export - > ochakai-okf.tar.gz
  ochakai export --prefix teams/growth - > growth.tar.gz   # one directory, not a backup of the base
  ochakai export --no-files - > concepts.tar.gz   # bytes are in GCS; copy them from there
```

## ochakai get

```
Usage: ochakai get [flags] <id>

Print one knowledge concept as an OKF document (YAML frontmatter +
markdown body), and nothing else, so the output round-trips through
`ochakai put`. Who wrote and confirmed it, the files beside it, and
what links at it (linked_from) are observations rather than part of
the document, so they go to stderr; --download saves the files themselves (an agent can
then read them from disk). --json prints the whole read instead: the
document, the projection under .summary, and the provenance under
.observed.

Flags:
  -download string
    	save the concept's files into this directory
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
`ochakai export`: each path names its concept (the path minus .md is
the id), the frontmatter type key names the type (required — a
markdown file without one is not a concept, and is kept as a file at
a renamed path, since `.md` is a concept's address and a note says
where it landed),
reserved index.md / log.md files are skipped, keys the format does
not define are kept as written, and existing concepts are replaced (kept as revisions; concepts identical
to what is stored are left untouched and reported as unchanged;
a document the server refuses as a concept — e.g. an Attested
Computation with no runtime — is not stored, and is reported by path
and reason; the bundle you imported from still has it, and the files
it pointed at are written anyway).
Files referenced by a concept's body markdown links become
attributed to it, wherever they sit in the bundle (their location is
preserved for re-export); unreferenced data files inside a concept's
directory (<id>/<name>) attribute to that concept the same way. Everything else the
bundle carried is written at the path it arrived at — what enters
leaves, so nothing is dropped for belonging to no concept. The packed shape is
the structure: an archive wrapped in a single directory imports
under that directory — the bundle keeps its own namespace. Works
with any OKF bundle, not just ochakai's own.
A file that cannot be stored at all — empty, oversized, or at a path
ochakai cannot address — is skipped; a value read differently than
it was written is a note and the concept still imports. A document
that says who generated or confirmed it is one of those: the keys
are kept as the document's own claim, under `received`, and never
become this instance's provenance — so a bundle from another
instance imports with a note per concept, while one exported from
here imports silently. Both are
reported and neither fails the command, because a consumer takes the
document rather than rejecting it. --strict is the opposite posture,
for a sync nobody watches: a bundle that is not read exactly as
written fails, and the counts land in the summary line either way.
--dry-run is the same run with nothing written: each object is sent
as a plan the server answers without storing it, so the notes, the
refusals and the created / updated / unchanged counts are the ones
the import would produce.

Flags:
  -dry-run
    	report what the import would do, and write nothing: every object is sent with the server's dry-run parameter, so the counts, the notes and the refusals are the ones the import itself would meet
  -json
    	print the whole run as one JSON document instead of a line per object: every concept with the plan the write did or would do, every file with the concept that claims it, the notes grouped, the refusals, and the counts the summary line carries. stderr still says every note and skip as it happens
  -strict
    	refuse a bundle that is not read exactly as written: any note or skip fails the command instead of being reported. With --dry-run the same verdict is reached with nothing written, which is what makes it a CI gate
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai import ./knowledge
  ochakai import ga4-bundle.tar.gz --dry-run
  ochakai import ./knowledge --dry-run --strict   # gate a CI sync on the import's own verdict
  ochakai export - | OCHAKAI_URL=https://other ochakai import -
```

## Shared filters

What `ochakai search -h` and `ochakai list -h` share (design doc
[0068](design/0068-how-a-face-is-added-and-removed.md) §2), once.

```
Flags:
  -fm key=value
    	filter by an OKF frontmatter key=value, exactly (repeatable, AND-ed) — the OKF keys with no flag of their own (attester, computation, description, executor, parameters, resource, runtime, title, usage_window); a value spelling a number or a boolean matches the typed one too (--fm required=true). A producer's own key is kept and handed back as written but is not part of the query vocabulary, and type, status, tags, sources and stale_after have filters of their own that answer from a column instead
  -json
    	print the raw JSON response
  -links-to id
    	only concepts whose body links at this id — what points at one concept (its backlinks)
  -prefix path
    	only concepts under this path, e.g. teams/growth — matched on segment boundaries, so it does not reach teams/growth-archive (repeatable, OR-ed)
  -rejected
    	only concepts a human turned down — how you check whether a proposal was already rejected. Without it, rejected concepts stay out of results
  -source resource
    	only concepts citing this resource (exact match against sources[].resource) — what derives from one piece of material
  -status value
    	filter by status: draft|stable|deprecated (repeatable)
  -tag value
    	filter by tag (repeatable)
  -trust value
    	filter by who confirmed the concept: unverified|machine-confirmed|human-reviewed (repeatable, OR-ed) — independent of --status, which is the lifecycle value
  -type value
    	filter by type: Metric|Attested Computation|Skill|Insight|Policy|Glossary Term|BigQuery Dataset|BigQuery Table|Reference, or any custom type (repeatable)
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)
```

## ochakai list

```
Usage: ochakai list [flags] [feed]

List concepts as a set rather than a ranking: the review feeds, and the
two reverse lookups. A listing is a total order, so it pages — a page
with more behind it prints the way on to stderr, and passing that back
with --cursor reads the next one.

The feed is the argument; it sets the order and the first column:

  usage         most-read first over the last 90 days, then by
                lifetime reads, then never-read oldest first at the
                bottom. With --status draft, the draft review feed
  verified_at   oldest verification first, never-verified last — the
                canary feed
  failed        unanswered failure reports (report_outcome failed),
                worst first over the last 90 days — the re-verification
                feed, which `ochakai verify` empties
  stale_after   past the expiry their author declared, most overdue
                first. Verifying does not empty this one: the date is
                the writer's declaration, so clearing it means editing
                the concept to re-declare an expiry

Without a feed, --source or --links-to lists in address order, because a
set is the answer and there is no text to rank it by — so those two
print no first column, the address being the first field. --source is
what cites one resource (the reverse of sources[].resource);
--links-to is what points at one concept (its backlinks).

To rank by relevance instead, use `ochakai search`.

Flags:
  -cursor cursor
    	resume a listing where the last page ended: the cursor the previous page printed, with the same feed and filters
  -limit int
    	max results (server default 100, max 1000)
  # shared with `ochakai search` — see "Shared filters" above

Examples:
  ochakai list usage --status draft --limit 50        # the draft review queue
  ochakai list failed --trust human-reviewed          # the re-verification queue
  ochakai list stale_after                            # past their declared expiry
  ochakai list verified_at --type 'Attested Computation' --trust human-reviewed --limit 100
  ochakai list --source https://wiki.example/finance/revenue-recognition  # what cites this
  ochakai list --links-to metrics/revenue --type Insight   # which insights read this metric
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
    	max concepts (default 1000)
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

Move (rename) a knowledge concept to a new id. Revisions, usage, and
files follow, and inbound references (link targets, and
a `model` key where a document carries one) are rewritten so nothing
breaks.

Where the deployment has an access policy, a move runs when its whole
rewrite fits inside what you may write: the concept, the destination,
and everything that links at the concept. When something you cannot
write links at it, the move is refused rather than performed without
those rewrites — the links it skipped would point at an id that is gone
— and an administrator can move it for you.

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai move insights/revenue-seasonality insights/sales/revenue-seasonality
```

## ochakai purge

```
Usage: ochakai purge [flags] <id>

Hard-delete an already soft-deleted concept: the concept, its revisions,
usage, and file metadata are erased and the id is freed for a
move. History is gone — `ochakai delete` first, then purge. A live
concept is refused.

Flags:
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai delete terms/obsolete-kpi
  ochakai purge terms/obsolete-kpi
```

## ochakai put

```
Usage: ochakai put [flags] <path>

Write one object of the bundle from -f or stdin, creating it or
replacing what is there. The bytes decide which kind it is, as they do
on the wire: an OKF document (--- frontmatter with type, markdown body
— the format `ochakai get` prints) is a concept, and the argument is
its id; anything else is a file, and the argument is the path it lives
at, extension included. Every change is kept as a revision.

As a concept: title is optional (the id's last segment is the display
name when it is absent), JSON input is accepted at a concept's address
too (see api/openapi.yaml), the argument overrides an id in the input
— OKF documents carry none, the path is the id — new concepts default
to draft, and provenance is recorded from your Google identity.
With --only-if-new the write lands only if the id is free, and fails
instead of replacing. With --if-match it lands only if the concept still
has the version you read, and fails instead of overwriting someone
else's edit.

Pick the type by what the concept holds. These are the recommended
vocabulary; any single-line value works as a type, and one of your own
is first-class:

- Metric: what a number means and what it is called — its definition and
  synonyms, one concept per metric (id metrics/<name>), not the SQL
- Attested Computation: a computation others run instead of improvising —
  the code in a # Computation fence, the contract in runtime (required),
  parameters, executor and attester; a confirmed question-and-SQL pair is
  one of these, with a top-level question key for the question it answers
- Skill: the procedure an executor.resource points at — how to actually run
  a computation in a given runtime
- Insight: how to read a metric — baselines, seasonality, the artifacts that
  fake a move, when a number is worth escalating
- Policy: the rule that decides a number, such as revenue recognition or
  cost allocation — what a sources[].resource cites
- Glossary Term: one term of the shared vocabulary, defined once
- BigQuery Dataset: a dataset's catalog entry — the container its tables sit
  in; resource is its canonical URI
- BigQuery Table: a table's catalog entry — where the data comes from,
  column notes, known problems; resource is its canonical URI, and the
  conventional sections are # Schema and # Common query patterns
- Reference: a copy of material that lives outside — enum definitions,
  licenses, schema docs; resource is the original

As a file: any bytes, up to 5 MiB, and the media type is sniffed from
them rather than taken from the name. Nothing derives the file's
concept from where it sits, so the hint printed afterwards is the
relative markdown link to paste into a body — a file no concept links
is a file nobody finds. Non-markdown bytes need the server to have GCS
configured (OCHAKAI_GCS_BUCKET).

Flags:
  -f string
    	input file (default: stdin)
  -if-match version
    	write only if the concept still has this version — its content hash (`ochakai get <id> --json` prints it as .summary.content_hash; a REST GET returns it as the ETag header); a stale version fails with a conflict instead of overwriting. Verifying or rejecting a concept does not move it: only an edit does
  -json
    	print the written object as JSON
  -only-if-new
    	write only if the id is free; a taken id fails instead of being replaced
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai put runbook/restore -f concept.md
  ochakai get insights/revenue-seasonality | sed s/40%/45%/ | ochakai put insights/revenue-seasonality-v2 --only-if-new
  ochakai put insights/reading-revenue/weekly.png -f weekly.png
  for f in *.csv; do ochakai put "tables/orders/$f" -f "$f"; done
```

## ochakai reembed

```
Usage: ochakai reembed [flags]

Embed concepts that have no vector for the configured model — concepts
written before semantic search was enabled, or before the model was
changed. Runs bounded passes until nothing is left, so it can be started
once and left alone.

Flags:
  -json
    	print the raw JSON response of each pass
  -limit int
    	max concepts to embed per pass (server default 200)
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

Record that a concept was reviewed and not accepted, with the reason.
Rejected concepts are hidden from search unless asked for
(`search --rejected`), which is how an agent checks whether a proposal
was already turned down before making it again.
It does not edit the concept: the lifecycle status and the ETag stay put.
A rejection is this instance's ruling, so an exported bundle carries the
concept's real status rather than folding the ruling onto deprecated.
Use --withdraw to take one back — the wire calls that ruling
`withdrawn`, and so does the revision it writes.

Flags:
  -json
    	print the concept as JSON
  -note string
    	why it was not accepted — the next agent reads this before proposing again
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)
  -withdraw
    	withdraw the live rejection instead of recording one

Examples:
  ochakai reject metrics/bad-revenue --note "double-counts refunds; see policies/revenue-recognition"
  ochakai reject metrics/bad-revenue --withdraw
```

## ochakai report

```
Usage: ochakai report [flags] <id> <worked|failed>

Report whether acting on a concept gave a correct result — the last
edge of the write-back loop. After running an attested computation or SQL you
wrote from a concept, report worked or failed (say what went wrong with
--note); failed counts against verified concepts flag them for
re-verification. Prints the concept's updated usage totals.

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

List a concept's change history, newest first: who changed it, how,
and when — the audit surface behind "every change kept as a
revision". Works for soft-deleted concepts too. The whole concept as it
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
Usage: ochakai search [flags] <query>

Search the knowledge base; verified concepts rank higher.
Output: uri, status, title — description (one hit per line).
The order is the answer; there is no score column, because the number
is on a different scale depending on whether the deployment embeds and
the line cannot say which (design doc 0110). --json carries it for a
caller that has calibrated one.
A search is a ranking: it is bounded by --limit and has no page two
(design doc 0068 §2.2), so it takes no cursor and prints none.
`ochakai list` is the other half — the review feeds and the reverse
lookups, which are sets rather than rankings and page with --cursor.
The filters below narrow either command the same way.

Flags:
  -limit int
    	max results (server default 10, max 50)
  # shared with `ochakai list` — see "Shared filters" above

Examples:
  ochakai search "gross margin" --type Metric --type 'Glossary Term' --trust human-reviewed
  ochakai search churn --json | jq -r '.hits[] | .id'
  ochakai search 活性化 --prefix teams/growth --prefix company   # our scope and the shared one
  ochakai search revenue --links-to metrics/revenue --type Insight   # among what reads this metric
```

## ochakai seed

```
Usage: ochakai seed [flags] <file.json | ->

Turn a warehouse's own schema listing into a bundle of draft concepts,
printed as a tar.gz on stdout. One `BigQuery Table` concept per table,
with its columns as a markdown table and its address as `resource`.
Reads JSON rows (an array, or one object per line) with the
INFORMATION_SCHEMA.COLUMNS column names: table_schema, table_name,
column_name, data_type, is_nullable, and description where there is one.
Rows for the same table are gathered however they arrive.

ochakai connects to no warehouse and holds no credential of one: you run
the query, with your own client and your own identity, and pipe the answer
here. Every concept comes out as a draft, because a projected schema is a
skeleton somebody still has to say something about — which is what the
review queue is for.

Raise your client's row limit when you run that query: `bq query` prints
the first 100 rows unless --max_rows says otherwise, and a listing cut
there looks exactly like a small dataset from here.

Flags:
  -prefix string
    	the id prefix each concept is written under, e.g. tables/shop/orders (default "tables")
  -project resource
    	the warehouse project or account the tables live in, written into each concept's resource address

Examples:
  bq query --max_rows=100000 --format=json --nouse_legacy_sql \
    'SELECT table_schema, table_name, column_name, data_type, is_nullable
       FROM `proj.dataset.INFORMATION_SCHEMA.COLUMNS` ORDER BY ordinal_position' |
    ochakai seed - | ochakai import -
  ochakai seed columns.json > catalog.tar.gz   # look before you import
  ochakai seed --project my-proj --prefix warehouse/tables - | ochakai import -
```

## ochakai stats

```
Usage: ochakai stats [flags]

Show the improvement loop as the instance sees it: what the knowledge
base is made of now, how much each queue is holding, what review did
lately, what callers reported, and what they searched for and did not
find. `usage` measures one concept; this measures the base.

A scope line says what the numbers cover whenever that is not the whole
bundle — because a prefix was given, or because an access policy grants
you part of it. Then `misses` reads `withheld` rather than a count: an
unanswered search carries no id, so it cannot be scoped, and the
instance's own list is an administrator's to read.

One line per number, so it composes: cron it and diff the output, or
grep one line out of it for a prompt or a dashboard. The gap lines are
the questions that came back empty, most-asked first — the list of what
to write next.

The three queue lines — drafts waiting to be published or turned down,
concepts whose failure reports are unanswered, concepts past the expiry
their author declared — carry the command that lists that queue, with
the scope you asked under, so the next step is the text on the line.
The verification-age feed is not one of them: it ranks every verified
concept rather than holding the ones that need something, so its size is
the size of the knowledge base and never reaches zero.
With --exit-code the command exits 2 while any of the three is
non-empty and 0 when all are, which is how a scheduled job goes red on
work nobody has picked up. An error still exits 1, so "unreachable"
cannot be read as "nothing to do".

Flags:
  -days int
    	how far back the flow numbers reach, 1-180 (default: 30; raw events are pruned after 180 days)
  -exit-code
    	exit 2 while any review queue is non-empty (0 when all are empty, 1 on error) — for cron and CI
  -json
    	print JSON
  -prefix path
    	measure only concepts under this path, e.g. teams/growth — matched on segment boundaries (repeatable, OR-ed). The unanswered questions are not scoped: one that found nothing found it nowhere
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai stats
  ochakai stats --days 7
  ochakai stats --prefix teams/growth       # our subtree only
  ochakai stats --json | jq .queues.drafts
  ochakai stats --exit-code                 # in CI: red while somebody owes a review
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

## ochakai usage

```
Usage: ochakai usage [flags] <id>

Show how often a concept was actually used: appeared in search results,
fetched individually, reported worked or failed — and when it was last
used. The measure of the write-back loop: evidence for promoting a
draft, and a staleness signal for verified concepts nobody uses.

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

Append a verification against the concept as it stands: you and the time
are added to its ledger. The first confirmation and the tenth re-check
are the same command, and re-checking is what takes a concept out of
both review feeds (`ochakai list verified_at`, `ochakai list failed`).
It does not edit the concept: the lifecycle status and the ETag stay put,
because confirming knowledge and publishing it are different acts. Use
`put` to move a draft to stable.
Verifying a rejected concept lifts the rejection.

Flags:
  -json
    	print the verified concept as JSON
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
the producer $OCHAKAI_PRODUCER declares for writes from this shell, if
any, and whether the server is reachable.

The last two lines are what the deployment is, when there is something
to say: `mode` when it refuses writes, and `posture` when it accepts
them on terms worth knowing first — a sandbox erases what you write on
its next restore, and a dev deployment authenticates nobody.

Flags:
  -json
    	print JSON
  -url ochakai use
    	ochakai server URL (default: $OCHAKAI_URL, else the ochakai use selection)

Examples:
  ochakai whoami
  ochakai whoami --json
```
