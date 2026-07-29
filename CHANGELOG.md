# Changelog

All notable changes to ochakai are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) — while
the major version is 0, a minor bump may break compatibility, and those
breaks are marked **BREAKING** below. Every interface is unstable at 0.x
and only the latest release is supported:
[docs/compatibility.md](docs/compatibility.md) states the policy in full.

Dates are the tag dates (JST). Each released version also has a GitHub
release with longer prose; this file keeps what an operator upgrading
needs to know. Database migrations run automatically at server start
unless a note says otherwise.

Releases before 0.9.0 are retracted in `go.mod` and unsupported — see the
last entry.

## [Unreleased]

### Added

- **BREAKING** — the three faces a human rules from become one,
  `POST /api/v1/review/{id}`, taking `ruling` in the body (design doc
  [0055](docs/design/0055-one-ruling-one-face.md)):

  ```
  POST   /api/v1/verify/{id}          →  POST /api/v1/review/{id}  {"ruling":"verified"}
  POST   /api/v1/reject/{id} {note}   →  POST /api/v1/review/{id}  {"ruling":"rejected","note":…}
  DELETE /api/v1/reject/{id}          →  POST /api/v1/review/{id}  {"ruling":"withdrawn"}
  ```

  The three shared every structural property — each appends to a ledger,
  none touches the document, its lifecycle status or its ETag, each is
  recorded against the caller's identity, and each is on the human
  surfaces and off MCP — and differed only in the value written. That is
  one operation with a parameter. `withdrawn` is not a DELETE because
  nothing is deleted: verifications are append-only, a rejection is the
  one live ruling, and lifting it adds the row the revision log has
  always called `unreject`. A `note` belongs to `rejected` alone and is a
  400 elsewhere. `report_outcome` (`POST /api/v1/usage/{id}`) is
  deliberately *not* folded in — a human's ruling and a machine's
  observation land on opposite surfaces.

  **`ochakai verify` and `ochakai reject [--lift]` are unchanged**, as is
  every MCP tool. No stored data, ledger, export form, revision `change`
  value or trust derivation moves. Clients calling REST directly rewrite
  three calls, each a one-liner.

- **BREAKING** — `GET /api/v1/queues` is gone; its three numbers are the
  `queues` key of `GET /api/v1/stats`, which they already were (design
  doc [0049 §3.1](docs/design/0049-queue-counts.md), revised before
  release). The endpoint returned what the stats response already
  carried, under the same key and from the same query; the only thing it
  could do that stats could not was scope the counts to a subtree.

  So **`prefix` moved to `GET /api/v1/stats`** — and got wider there.
  Every number keyed by an entry now honors it (`entries`, `queues`,
  `review`, `outcomes`), where before a team on a shared deployment could
  count its own review queue but not its own knowledge. `misses` is never
  scoped and cannot be: a search that found nothing found it nowhere, so
  there is no id to scope by (design doc
  [0051 §3.7](docs/design/0051-instance-metrics-and-search-misses.md)).

  **`ochakai queues` is unchanged**, including `--exit-code`; it reads
  the stats face now. `ochakai stats` gains `--prefix`.

- **BREAKING** — the MCP surface calls its two kinds of object what OKF
  and the bundle call them, `concept` and `file` — six tool names, two
  response keys and one argument (design doc
  [0054](docs/design/0054-concept-is-the-okf-word.md)):

  ```
  search_knowledge      →  search_concepts
  get_knowledge         →  get_concept
  put_knowledge         →  put_concept
  delete_knowledge      →  delete_concept
  get_knowledge_usage   →  get_concept_usage
  get_attachment        →  get_file

  get_concept / put_concept result:  {"knowledge": …}   →  {"concept": …}
  get_file result:                   {"attachment": …}  →  {"file": …}
  report_outcome argument:           target             →  id
  ```

  OKF SPEC §2 defines a **concept** as "a single unit of knowledge within
  a bundle", and a **concept ID** as its path without `.md`. ochakai's own
  design records and code comments have said "concept" since 0046; once
  §3.5's fold finished, these tool names were the only place on the wire
  still saying `knowledge` — REST and the CLI carry none. It is the third
  time this project has chosen OKF's spelling over its own (0023, 0038),
  and the last double vocabulary.

  Search alone is plural: `concept` is countable, a search returns many
  and the rest act on one. `get_context` and `report_outcome` keep their
  names — neither names the unit. `attachment` went the same way as
  `knowledge`: 0046 §2.1 dissolved "an attachment" as a kind of object
  and REST folded both kinds onto one address, so `get_attachment` was
  named from a model that no longer exists — its own description already
  said "one file of the bundle". A concept's `attachments` metadata is
  **unchanged**: that is derived attribution ("which files does this
  concept's body point at"), not "a file of the bundle", and the two
  being one word was the problem.

  **`ochakai://` now addresses either kind**, and `resources/read`
  resolves a concept first and a file otherwise — the order
  `GET /api/v1/bundle/{path}` already uses. `get_file` embeds a file
  under `ochakai://<path>`, and that URI used to come back not-found
  from the same server that wrote it (0054 §3.4). The resource template
  is `ochakai://{+address}`, named `object`.

  **The tool count stays at 8** and no capability changes. Breaking for
  a client that names these tools in its configuration — an allow-list,
  a hook, a prompt — or reads the structured result's key. A client that
  reads the advertised tool list and the content blocks needs nothing.
  Shipping both names was refused: tool schemas are paid for out of the
  agent's context, and 8 tools would have become 14 (0054 §4).

- **BREAKING** — `ochakai create` and `ochakai update` are one command,
  `ochakai put` (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.14):

  ```
  ochakai create <id> -f entry.md          →  ochakai put <id> -f entry.md --only-if-new
  ochakai update <id> -f entry.md          →  ochakai put <id> -f entry.md
  ochakai update <id> --if-match <version> →  ochakai put <id> --if-match <version>
  ```

  The wire has been create-or-replace since 0043 §3.5 — a write states
  what the entry should say, and whether the id was free is a
  precondition rather than a different act. The CLI kept two commands
  over one endpoint, so a writer had to answer a question the format does
  not ask, and guessing wrong meant a 409 or an overwrite. `--only-if-new`
  is the `If-None-Match: *` that `create` always sent; without it, `put`
  writes.

  Output still distinguishes the three outcomes: `created`, `updated`,
  `unchanged`. The CLI goes from 28 commands to 27 (docs/surface.md).

  **Two renames 0046 §3.14 called for were not taken**, and the record
  now says so: `browse` and `delete` keep their names rather than
  becoming `ls` and `rm`, and `attach` / `detach` are not absorbed. The
  reasons are in §3.14 — this project's CLI vocabulary is `verify` /
  `reject` / `queues` rather than unix's, and absorbing the file commands
  would split the CLI between path-taking and id-taking commands, which
  is the split §3.5 just removed from REST.

- **BREAKING** — `GET /api/v1/export` is retired. An archive is the
  bundle at a path, so it is a representation of that path (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.5):

  ```
  GET /api/v1/export                     →  GET /api/v1/bundle/          + Accept: application/gzip
  GET /api/v1/export?attachments=false   →  GET /api/v1/bundle/?attachments=false + the same header
  ```

  **This completes §3.5's fold: REST is 14 operations, from 19.** The
  address space is the bundle, and what is not an object of it — a
  ranking, a judgment, a measurement — is addressed outside it.

  The archive gains a scope it did not have: **any path is a subtree**.
  `GET /api/v1/bundle/metrics/` with the header archives `metrics/` and
  everything under it, matched on segment boundaries as every other path
  scope is (design doc 0041). Paths inside are not re-rooted — an archive
  of `metrics/` carries `metrics/revenue.md` and imports back where it
  came from, because this is a copy of a subtree and not a move of one.

  `ochakai export` is unchanged and asks this way underneath. **The web
  UI's "Export OKF" now downloads through the page rather than streaming
  from a link**: a plain `<a href>` cannot send an `Accept` header, so the
  archive lands in the browser's memory before it lands on disk. That is
  the price of the bundle having one address instead of an endpoint named
  after downloading, and it is bounded by what a curated base holds — a
  real backup belongs in CI, where `ochakai export` streams it
  ([operating guide](docs/guides/operating.md)).

- **BREAKING** — `GET /api/v1/backlinks/{id}` is retired. What points at
  an entry is a filter on the search face now (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.5):

  ```
  GET /api/v1/backlinks/metrics/revenue  →  GET /api/v1/search?links_to=metrics/revenue
  ```

  It is the reverse lookup `source=` already was — "what cites this
  material" and "what points at this entry" are the same shape of
  question, and both answer with a set of entries rather than a ranking.
  Having one as an endpoint and the other as a filter meant the composable
  one could be narrowed by type, status, trust or path while the other
  could not.

  What it gains: every filter, any `sort`, and a `cursor` — the endpoint
  capped at 100 rows with no way to read the 101st. Two things change with
  it. Rows come back in **address order** rather than most-recently-updated
  first, which is what a listing that can be resumed costs (design doc
  0050). And **rejected entries are excluded** unless `rejected=true`, as
  on every other listing; the endpoint returned them.

  `ochakai backlinks <id>` is unchanged as a command and asks this way
  underneath; `ochakai search --links-to <id>` is the same question with
  the rest of the filters available. The web UI's "linked from" is
  unchanged. **MCP does not get `links_to`** — a tool schema is paid for
  out of the agent's context, and an agent that wants the reverse edge
  already gets it inside `get_context`, which follows it when packing
  companions (design docs 0015 §3.1, 0046 §3.14).

  The REST surface goes from 16 operations to 15 (docs/surface.md).

- **BREAKING** — `GET|PUT|DELETE /api/v1/knowledge/{id}` is retired, and
  `GET /api/v1/knowledge` is now `GET /api/v1/search` (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.5). A concept is
  read, written and deleted at the path it lives at, its id plus `.md`:

  ```
  GET    /api/v1/knowledge/metrics/revenue      →  GET    /api/v1/bundle/metrics/revenue.md
  PUT    /api/v1/knowledge/metrics/revenue      →  PUT    /api/v1/bundle/metrics/revenue.md
  DELETE /api/v1/knowledge/metrics/revenue      →  DELETE /api/v1/bundle/metrics/revenue.md
  DELETE /api/v1/knowledge/metrics/revenue?purge=true
                                                →  DELETE /api/v1/bundle/metrics/revenue.md?purge=true
  GET    /api/v1/knowledge?q=revenue            →  GET    /api/v1/search?q=revenue
  ```

  Nothing was added and nothing can no longer be asked: the retired
  address was a *second spelling* of what the bundle address already
  answered, one suffix away, and a client had to know which of two names
  ochakai preferred for the same object. The View, the document under
  `Accept: text/markdown`, the ETag, `If-Match`, `If-None-Match: *`,
  `Ochakai-Unchanged` and the 415 for a JSON body are all unchanged —
  they are the same code, reached at the address the object has.

  The list face is renamed rather than moved, because it does not return
  an object of the bundle at all: it returns a *ranking*, which is
  ochakai's own. Under the old name it read as the collection that
  `/api/v1/knowledge/{id}` indexed into, and it was neither that
  collection nor addressable as one.

  The CLI, the MCP tools and the web UI are unchanged — they speak this
  for you. A direct REST caller edits the URL. The REST surface goes from
  19 operations to 16 (docs/surface.md); `/api/v1/backlinks/{id}` and
  `/api/v1/export`, which §3.5 also folds, keep working until their
  successors exist.

- **Which agent, at which build, wrote this** (design doc
  [0052](docs/design/0052-producer-beside-the-actor.md)). An actor now
  carries a fourth field, `producer` — OKF SPEC §7's
  `<producer>/<version>` form, e.g. `insightflow/1.4.0`. It arrives on
  `X-Ochakai-Producer`, or, on MCP, from the `clientInfo` the protocol
  already carries at `initialize`, or from `OCHAKAI_PRODUCER` for the
  CLI. Any authenticated caller may send it: unlike a delegated identity
  it names nobody but the caller, so no allowlist gates it. A malformed
  value is a 400, not a silent drop.

  It is recorded **beside** the authenticated actor and never in its
  place. This is the one value ochakai records that the caller declares
  about itself, and a self-declared name put where `created_by` goes
  would make "who wrote this" answerable by anyone — the composition is
  the same one delegation already uses (0027). So provenance now reads
  `human:tanaka@… via process:app-sa@… using insightflow/1.4.0` on every
  surface: the CLI's provenance lines, the web UI, `log.md`, and a
  `producer` key beside `by` and `via` in an exported document's
  `generated` / `verified`. Import reads none of it back, as with the
  rest of the trust family.

  Without it, an agent writing under a person's own credentials — an MCP
  client, a CLI in a script — left a record saying a person wrote the
  prose, and a model upgrade could not be judged against the drafts that
  followed it. Nothing is backfilled: no past write declared a producer,
  and stamping one now would invent an observation the server never made.

  `ochakai whoami` prints the producer this shell declares, so a value
  that is wrong is visible before it is recorded a thousand times.

  The actor kinds are unchanged (`human` / `process`), and so is the
  trust tier — SPEC §5.3 reads the `human:` prefix alone. The web UI
  sends no producer: a person editing by hand runs none.

- **BREAKING** — `GET|PUT|DELETE /api/v1/attachments/{id}/{name}` is
  retired. A file is read, written and deleted at the path it lives at:
  `/api/v1/bundle/{path}` (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§3.3, 3.5). MCP's
  `get_attachment` takes a `path` for the same reason.

  The old address was named after a concept that no longer exists. A
  file used to be something an entry *had*, and the address said so by
  putting the entry's id in front of a filename. Whether an entry shows
  a file is now a question its body answers, so the address that
  asserted it had nothing left to say — and an imported file that lives
  elsewhere in the bundle had no honest spelling there at all.

  Every file an entry shows carries its `path`, so a client that reads
  an entry has the address of each of its files without composing one.

- **Something to tell you the review queue is not empty** (design doc
  [0049](docs/design/0049-queue-counts.md)). `GET /api/v1/queues` and
  `ochakai queues` count the three feeds a curator empties — drafts
  waiting to be published or turned down, entries whose failure reports
  are unanswered, entries past the expiry their author declared —
  instead of listing them. `--prefix` scopes the counts to a subtree, and nothing else
  filters them: which entries is the feed's question.

  The queues were emptiable already; nothing said they were not empty,
  and a review queue going quiet looked exactly like one with nothing in
  it. Each printed line carries the command that lists that queue, and
  `ochakai queues --exit-code` exits **2** while any of them is
  non-empty, so a scheduled CI job goes red on work nobody picked up —
  1 stays "the command failed", so an unreachable server cannot be read
  as "nothing to do". The web UI shows the same counts on its Review
  tab. [docs/guides/operating.md](docs/guides/operating.md) has the cron
  recipe.

  ochakai delivers nothing itself: no mail, no chat, no webhooks, no
  address book, and no scheduler inside the server. The verification-age
  feed is deliberately uncounted — it ranks every verified entry, so its
  size is the size of the knowledge base and nobody can drive it to
  zero. MCP does not get the endpoint: an agent's ends of the loop are
  drafting and reporting outcomes, both of which lengthen a queue rather
  than read it.

- **BREAKING** — a file reports the path it lives at. `okf_path` is gone
  from the wire and from the attach call; `Attachment` carries `path`
  instead (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.3).

  `okf_path` meant "this file is not where ochakai would have put it" —
  a second identity for one object, and one that could disagree with the
  row it described. Since files became objects at paths, where a file
  lives is simply its address: a file written through
  `/api/v1/attachments/{id}/{name}` is at `<id>/<name>`, and one written
  at its own bundle path is there. `name` stays beside `path` as the last
  segment, the way a concept keeps its id beside its path.

  The `?okf_path=` parameter on attach is gone with it. A file that
  belongs elsewhere in the bundle is written at the path it belongs at
  (`PUT /api/v1/bundle/{path}`), which is what `ochakai import` now does
  — where a file lives is an address, not an annotation on a write to
  something else.

- **BREAKING** — the MCP write face is one tool. `create_knowledge` and
  `update_knowledge` are `put_knowledge`: it creates when the id is free
  and replaces when it is taken (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.14).

  The two tools asked the agent a question the document does not answer.
  A write states what the entry should say, and whether the id was
  already taken is not part of that statement — the same reasoning that
  gave REST one `PUT` instead of a POST beside it. The guards are
  unchanged and still apply to the half they belong to: reviving a
  curated tombstone is refused on the create side, replacing an entry a
  human ruled on is refused on the other, and both refusals still name
  the way forward.

  It is also where the tool budget actually is. The whole surface is
  33.5 KB of JSON schema; this merge saves about 6 KB of it, which is
  nearly twice what dropping `delete_knowledge`, `get_knowledge_usage`
  and `get_attachment` would have bought — so those three stay
  (issue #272), and the surface is eight tools.
- **A listing walks past the limit** (design doc
  [0050](docs/design/0050-listings-page-rankings-do-not.md)). Every
  listing mode of `GET /api/v1/knowledge` — the four `sort` feeds and the
  `source` lookup — answers with a `cursor` when more entries follow, and
  takes it back as `?cursor=` to continue. The feeds are ledgers a
  reviewer works through, and one that stopped at 1000 with no way
  forward was indistinguishable from one that was finished.

  The cursor is opaque and keyset, like `reembed`'s: it carries a
  position in that listing's order, so paging costs the same on page 40
  as on page 1, and it names the listing it came from — one from another
  feed is a 400 rather than a page starting somewhere surprising. Pass it
  back with the same `sort` and the same filters; changing them mid-walk
  asks a new question from an old position.

  **A search still does not page.** `q` with `cursor` is a 400: a
  relevance ranking is a fused window rather than an order to resume
  from, so there is no honest page two, and 50 hits remain the whole
  contract for a search. The way past them is a narrower question —
  which is what the filters are for.

  No total count comes with a page either. The absence of a cursor is the
  end of the listing; an exact count over a filtered feed costs a second
  scan of it, and a cursor is a position rather than a snapshot — the
  feeds are live, so an entry that moves while you walk may be missed or
  seen twice. Counting the three review queues is `GET /api/v1/queues`
  above, on an address of its own: count once, or walk — they are
  different questions.

  On the surfaces: MCP's `search_knowledge` takes and returns `cursor`;
  `ochakai search --cursor` resumes a listing and prints the way on to
  stderr, leaving stdout one hit per line (the command does not walk the
  pages for you — `--limit` is your bound); and the web UI's review queue
  and feeds now have a "load more" that appends a page, replacing the
  "load up to 1000" note that was the cap made visible.

- **A document's own `generated` and `verified` are kept as a claim**
  instead of being destroyed (design doc
  [0046](docs/design/0046-bundle-address-space.md) §2.2). A bundle from
  another instance — or from any OKF producer — says who generated the
  content and who confirmed it. Those keys reached no ledger, which is
  right, and were also cut out of the stored bytes, which was not: the
  fact that a named person confirmed something somewhere else was gone,
  and no later release could get it back.

  They now travel under a `received` key in the stored document:
  `received.verified` is always somebody else's assertion, `verified` is
  always this instance's ledger, and the export form carries both
  without them colliding. The write reports what it did, so nothing is
  reinterpreted in silence (SPEC §11) — an `Ochakai-Note` on REST, a
  `notes` entry on MCP, a `note:` line and a bump in the summary count
  on `ochakai import`, which also means `--strict` now fails a
  cross-instance sync rather than passing it as clean.

  **Nothing derives trust from a claim.** The trust tier, `?trust=` and
  `observed` answer from this instance's ledger exactly as before, and
  an imported entry is still unverified until somebody here verifies it
  — design doc [0009](docs/design/0009-provenance-portability.md)'s
  reason for refusing to *believe* a payload stands untouched; only the
  part that discarded it is gone. A claim is an ordinary key of the
  stored document — it reads back out of `content`, and no listing,
  ranking or filter surface answers from it.

  Round trips are unchanged. The export form of an entry, sent back —
  `get` → edit → `PUT`, or `export` → review → `import`, the Git review
  loop — carries a trust family that says exactly what this instance
  observed, which is recognized as its own and taken back off. The same
  bytes are stored, no revision is written, and no note is printed.

- **A search that finds nothing is recorded, and the loop is measured at
  the instance** (design doc
  [0051](docs/design/0051-instance-metrics-and-search-misses.md)).

  Everything ochakai measured was per entry — how often this one was
  searched, fetched, reported worked — and a search that returned no
  hits was discarded entirely, because every measurement was keyed by a
  knowledge id and a miss has none. What somebody asked for and did not
  find is the one list that says what to write next, and it was the only
  one being thrown away.

  It is now a row of its own: the query as it was typed (up to 500
  bytes), the caller, and when. Recording stays off the read path and is
  best-effort, exactly like usage events — buffered, flushed every five
  seconds, pruned after 180 days.

  `GET /api/v1/stats?days=30` answers the question nothing answered
  before: entries by lifecycle and by trust tier, how many were created
  and verified in the window, what callers reported, and the most-asked
  questions that came back empty. It carries the three queue depths of
  `/api/v1/queues` beside them, from that endpoint's own query — the
  place 0049 §3.1 kept for exactly this. It is computed on demand — no
  rollup table, no scheduler — and a window longer than the 180-day
  retention is a 400 rather than a quietly short answer.

  `ochakai stats` prints it one number per line, so cron and `diff` can
  do the rest — `ochakai queues` stays the nudge, with the next command
  on each line and the exit code; this is the whole picture. The web
  UI's review page carries four tiles and the list of unanswered
  questions beside the queue strip. MCP does not get a tool: an agent
  that searched and found nothing already knows, and its search is what
  the miss was recorded from.

  Storing what a caller typed is new here, so it has an off switch —
  `OCHAKAI_RECORD_MISSES=false` — and a public deployment
  (`OCHAKAI_PUBLIC_READ_ONLY`) keeps none at all, for the same reason it
  reads no identity. Migration `0031_search_miss.sql` adds the one
  table and touches nothing else.

- **What enters the bundle leaves it** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2). `ochakai
  import` now keeps every file the bundle carried, at the path it
  arrived at — a file no entry references, a markdown document with no
  `type`, an archive, an SVG. They are written through the bundle
  address, so nothing is attributed to an entry that did not ask for it:
  whether an entry shows a file is a question its body answers (§3.3).

  A bundle used to come back smaller than it went in. Anything that was
  not a concept and not an attachment was reported and dropped, which is
  the one thing a store of bundles must not do — and the report made it
  look deliberate. The skip list is now down to what cannot be stored at
  all: an empty file, one past 5 MiB, or a path ochakai cannot address.

  The summary line and `--dry-run` count them (`… 0 attachments, 3
  files, 0 skipped`), and `--strict` is unaffected: a file that is kept
  is not a reinterpretation of anything.

- **The bundle address reads and writes every object in it** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.5).
  `GET|PUT|DELETE /api/v1/bundle/{path}` now serves a concept at
  `<id>.md`, a file at its own path, and the two generated files as
  before.

  A concept answers and is written exactly as it is on
  `/api/v1/knowledge/{id}` — the same View, the same document under
  `Accept: text/markdown`, the same `ETag`, the same `If-Match` and
  `If-None-Match: *`, the same `Ochakai-Unchanged`. One object, two
  addresses, one answer: an address is not a second surface.

  What the bytes are decides what they become. A markdown document whose
  frontmatter carries a `type` is a concept; anything else is a file —
  **including a markdown document with no `type`**, which SPEC §11 makes
  not-a-concept and which a bundle that carried one now gets back
  instead of losing (§3.2). A file needs no owner: whether an entry
  shows it is a question its body answers (§3.3), so a file written
  under an entry's namespace arrives attributed and one nothing points
  at is simply a file.

  `index.md` and `log.md` stay 409 on write, and a file's history lands
  in the same ledger the concepts use — created and deleted, by whom.

- **`ochakai log [path]`** prints the update history as OKF's `log.md`
  (SPEC §9): date-grouped, newest first, for a path or the whole bundle
  (design doc [0046](docs/design/0046-bundle-address-space.md) §3.8).
  It says what `ochakai revisions` says, in the format a bundle carries
  — which is what makes a history portable.

- **An OKF frontmatter key is a filter**: `?fm.resource=bq://p.d.t` on
  REST, `fm: {resource: "bq://p.d.t"}` on MCP, `--fm resource=bq://p.d.t`
  on the CLI (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.11). It matches a
  scalar exactly or a member of a list, and repeating it with different
  keys ANDs them.

  The whole frontmatter is now indexed as `jsonb`, so a key OKF adds in a
  later version is askable as soon as ochakai knows the spelling — no
  column, no migration, and no data to move. Storage became additive when
  the document became the stored form; querying did not follow until now.

  A value is text, and one that spells a number or a boolean matches the
  typed value the document wrote as well: YAML types frontmatter, so
  `required: true` is indexed as a boolean and `usage_count: 5` as a
  number, and asking for them by their text alone found nothing —
  silently, since a filter matching nothing is zero rows rather than an
  error. `fm.required=true` now finds both the document that wrote
  `true` and the one that wrote `"true"`, while text that only looks
  numeric stays text (`fm.code=007` finds `code: "007"`).

  Exact match and list membership are the only two comparisons. Ranges,
  negation and boolean expressions are the doorway to a query language,
  and what ochakai returns is knowledge rather than rows.

  **The filter vocabulary is OKF's** (design doc
  [0047](docs/design/0047-fm-carries-okf-keys.md)). `fm.` asks the OKF
  keys nothing else asks about — `attester`, `computation`,
  `description`, `executor`, `id`, `parameters`, `resource`, `runtime`,
  `status_note`, `title`, `usage_window` — and every surface lists them,
  so the OpenAPI text, the MCP tool schemas and `--help` say exactly what
  the server accepts.

  A key a producer invented is **stored and handed back exactly as
  written** and always will be (OKF SPEC §4.1 requires it, §11 forbids
  rejecting a document for carrying one), but naming it in a filter is a
  400 listing what can be asked. An open vocabulary cannot tell you
  whether zero results means "no entry says that" or "the producer
  spelled it `team`, not `owner`" — and there is no way to write down
  what is askable, so an agent that sees only a tool schema guesses.

  The typed columns stay where they are — they are the same values with
  better indexes (trigram over the text, a date for `stale_after`, an
  array for tags) — so `type`, `status`, `tag`, `source` and
  `stale_after` keep answering from them, and `fm.type`, `fm.status`,
  `fm.tags`, `fm.sources` and `fm.stale_after` are refused with a 400
  naming the filter to use instead. A column and a jsonb path do not
  answer the same question — `status=stable` matches a document that says
  nothing, since OKF's default is stable, while `fm.status=stable` never
  would — and which of the two you got would have depended on how you
  spelled the key.

  Migration 0027 adds the column and a GIN index and backfills from each
  stored document; no timestamp moves and no hash changes, since indexing
  what an entry already said is not a change to what it says.

- `ochakai import --strict` — fail on a bundle that was not read exactly
  as written, instead of reporting it. A note is still the default and
  still not an error: OKF tells a consumer to take a document rather than
  reject it, and an operator watching the output sees every
  reinterpretation as it scrolls past. Nobody watches a sync running in
  CI, and a knowledge base whose entries quietly landed as drafts is a
  knowledge base whose agents stopped trusting the right things.

  Parse-time notes and skips are known before the first write, so
  `--strict` refuses there: the import lands whole or writes nothing.
  What only the server can judge — a document it read differently, or one
  it refused — is checked again after the run, so the summary still says
  how far it got and the exit code still says it was not clean.
  `--dry-run --strict` is the gate to put in front of a CI sync.

  The summary line now carries the note count on both paths
  (`… 0 skipped, 0 notes`), because a count that only appears in the
  scrollback is a count nobody reads.

### Security

- **A bundle carries what it carries** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2). The write path
  refuses no media type: the allowlist of png, jpeg, webp, pdf and plain
  text is gone, and so is the cap of 20 files per entry. A file is still
  sniffed rather than trusted, and still bounded at 5 MiB.

  The allowlist was reasoned from a knowledge store that should hold only
  what an agent can read (design doc
  [0013](docs/design/0013-attachment-files-gcs-only.md)). What it missed
  is that what ochakai holds is a *bundle*: refusing a producer's zip of
  seed data, a `.parquet`, or an SVG diagram does not keep them out of
  the knowledge base — it keeps them out of the copy ochakai hands back.
  The cap was the same mistake in miniature; a limit on how many objects
  a directory may hold is a limit on what a bundle may be.

  What the allowlist protected against is protected against on delivery
  instead, where it belongs, and has been since the entry below: an SVG
  or an HTML file is served as a download under
  `Content-Security-Policy: sandbox`, with no origin to run script in.
  "Receive it, do not render it" is one rule with two halves, and both
  halves are now true.

  A file the embedding model does not take — an archive, a font — is
  stored, served and exported like any other and found by its name. It is
  no longer offered to the embedder, so a bundle full of them logs
  nothing.

- **A file is served the way its media type deserves** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2). An image, a PDF
  or plain text is served `inline` as before; anything else now goes out
  as a download, under `Content-Security-Policy: sandbox`, and
  `X-Content-Type-Options: nosniff` is set on both.

  Nothing stored today takes the second branch — the write path still
  refuses every media type but those (design doc
  [0013](docs/design/0013-attachment-files-gcs-only.md)), and it refuses
  `text/html` and `image/svg+xml` by name. That is exactly why the rule
  belongs on the way out as well: an SVG is an image by every convention
  and a document that carries script by specification, and the only thing
  standing between one and the web UI's own origin was a sniffer check at
  write time. A row written before that check, or bytes a browser reads
  differently than `http.DetectContentType` did, had nothing behind it.

  It is also the half that has to exist first. 0046 §3.2 stops refusing
  media types on the way in — a bundle whose files come back missing does
  not round-trip — and "receive it, do not render it" is only a policy
  once the second half is true.

### Fixed

- **One spelling decides what is reserved.** "Is this index.md or
  log.md" was answered in four places — `domain.ReservedBundleName`,
  which folded case, and three inline comparisons that did not (the
  bundle address's write refusal, the import skip, and a copy in
  `internal/okf` with no callers). The halves of one address disagreed:
  at `Index.md` a concept was accepted and a file was refused with
  "index.md and log.md are generated rather than stored". OKF SPEC §3.1
  reserves exactly the two spellings it writes, so that is what
  `ReservedBundleName` answers now, and it is the only one that answers.
  `Index.md` is a file OKF says nothing about, and a bundle that carried
  one gets it back (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2).

- **BREAKING** — **an object's history is at the object's address**, and
  a `log.md`'s two representations say the same thing (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§3.5, 3.8):

  ```
  GET /api/v1/bundle/{id}/log.md   (Accept: application/json)
      →  GET /api/v1/bundle/{id}.md?history
  ```

  The JSON at a `log.md` was the ledger of the concept named by the
  *directory*, while the markdown at the same address was the history of
  the whole subtree. So `Accept: application/json` on the root, or on any
  directory that is not also a concept, was a **404** — the structured
  history of a directory could not be read at all, and the spec's own
  "two representations of one thing, at one address" was not true of this
  one. A concept's history also sat at `{id}/log.md`, inside a directory
  named after it that need not exist.

  `?history` is that history, at the object's own address, for a file as
  much as for a concept — a revision is an event about an object (§3.1)
  and the ledger is keyed by path, so "what happened to this object" is
  one question. A concept's carries each revision's whole document, so a
  diff between two revisions is a text diff; a file's carries the changes
  alone, having none. A `log.md` is now the subtree's changes in both of
  its representations, at every path including the root, under a
  `changes[]` key that names each object by its path.

  Breaking for a client reading `{id}/log.md` as JSON — `ochakai
  revisions`, the web UI's history panel and `apiclient.Revisions` all
  moved with it, so only a hand-written client is affected.

  **This raises `docs/surface.md`'s PARAM ceiling from 18 to 19.** Said
  out loud, which is what the ceiling is for.

- **The archive carries the history.** Design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.8 ends "the history
  becomes portable", and the archive carried the `index.md` of every
  directory and no `log.md`: the ledger was readable only by calling the
  instance that holds it, and a purge
  ([0031](docs/design/0031-purge.md)) is the only thing that ever
  removes a row from it. A `log.md` is written beside every `index.md`
  now — the root's included — from the same renderer and the same
  1000-line bound as the address, so a reader extracting the bundle and
  a reader calling `GET /api/v1/bundle/{dir}/log.md` are not reading two
  different histories, and from the export's own snapshot, so it
  describes the same instant as the documents beside it.

  An import still never reads one back (§2.3): what happened here is
  this instance's observation, published the way `generated` and
  `verified` are ([0009](docs/design/0009-provenance-portability.md)).

- **A directory's `index.md` lists the files in it** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.7). §3.7 names
  three sections — subdirectories, concepts, files — and only the first
  two were rendered, so a file at a path no concept shows was stored,
  served at its own address, carried in the archive, and invisible in
  the bundle's own navigation. It is listed now, under a `## Files`
  heading, in all three places that render a listing: the generated
  `index.md`, its JSON representation (`files[]`, so a web UI's
  directory page can show them), and the copy the archive carries — the
  same renderer, so a bundle's navigation does not depend on which asked.
  `ochakai browse` prints them after the entries.

  A subdirectory line still counts **concepts**, and a directory holding
  nothing but files is still not a subdirectory: OKF's §8 listing is
  navigation between concept documents, and a count mixing the kinds
  would misdescribe every directory that has both. Asked about directly,
  such a directory lists what it holds.

  A bundle with no loose files exports byte-for-byte as it did — the
  heading appears only where there is something under it.

- `api/openapi.yaml` stopped promising fields the server never sent, and
  started declaring things it did. The contract test validates requests
  and responses against the spec, but JSON Schema only checks what is
  *present*, so a schema could advertise a field no release ever wrote
  and every response still passed. Four had drifted:

  - `ContextRank` declared `verified: boolean`; the server sends
    `trust` (the three-tier value of SPEC §5.3). The spec's own example
    showed `trust`, so the schema disagreed with the sample above it, and
    a generated client waited for a field that never came.
  - `View` declared `created_at`, `updated_at`, `trust` and
    `content_changed_at`. None is sent — the first three live in
    `summary` and the fourth is `observed.generated.at`.
  - `components/headers/Note` was declared and referenced by no
    operation, so the `Ochakai-Note` header the server sets on a write
    (an unrecognized status, an unparseable date) appeared nowhere in the
    contract. It is now declared on `PUT /api/v1/bundle/{path}`.
  - `components/parameters/IfMatch` was likewise unreferenced, and
    `PUT /api/v1/bundle/{path}` — the write that honors it — declared no
    parameters at all. `If-Match`, `If-None-Match`,
    `X-Ochakai-On-Behalf-Of` and `X-Ochakai-Producer` are now on it, and
    the two provenance headers on `DELETE` beside it.

  `log.md`'s JSON representation is also declared now (`BundleLog`,
  carrying `Revision`, which had been a component nothing referenced):
  the prose said `Accept: application/json` returns "the listing or the
  revisions" and only the listing had a schema, so a generated client
  could not read the history at all.

  Two new checks keep it that way: `TestOpenAPISchemasMatchGoTypes`
  compares each response schema to the Go type that marshals into it, in
  both directions, and `TestOpenAPIComponentsAreReachable` fails on a
  component no operation points at.

- **A path prefix is a string, not a pattern.** An id may contain `_`,
  and `LIKE` reads it as "any single character". The statements that
  decide which files sit inside a concept's namespace were written with
  `LIKE`, so `sales_2024` matched everything under `salesX2024/` as
  well as its own — and three of those statements write:

  - moving `sales_2024` carried `salesX2024`'s files away with it;
  - purging `sales_2024` deleted `salesX2024`'s files;
  - `salesX2024`'s filenames were searchable as `sales_2024`'s, and its
    files came back on `sales_2024`'s reads.

  Browsing has avoided `LIKE` for this reason since design doc
  [0014](docs/design/0014-folder-browse.md) and the search filters since
  [0041](docs/design/0041-path-scoped-search.md); the object-path
  statements introduced with
  [0046](docs/design/0046-bundle-address-space.md) §3.3 did not inherit
  it. They match by string now, in Go and in the one SQL function that
  had it too (migration 0033, which recomputes the affected lexical
  haystacks). Only ids containing `_` (or `%`) were affected, and only
  when another id differed from them exactly at that character.

- **The update history survives a file, and carries one.** A revision is
  an event about an object (0046 §3.1), so writing a file records one
  with no concept id — and the log read them by id. The root
  `GET /api/v1/bundle/log.md` (`ochakai log` with no path) answered 500
  on any instance that had ever attached a file, and every other
  `log.md` left files out of the history they belong in. The ledger is
  read by path now, so attaching, replacing and removing a file appears
  in the log of the directory it happened in, and a file's history
  follows the file when a `move` carries its namespace along.

- **The archive carries a file nothing owns.** `Accept: application/gzip`
  on a bundle path built its file list from what was *attributed* to a
  live entry, so a file at a path no body points at — a producer's seed
  data in a shared directory, or a file whose concept has since been
  deleted — was stored, served at its own address, and left out of the
  archive. Silently, in the one endpoint that exists to be the copy you
  keep. What enters the bundle leaves it (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2): the archive is
  every file under the path now, at the path it lives at, plus the files
  an in-scope entry's body points at from outside the subtree.

- **`DELETE …?purge=true` at a file's path removes the file** instead of
  answering 404. `purge` names something only a concept has — a
  tombstone still holding an id — and the handler read the parameter as
  "this is a concept", so a file at that path was neither purged nor
  deleted and the caller was told there was nothing there. It was, and
  it went on being served. A file's one delete is already the
  irreversible one, so the parameter changes nothing at its path.

- **A concept written where a file already lives is a 409.** The two
  kinds of object share one address space (0046 §3.5), and the insert
  named the concept id in its conflict clause — which a file has none
  of. The primary key on the path caught it and the caller read a 500
  with a constraint name in it. It now reads the same "already exists"
  a live entry gives, saying that a file holds the path.

- The invalid-status error names the statuses there are. It offered
  `draft, verified, deprecated, rejected` — two of which stopped being
  statuses when design doc
  [0043](docs/design/0043-document-first.md) §§3.2-3.3 moved confirmation
  into the verification ledger and rejection into a ruling — and omitted
  `stable`, the value the writer usually wants. A caller who wrote
  `status: published` was told to retry with `verified`, which fails the
  same way. It now renders `domain.Statuses` like every other
  enumeration, and a test reads the whole tree for a lifecycle list
  spelled by hand, so this copy cannot come back.

- The shipped Claude Code write-back hook told the agent to search with
  `--status rejected`, which is not an invocation `ochakai search`
  accepts — `--status` takes a lifecycle value and a rejection is not
  one. It now says `--rejected`, the flag that actually shows entries a
  human turned down.

- Producer-defined keys **inside** a `sources` entry, a `parameter`, an
  `executor` or an `attester` are kept rather than dropped (SPEC §4.1,
  design doc [0043](docs/design/0043-document-first.md) §3.6). They ride
  inline in a document, exactly where their writer put them, and are
  nested under `extra` in ochakai's JSON — where nesting keeps a producer
  key from ever colliding with one a later spec adds.

  0036 §3.5 dropped them with a note, reasoning that the point of
  promoting these families to typed fields was that four surfaces could
  describe one shape. The premise went when the document became the
  stored form: storage keeps whatever the writer wrote, a schema is only
  needed by the projections that read it, and a key discarded on the way
  in is a key no later release can recover. Nothing is reported any more,
  because nothing is reinterpreted — a note is for a value read
  differently than written, and a key carried through untouched is not
  one.

  A producer key is content: changing one moves the entry's version, and
  a write that only repeats it is still a no-op.

### Changed

- [docs/surface.md](docs/surface.md) counts two more dimensions and sets
  a ceiling on each. Counting operations alone measured the one dimension
  the pressure escapes through: folding an endpoint into a query
  parameter or a content type always lowers the operation count and can
  never raise it, so every fold read as a pure win. 0046 §3.5's 19 → 14
  moved three endpoints into `links_to=`, `Accept: application/gzip` and
  a path suffix, and the document recorded only the five that went away.

  `## PARAM (18)` and `## HEADER (9)` are read back out of
  `api/openapi.yaml` by `cmd/ochakai/surface_test.go`, so a fold now
  shows as a trade rather than a subtraction. Distinct names, not
  occurrences — `limit` means the same thing on five operations and is
  learned once, so reusing the vocabulary is free and inventing a word is
  what costs. Path parameters are not counted: `{path}` and `{id}` are
  the address of the thing being operated on, not an option beside it.

  The new caps section declares a ceiling per surface and
  `TestSurfaceStaysUnderItsCaps` fails when one is exceeded. The cap is
  one number anybody can raise, which is the point rather than a
  weakness: a PR that moves it is a PR saying out loud that it is making
  ochakai bigger.

- **BREAKING: semantic search is on by default on Google Cloud** (design
  doc [0053](docs/design/0053-embeddings-by-default.md)). ochakai asks the
  metadata server which project it is running in and turns hybrid search
  on with it — `OCHAKAI_VERTEX_PROJECT` is no longer how you switch it on,
  only how you name a different project or run outside Google Cloud.

  **What actually decides it is IAM, not configuration.** Without
  `roles/aiplatform.user` on the service identity, Vertex AI does not
  answer: ochakai finds that out with one small embedding at startup, logs
  one line, and serves lexical-only search. So a deployment that never
  granted the role is unaffected by this release, and so is every
  deployment outside Google Cloud. **A deployment whose service account
  does hold the role — the broadly-scoped default compute identity, for
  instance — starts embedding on the next deploy, which means one Vertex
  AI call per write.** Set `OCHAKAI_EMBEDDINGS=off` to decline; it takes
  `on` or `off`, and any other spelling is a startup error rather than a
  guess at what you meant.

  The reason is in the ROADMAP: a trigram index cannot serve a
  two-character pattern, so a Japanese knowledge base — the case ochakai
  is built for — is answered by scanning, and "enable embeddings" was the
  standing answer sitting in §4 of a 948-line deploy guide, under the word
  *Optional*.

  A deployment that named its project keeps the old strictness: if Vertex
  AI or pgvector is not there, it refuses to start, because it asked. A
  discovered one falls back to the lexical search it would have had.

- **A changed embedding dimension rebuilds the vector tables instead of
  refusing to start** (design doc
  [0053](docs/design/0053-embeddings-by-default.md) §3). Changing
  `OCHAKAI_EMBEDDING_DIM` on a base that already holds vectors used to
  stop the server and hand the operator a `DROP TABLE knowledge_embedding,
  attachment_embedding` to run in `psql`. ochakai performs it now, and
  logs that it did.

  Nothing you curated is involved: a vector is derived from the object it
  describes — which is why these tables live outside the versioned
  migrations and why `reembed` exists — and the old ones were in a space
  nothing would query. Refilling them stays `ochakai reembed`, because
  that spends money; until it runs, search answers lexically.

- **The published contract says which address to build on.** While
  [0046](docs/design/0046-bundle-address-space.md) §3.5's fold is in
  flight, three addresses answer for the same object, and
  `api/openapi.yaml` did not say which one survives it.
  `/api/v1/bundle/{path}` does. The operations it replaces —
  `GET|PUT|DELETE /api/v1/knowledge/{id}` and
  `GET|PUT|DELETE /api/v1/attachments/{path}` — are now marked
  `deprecated`, each naming the bundle call that answers the same
  question. Nothing is removed and no behaviour changes: a removal
  arrives in a minor release with a changelog entry, as
  [the compatibility policy](docs/compatibility.md) says, not after a
  deprecation window.

  `/api/v1/knowledge` (the list), `/api/v1/backlinks/{id}` and
  `/api/v1/export` are retired by the same section but carry no mark:
  their successors are not built yet, so they are still the only way to
  ask what they answer. The document says so rather than leaving a
  reader to find out.

  Two things that read as holes are closed with it. The `501` on
  `PUT /api/v1/bundle/{path}` now says what it is — writing a file needs
  GCS (design doc 0013), and a concept is unaffected — rather than
  standing unexplained where a "not built yet" placeholder used to be.
  And `/api/v1/export`'s recipe for writing your own importer no longer
  sends the reader to a `POST /api/v1/knowledge` that does not exist:
  it is one PUT per bundle member, at the path it arrived at.

- **A file is an object in the bundle** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§3.3, 3.13). Migration
  0029 moves every `attachment` row into `object`, at the path the file
  lives at, and attribution stops being stored: a file belongs to the
  entry whose `<id>/` namespace it sits directly under, or to the entry
  whose body points at it.

  A bundle has nowhere to write ownership down, and it needs nowhere —
  what makes a diagram the metric's diagram is that the metric's document
  shows it. So the entry's file references are derived from its body on
  every write, the way its links have been since 0024, and the two halves
  of the derivation are one predicate every read goes through.

  Nothing on the wire moves: a file is still fetched, attached and
  detached by `(entry, filename)`, and `okf_path` still reports a file
  that lives somewhere other than the canonical `<id>/<name>` — derived
  now from the path rather than stored beside it. Two behaviours follow
  from the derivation rather than from a column, and both are what the
  design asks for: moving an entry carries the files under its namespace
  with it, and attaching a file at a path the entry's body says nothing
  about stores it at `<id>/<name>` instead of orphaning it.

  Operators: `attachment` and `attachment_embedding` keep their rows —
  the read paths move in this release and the tables go in a later one,
  the way 0024 kept `knowledge_revision.snapshot`. A file's bytes do not
  move; only the row that names them does.

- **The store is keyed by the bundle path** (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§2.1, 3.1). Migration
  0028 renames `knowledge` to `object` and moves its primary key to
  `path`, the address each object lives at; the revision ledger is
  re-keyed the same way, so a file's history will land beside the
  concept's instead of in a second place.

  Nothing on the wire moves. The concept id is still the address a
  concept is called by — SPEC §2's "the path with `.md` removed", which
  every ledger joins on and every surface names an entry by — and it
  keeps a unique index of its own. What changed is what the table is a
  table *of*: one bundle, mapping a path to an object, with a knowledge
  entry as one kind of object in it. Files arrive in a later change
  (§3.13), which is where the path stops being derivable from the id.

  Operators: the migration is one rename, two column additions and two
  primary-key swaps, all in one transaction. It rewrites no content and
  moves no timestamp.

- **BREAKING**: the two files OKF reserves are served at the paths OKF
  reserves them at (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§3.7-3.8).
  `GET /api/v1/bundle/{path}` answers `index.md` (SPEC §8's directory
  listing) and `log.md` (SPEC §9's update history); both are generated
  from the bundle, so `PUT` and `DELETE` against either are `409`.
  Any other path under `/api/v1/bundle/` is `404` on read and `501` on
  write: the rest of the bundle joins this address in a later change
  (0046 §3.5), and a refusal there says so rather than explaining
  itself in terms of two files the caller never named.

  - **`GET /api/v1/browse` is gone.** A directory listing is what
    `index.md` is, and ochakai was serving it at an address of its own
    invention. `GET /api/v1/bundle/metrics/index.md` is the same
    listing: with `Accept: application/json` it is the same JSON the
    browse endpoint returned, and without it, it is the file. The
    `prefix` parameter went with it — the directory is the path now.
  - **`GET /api/v1/revisions/{id}` is gone**, for the same reason.
    `GET /api/v1/bundle/{id}/log.md` returns the history, as JSON when
    asked for it and as the file otherwise, and a *directory's* log.md
    covers everything under it — which the revisions endpoint could not
    answer at all.
  - The history is published, never read back: an import skips both
    files, the same way it skips `generated` and `verified` (design doc
    0009). What happened in one instance is that instance's
    observation.
  - **CLI**: `ochakai browse` and `ochakai revisions` print what they
    always did, and read the new paths. `ochakai log` is new.
  - **Web UI**: unchanged on screen — the tree and the history tab read
    the JSON representation of the same two files.

- **BREAKING**: a read of one entry is a *view* — `{id, document,
  summary, observed}` — and a listing row is the `summary` alone (design
  doc [0043](docs/design/0043-document-first.md) §3.5). The flat JSON
  entry is gone from every read.

  - `GET`, `PUT`, `POST /api/v1/move`, `/verify/{id}` and `/reject/{id}`
    answer a view. `document` is the entry's canonical OKF document —
    the same text `ochakai get` prints — and it is *canonical*, so there
    are no server-owned keys to strip before editing it and sending it
    back. `summary` is the projection listing surfaces sort and filter
    by; `observed` is what this instance recorded (`created_by`,
    `generated`, the verification ledger, a live rejection). A client
    reading `k.title` reads `v.summary.title`, and one reading
    `k.verifications` reads `v.observed.verified`.
  - **A search hit is a projection, not an entry.** `hits[]` on
    `GET /api/v1/knowledge` (and `search_knowledge`, and
    `ochakai search --json`) carries id, type, title, description,
    resource, tags, status, `stale_after`, `verified` / `verified_at`,
    `rejected`, links, `content_hash` and the timestamps — plus `score`,
    `usage` on the feeds that populate it, and attachment metadata on
    REST. Bodies, sources and the computation contract are no longer in
    a result page: an entry is a document now, and shipping ten of them
    to answer "which of these should I read" spends the caller's context
    on the nine it will not. Fetch the ones worth reading by id, or ask
    `GET /api/v1/context`, which is the call that returns entries.
  - `GET /api/v1/backlinks/{id}` returns the same projection: a backlink
    names an entry, it does not hand it over.
  - `context.hits[]` gains `verified`, since the lifecycle status no
    longer carries it.
  - **CLI**: `ochakai get` prints the canonical document and nothing
    else, so `ochakai get <id> | $EDITOR | ochakai update <id>` needs no
    stripping; who created and confirmed the entry goes to stderr, where
    the attachment hints already go. `--json` prints the whole view, so
    `--if-match` reads `.summary.content_hash` and the newest
    confirmation is `.observed.verified[-1]`. `ochakai context` prints
    each entry's document rather than a hand-picked summary of selected
    fields.
  - **Web UI**: the detail page renders from the projection, with the
    body cut from the document and the frontmatter available raw under a
    "Document" tab.

- **BREAKING**: the web UI edits documents, and the form sugar is gone
  (design doc [0044](docs/design/0044-web-ui-edits-documents.md)). The
  editor is the canonical document in a textarea, checked for parse
  errors before it is sent; new entries start from a per-type template.
  The row editors for `sources` and `parameters`, and the contract
  section that appeared for an Attested Computation, are removed.

  They were genuinely useful, and losing them is a payment rather than a
  discount. The alternative was a hand-written YAML parser in the
  browser — a parser that "only ever sees our own output" is the kind
  that fails on an unexpected value, and here it would fail by silently
  dropping a field from a form. If the demand comes back, the answer is
  a server-side "give me this document as structure" surface, not a
  second wire shape for reads.

- **BREAKING**: a document is stored as it was written (design doc
  [0046](docs/design/0046-bundle-address-space.md) §§2.2, 3.4, 3.9). The
  bytes a writer sends are the bytes the entry holds: frontmatter
  comments, key order and quoting style survive a round trip, because
  nothing re-serializes them. Only the line endings are normalized, and
  the keys this instance owns are taken out line-wise on the way in and
  appended on the way out — so a document served by `ochakai get` can be
  sent straight back.

  - The **ETag is the hash of those bytes**, so reformatting an entry
    moves its version: the file did change. What the entry *says* did
    not, so `generated` stays with whoever the content already stood by
    — which needs a column of its own, `content_changed_at`, since
    `updated_at` had been standing in for it. It rides on the wire
    beside `updated_at`. Sending byte-identical bytes is still nothing
    happening: no row written, no revision, `Ochakai-Unchanged: true`.
  - A **recommended type keeps the writer's casing**. `type: metric` is
    stored as written rather than corrected to `Metric`; filters still
    match case-insensitively. The type is the writer's vocabulary
    (design doc [0038](docs/design/0038-type-vocabulary-realignment.md)),
    and rewriting it would also leave the index disagreeing with the
    document it is derived from.
  - **A read hands those bytes back.** The `document` field of a read —
    what `ochakai get` prints, what the web UI's editor loads, what
    `get_knowledge` returns — is the stored document, not a rendering of
    it. Storing the bytes and rendering them back would have kept the
    promise only for callers that asked for `text/markdown`, and every
    edit through the UI or the CLI would have reformatted the entry on
    its way past. The server-owned keys stay out of that field, as they
    always were: `observed` is where they are, which is what makes the
    field editable and sendable back as it came.
  - The canonical rendering stays as the form ochakai writes when it
    composes a document itself, and as what a write path falls back to
    when a caller changes an entry's fields rather than its document.

- **BREAKING**: body links keep only the two forms OKF defines (design
  doc [0046](docs/design/0046-bundle-address-space.md) §3.6).
  `[revenue](/metrics/revenue.md)` and `[gross](./gross.md)` are edges;
  `ochakai://metrics/revenue` is not one any more, in a link, an
  autolink, or bare prose.

  A body travels inside the bundle, so a scheme ochakai invented meant
  every export carried links no other consumer could resolve. Migration
  0026 rewrites the bodies that used it — a link target becomes the
  bundle-absolute path and keeps its anchor text, a bare or autolinked
  URI becomes a whole markdown link named by the id, since a bare path
  in prose is not a link and would have cost the entry an edge. The
  ETags of rewritten entries move; `generated` does not, because
  spelling a link the way the spec spells it is not a change to what the
  entry says. Revision snapshots keep the spelling they were written
  with.

  The scheme stays where portability is not the question: MCP still
  addresses an entry as `ochakai://{id}`, and `report_outcome` and the
  CLI still tolerate the prefix on an id argument.

- **BREAKING**: status and trust are said the way OKF says them (design
  doc [0046](docs/design/0046-bundle-address-space.md) §§3.9-3.10).

  - **A document that names no status is stable**, which is SPEC §5.4's
    default. ochakai no longer writes a `status` its writer left out —
    a create used to land on `draft`, and an update used to keep
    whatever was there. Both were the server authoring the writer's
    vocabulary. An agent that means "draft" writes `status: draft`, and
    the MCP tool descriptions say so.
  - **`verified` as a boolean is gone.** Entries carry `trust`, SPEC
    §5.3's tier derived from the verification ledger: `unverified`,
    `machine-confirmed`, `human-reviewed`. It rides on every entry and
    on every `/context` rank row beside `status`.
  - The filter follows: `?verified=true` becomes `?trust=` (repeatable
    and OR-ed) on REST and MCP, and `--verified true` becomes `--trust
    human-reviewed` on the CLI. Asking for two tiers is two values
    rather than a second kind of filter.
  - Nothing about verifying changes: it is still a ledger append, still
    absent from MCP, and still leaves the entry's version alone.

- **BREAKING**: knowledge is written as an OKF document, and `PUT` is the
  whole write surface (design doc
  [0043](docs/design/0043-document-first.md) §§3.5, 3.11).

  - `PUT /api/v1/knowledge/{id}` takes `text/markdown` — frontmatter and
    body, the same text `ochakai get` prints and an export writes, so
    read → edit → write is one loop with no translation in it. A JSON
    body is a `415` naming the document form. Keys the server owns
    (`generated`, `verified`, `created_by`, `rejected_*`) are ignored
    rather than refused, which is what makes a served document sendable
    back as it came.
  - **`POST /api/v1/knowledge` is gone.** PUT is create-or-replace: a
    document says what the entry should say and nothing about whether the
    id was taken. `If-None-Match: *` writes only if the id is free (an
    occupied one is a `409`), `If-Match` only if the entry still says
    what you read. A create answers `201`, a replacement `200`.
  - `GET /api/v1/knowledge/{id}` answers the document when asked with
    `Accept: text/markdown`; JSON is still the default.
  - Values the server read differently than they were written come back
    as `Ochakai-Note` response headers instead of being applied in
    silence.
  - **MCP**: `create_knowledge` and `update_knowledge` take `{id,
    document}`, and `get_knowledge`, `create_knowledge` and
    `update_knowledge` return the view under `knowledge`; `get_context`
    returns views under `entries`. The eighteen-field mirror of the
    envelope is gone — it was a fourth place the field list was written
    out, and an agent that trusted the enumeration could silently drop
    whatever it had fallen behind on. Notes come back in the tool
    result.
  - **CLI**: unchanged to use. `create` and `update` still read an OKF
    document or JSON from `-f`/stdin; JSON is now rendered to a document
    locally, so the server keeps one write format.

- **BREAKING**: the canonical OKF document is the stored form, and its
  hash is the entry's version (design doc
  [0043](docs/design/0043-document-first.md) §§3.1, 3.4, 3.7, 3.9).

  - **The ETag is now `content_hash`**, the SHA-256 of the entry's
    canonical document, also returned in the body. It is a hash of the
    content alone, so verifying an entry, rejecting it, or attaching a
    file to it no longer moves it — a held `If-Match` survives all three,
    which the `updated_at` version could not manage because it was also
    the row's write timestamp. Treat it as opaque: `If-Match` no longer
    validates a format, so an unrecognized value is a `412` rather than a
    `400`, and two entries that say the same thing carry the same version.
  - **A revision carries `document` instead of `snapshot`**: the entry as
    it stood, as an OKF document, rather than a JSON copy of the server's
    own struct. A diff between two revisions is now a text diff.

  Migration `0024` adds the columns; the server composes the documents
  once at startup, right after migrating, because rendering a document
  means writing YAML in the spelling the spec fixes and that is not
  SQL's job. The pass is idempotent and resumable, holds the same
  advisory lock the migrations do, and moves no `updated_at`. A revision
  snapshot the current shape cannot read is replaced by a placeholder
  document saying so; `knowledge_revision.snapshot` is kept (nullable)
  until a later release drops it, since it is the only source the
  backfill has.

- **BREAKING**: `status` is OKF's lifecycle vocabulary and nothing else —
  `draft`, `stable`, `deprecated` (SPEC §5.4). The two values ochakai
  added were never lifecycle values, and both become ledgers (design doc
  [0043](docs/design/0043-document-first.md) §§3.2-3.3):

  - **Verification** is an append-only list on the entry, which is what
    SPEC §5.3 means by `verified`. `verified_by` / `verified_at` are
    replaced by `verifications: [{by, at}]`, plural: re-checking an entry
    appends rather than overwriting, so the record answers "how often,
    and by whom" and not just "when last". `POST /api/v1/verify/{id}` no
    longer touches the document — the status, `updated_at` and the ETag
    all stay put, so confirming an entry cannot invalidate an editor's
    `If-Match`, and promoting a draft to `stable` is a separate PUT.
  - **Rejection** is a ruling, not a stage: `rejected_by` / `rejected_at`
    are replaced by `rejection: {by, at, note}`, written by the new
    `POST /api/v1/reject/{id}` and cleared by `DELETE` on the same path
    (`ochakai reject <id> --note …` / `--lift`). The reason moves from
    `status_note` to the ruling's own `note`. Export stops folding a
    rejection onto `deprecated`, which asserted the opposite thing about
    the entry; a bundle now carries the entry's real status.

  Filtering follows: `?verified=` and `?rejected=` on
  `GET /api/v1/knowledge` (and `?verified=` on `/context`), `--verified`
  / `--rejected` on the CLI, the same two on `search_knowledge`. Rejected
  entries stay hidden unless asked for, but a `status` filter no longer
  turns that exclusion off — "show me the drafts" never meant "including
  the ones we turned down". `search_knowledge` and `get_context` take
  `verified`, and `statuses` now means the lifecycle value alone.

  A foreign bundle's `stable` entry keeps its lifecycle value on import
  instead of being demoted to a draft: with confirmation held as a status
  there was nowhere to put "ready for consumption, unconfirmed", and that
  demotion is gone with the reinterpretation note that reported it.

  Migration `0023` moves every verified entry to `stable` plus one
  verification, every rejected entry to `draft` plus a rejection carrying
  its `status_note`, drops the four provenance columns, and rewrites the
  revision snapshots into the current shape. No `updated_at` moves — no
  held ETag is invalidated and nothing needs re-embedding.

- **BREAKING**: the actor kind for anything that is not a person is
  `process`, not `agent` — the spelling OKF SPEC §7 defines (design doc
  [0043](docs/design/0043-document-first.md) §3.8). `agent` was never a
  spelling the spec offered, and the distinction it drew is one nothing
  in ochakai ever read: SPEC §5.3 derives trust from the `human:` prefix
  alone. A client reading `actor.kind` must accept `process`, and
  `X-Ochakai-On-Behalf-Of: agent:…` is now a 400 — send `process:…`.
  Exported bundles write `generated: { by: process:… }` and
  `created_by: process:…`, so the next export of a git-tracked bundle
  shows that diff. Migration `0022` rewrites every stored actor kind and
  every `via` (which holds a whole `kind:name` string), in the entries,
  their revisions and revision snapshots, the usage events, the
  attachments and the purge log. It moves no `updated_at`, so no held
  ETag is invalidated and nothing needs re-embedding.

- `api/openapi.yaml`'s examples stop teaching `type: Golden Query` with
  the SQL in `attrs`. 0.15.0 retired that spelling and rewrote the shipped
  example and the canary guide onto `Attested Computation`, but the spec's
  own examples — including the first one a reader meets, the create
  request — kept the retired shape, which is what a reader of the wire
  surface copies. All five are now an Attested Computation with a
  `runtime` and the SQL in the body's `# Computation` fence. No schema,
  endpoint or status code changed; the guard that pins the vocabulary now
  reads the whole document rather than only the `Type` schema.

### Fixed

- `api/openapi.yaml` describes what a read-only deployment actually
  answers: the `403` every write gets and the `Ochakai-Read-Only: true`
  header stamped on every `/api/v1` response (design doc 0040 §2.3),
  neither of which the spec mentioned. The same `403` also covers a
  caller that sends `X-Ochakai-On-Behalf-Of` without being on the
  delegator allowlist (design doc 0027) — promised in prose there since
  it landed, declared on no operation. No behavior changed; the one REST
  contract (design doc 0015 §2) now says what the server has been doing.
  The read-only tests run through the spec-checking test server, so a
  status or a route the spec does not describe now fails the build
  rather than drifting quietly.

## [0.15.0] - 2026-07-28

### Added

- `examples/demo` — a ten-entry knowledge base loaded with
  `ochakai import examples/demo`, and now what the quick start registers.
  Entries link to each other, two are drafts and one is past its declared
  expiry, so `get_context`, backlinks, the review queue and the stale
  feed all have something to show on a base that is a minute old.
- A `prefix` filter on search and `context`, across all four surfaces
  (`?prefix=` repeated, MCP `prefixes`, `--prefix`, and a path box plus a
  "Search here" action on every directory page in the web UI). An entry's
  id is its address, so the path is also a scope to search within: several
  prefixes are OR-ed, so one call can ask a subtree and a shared one
  together, and each hit's id says which it came from. Matching is on
  segment boundaries, so `metrics` does not reach `metrics-legacy`; a
  trailing slash is optional. It composes with a query and with any
  `sort`, and in `context` it scopes the search that picks the hits, not
  the link expansion — an entry in scope still arrives with the glossary
  term it cites (design doc 0041). No migration, no index, and no
  behavior change for callers that pass no prefix. **This is a filter,
  not an access control**: any caller may pass any prefix, and passing
  none returns everything they could already reach (design doc 0002 is
  unchanged).
- `ochakai mcp-stdio` — speaks MCP on stdin/stdout and forwards to the
  selected server, for clients that can only launch a command and for any
  client against Cloud Run, where the request needs a Google ID token the
  client cannot mint. It resolves identity the way every other client
  command does, so the client is configured with no credentials; it
  listens on no port and copies JSON-RPC messages rather than holding a
  second copy of the tool schemas (design doc 0039). No server surface is
  added and the MCP tool count is unchanged.
- A Terraform module at `deploy/terraform` for the deployment the Cloud
  Run guide describes, so it can be reviewed as a diff, reproduced per
  environment and destroyed cleanly. Private IP, Vertex embeddings, GCS
  attachments and the IAP-fronted web UI are behind flags, all off by
  default. It creates no password and cannot run the §3 bootstrap SQL —
  `terraform output -raw database_bootstrap_sql` prints those statements
  instead. The gcloud guide stays the reference.
- Request and response examples throughout `api/openapi.yaml`, which
  previously carried exactly one. Every example is validated against the
  schema it sits under.
- `OCHAKAI_PUBLIC_READ_ONLY` — the posture for a deployment anyone may
  reach, so a demo can exist without breaking the project's own rule
  about public services. It **reads no identity at all**: the
  Authorization header is ignored, delegation is ignored, every caller is
  `human:anonymous`, and nobody is refused. That is not a relaxation but
  the opposite — without Cloud Run IAM in front nothing verifies a
  token's signature, and until now a public deployment would have
  accepted a forged one naming any person while turning away the
  anonymous visitor a demo is made of. It implies `OCHAKAI_READ_ONLY`
  and cannot be separated from it, so a publicly readable *and writable*
  ochakai is not a configuration the server accepts; setting it with
  `OCHAKAI_INSECURE_DEV` is refused at startup (design doc 0042).
  Default: off, and the private posture is untouched — a caller with no
  token is still a 401 there.
- `OCHAKAI_READ_ONLY` — serve knowledge without changing it. Every write
  is a 403, the MCP surface does not offer the write tools at all, and
  the web UI stops drawing the buttons that would only fail. For a
  reference-only instance, a public demo, or freezing a base during a
  migration. It is not authorization: it does not look at the caller, and
  it refuses the operator too (design doc 0040). Usage recording
  continues, being the server's own observation rather than a write a
  caller asked for. Default: off, so nothing changes for a deployment
  that does not set it.

### Changed

- **BREAKING** — the recommended type vocabulary is realigned to what OKF
  itself spells out, going from nine types to eleven (design doc 0038).
  The test is SPEC §4.1's one demand of a producer — that a spelling be
  descriptive and self-explanatory — applied in both directions.
  - **Retired: `Semantic Model` and `Golden Query`.** `Semantic Model`
    failed the test: since 0.13.0 removed compile and its write-time
    validation, all that remained was a convention to put an Apache Ossie
    object in `attrs.spec`, which nothing on the server reads and no
    spelling explains. `Golden Query` is what an `Attested Computation`
    with a `runtime` already is — SPEC §10.2 requires only `runtime` and
    leaves `parameters`/`executor`/`attester` optional — so store a
    golden query as one: the SQL in a `# Computation` body fence, the
    question in `attrs.question`.
  - **Added: `Skill`, `Playbook`, `Policy`, `API Endpoint`.** `Playbook`
    and `API Endpoint` are SPEC §4.1 examples, and §4.4 writes a Playbook
    out in full as its example of a concept bound to no resource.
    `Policy` and `Skill` come from the reference bundles, and they close
    a real gap: an entry's `sources[].resource` cites a Policy and its
    `executor.resource` points at a Skill, so ochakai was storing both
    ends of an edge while naming only one.
  - **No migration runs and no stored entry changes.** The retired
    spellings stay valid to write and are matched, searched, exported and
    updated exactly as before — they are now free types (design doc
    0005). The type is not renamed for you, because `runtime` is required
    on an Attested Computation and only the author knows it.
  - `domain.TypeModels` and `domain.TypeQueries` are gone from the Go
    API. `--type 'Golden Query'` still returns those entries.
  - No server behavior attaches to any of the four new types. The one
    type-specific rule stays the `runtime` requirement on an Attested
    Computation (design doc 0036 §3.10).
  - The vocabulary was previously written out in eleven places at three
    different lengths, with `Semantic Model` already missing from four of
    them. Every list that Go emits — the MCP tool descriptions and all
    three shell completions — now comes from `domain.TypesHint()`, and
    the static ones (OpenAPI, the Web UI, the MCP jsonschema tags) are
    pinned by tests that fail when a type is missing or a retired
    spelling is still recommended (design doc 0038 §4.4).
- The shipped example `examples/golden-query.md` and the canary guide are
  rewritten onto `Attested Computation`. The example's SQL moves out of
  `attrs.sql` into its `# Computation` fence: SPEC §10.2 makes the fence
  the computation, and keeping a second copy left a consumer no way to
  tell which was authoritative.

### Fixed

- Web UI: a plain markdown link to an attachment is drawn as a link. Since
  design doc 0013 an attachment is any file, not only an image, and a plain
  link is the only notation that can reference a non-image one — but the
  renderer resolved links against entries alone, so those references stayed
  raw markdown on the page while the attachments tab printed exactly that
  form for authors to paste. Both notations now resolve through the same
  file resolver: an entry link still wins, and a reference matching no
  attachment still stays literal text rather than becoming a dead link.
- The 400 a search without a query or a sort returns names all four listing
  modes. It had named three since before `stale_after` shipped in 0.14.0,
  so REST and MCP callers were told a way out that omitted one; the CLI's
  message was already current. The sentence now reads `domain.ListSorts`
  instead of restating it.
- Importing an OKF bundle that declares `status: stable` with no
  `verified` entry no longer demotes it to draft silently. ochakai has no
  status meaning *stable and unverified*, so such an entry does land as a
  draft and exports as one — that loss is real and unchanged — but the
  import now reports it, the way every other reinterpretation already
  did. Refusing to read `stable` as reviewed stays deliberate: OKF puts
  human review in `verified` (SPEC §5.3). The README claimed the reverse
  mapping "keeps the round-trip exact", which held for ochakai's own
  exports and not for a foreign bundle; it now says which is which.
- Shell completion offers `--source` on `ochakai search` and
  `stale_after` as a `--sort` value. Both shipped in 0.14.0 and reached
  none of the three scripts: the sort values were a hand-written subset
  in the test, and the per-command flag list agreed with the scripts that
  `--source` did not exist. The sort values now come from
  `domain.ListSorts`, so a new one fails the test until every script
  offers it.
- The per-command flag list itself is no longer transcribed: it is read
  from the commands' own FlagSets, so a flag cannot be missing from the
  completions and from the list that checks them at the same time — which
  is how `--source` stayed invisible for two releases with every test
  passing. Adding a flag to a client command now fails the test until
  zsh, bash and fish all offer it. `ochakai mcp-stdio --url`, which fish
  had never offered because that command was absent from the old list
  altogether, is the first thing this caught.

## [0.14.0] - 2026-07-27

The baseline for everything below is **0.13.0**, not 0.12.x: OKF v0.2
export itself shipped in 0.13.0.

### Added

- Seven OKF v0.2 keys become first-class envelope fields on every surface
  (REST / MCP / CLI / Web UI): `sources` and `usage_window` (SPEC §5.1),
  and the Attested Computation contract `runtime`, `parameters`,
  `computation`, `executor`, `attester` (SPEC §10.2). The rule is now
  "the keys OKF defines are envelope fields; `attrs` carries producer
  extension keys and nothing else" (design doc 0036). Holding
  `executor`/`attester` is not running them — ochakai still never fetches
  a resource, executes SQL, or calls an LLM (design doc 0001).
- `Attested Computation` joins the recommended type vocabulary
  (design doc 0036 §3.6, revising 0023 §3.1).
- `sort=stale_after` — a feed of entries whose declared expiry has passed,
  most overdue first. It differs from the other two feeds: `verified_at`
  and `failed` are cleared by verifying, `stale_after` is cleared by
  **editing the entry** to declare a new expiry (design doc 0037 §2.2).
- `source=<uri>` — reverse lookup of the entries citing a given source.
  Exact match on `sources[].resource`, single-valued, and a filter rather
  than a mode, so it composes with a query and with `sort=stale_after`
  (design doc 0037). Both reach MCP as one new `sort` value and one new
  argument on `search_knowledge`, not as new tools.
- Web UI: row editors for `sources` and `parameters`, a contract section
  shown by type, sources rendered under the prose they support, a third
  feed chip at `#/search/stale`, and each source linking to what else
  cites it.
- Machine-checked invariants: golangci-lint rules for exhaustiveness, a
  contract test of the REST surface against `api/openapi.yaml`, and fuzz
  targets for the OKF parser and the export/import round trip
  (design doc 0035).

### Changed

- **BREAKING** — `attrs` loses the keys that moved into the envelope.
  Clients reading `attrs.sources` and friends must read the envelope
  field. Migration 0020 moves the values and deletes the keys.
- **BREAKING** — writing an `attrs` key that shadows an envelope key is
  now a 400. In 0.13.0 it was accepted silently and then vanished on
  export.
- **BREAKING** — exported frontmatter follows the SPEC's family order.
  0.13.0 placed the `status` family ahead of trust. Values are unchanged,
  but a diff against an older export will move.
- **BREAKING** — per-source and per-parameter producer extension keys are
  dropped (with a note). Top-level extension keys still pass through.
- **BREAKING** — legacy ochakai ≤0.12 status spellings are no longer read
  back on import. A bundle with `status: verified` still imports as
  verified, through the `verified` key SPEC §5.3 makes authoritative, not
  through an ochakai alias; `status: rejected` now imports as draft.
- An unknown `status` accompanied by a `verified` key now imports as
  verified. 0.13.0 forced any unrecognized status to draft, discarding the
  trust signal (design doc 0036 §3.4).
- Write-path validation for the new fields: an Attested Computation
  without `runtime`, a source without `resource`, a non-date
  `last_modified` or `usage_window`, a parameter missing `name` or `type`,
  and an executor missing `receipt` are all 400. Bundle import stays
  permissive and reports a note instead.
- The YAML library moved from the archived `gopkg.in/yaml.v3` to
  `go.yaml.in/yaml` v3.0.4 — same API, same call sites, and the upstream
  fix for the bug below.

### Fixed

- Export dropped a leading newline in a title or description, so
  re-importing the bundle gave back a different entry.
- Export could emit frontmatter the parser then refused to read: a
  multi-line title, description, tag or attr whose first line opens with a
  space, tab or newline came out as a block scalar with the wrong
  indentation, and the document was skipped on re-import. Such values now
  go out double-quoted; ordinary multi-line prose still exports as a block
  scalar.
- Web UI: clearing a source's `resource` in the edit form threw before the
  validation banner could appear, so Save did nothing and said nothing.

### Migrations

0020 moves the envelope values out of `attrs`. It does not advance
`updated_at`, so held ETags and `generated.at` are unaffected, and no
re-embedding is needed — the embedding text never read these keys. 0021
adds two indexes for the stale and source lookups and touches no row.

## [0.13.0] - 2026-07-26

The largest release so far: four migrations, ten new design docs, and the
write-back loop finally closing — an agent reports a bad answer, the entry
surfaces in a re-verification feed, a human verifies it, and the
provenance says who did what.

### Removed

- **BREAKING** — the whole compile surface is gone (design doc 0028):
  the MCP tool `compile_sql`, `POST /api/v1/compile`, `ochakai compile`,
  and the Web UI's compile form. ochakai never executed SQL, but it did
  assemble it, which put a type-specific behavior registry in a product
  whose position is that types are a vocabulary. Nobody was using it as of
  0.12.1, so it was removed rather than deprecated. The `Semantic Model`
  type keeps working as an ordinary type; callers that compiled must build
  their own SQL from the entry body.

### Added

- `POST /api/v1/verify/{id}` and `ochakai verify <id>` record a human's
  verification against the entry as it stands — promoting a draft,
  re-affirming a verified entry, or clearing a rejection. Previously only
  the Web UI could do this (design doc 0025).
- `sort=failed` — a re-verification feed ordered by failed outcome
  reports, alongside `sort=verified_at` and `sort=usage`.
- `purge`: a deliberate second stage of delete that frees a tombstoned id.
  `DELETE /api/v1/knowledge/{id}?purge=true` and `ochakai purge <id>`.
  Purging a live entry is a 409, the purge is recorded in
  `knowledge_purge`, and it is deliberately **not** on MCP or the Web UI
  (design doc 0031). It does not reclaim attachment bytes.
- Optimistic locking: opt-in `If-Match` on update, so a concurrent edit
  fails with 412 instead of overwriting (design doc 0030).
- Delegated identity — a caller acting for an end user passes that user
  through as `A via B`, so provenance records the human rather than the
  integration (design doc 0027).
- `updated_by` (who produced the content as it now stands) and
  `stale_after` (the OKF lifecycle date) on every surface.
- An HNSW index on the pgvector column when the dimension is within the
  index limit, with `ef_search` sized to the search's own limit.

### Changed

- **BREAKING** — exports speak OKF **v0.2** (`okf_version: "0.2"`).
  `timestamp` becomes `generated.at`, the body's `# Citations` heading
  becomes `sources`, and `verified_by`/`verified_at` become a one-element
  `verified` list whose *presence* carries the trust tier. `status` maps
  to the OKF vocabulary: `draft` stays `draft`, `verified` becomes
  `stable` plus `verified:`, and `deprecated`/`rejected` become
  `deprecated` with a `status_note`. Import still reads v0.1 spellings, so
  old bundles load.
- **BREAKING** — `get_context` changed shape: knowledge travels once under
  `entries`, and `hits` is purely the ranking behind the pack. The byte
  budget covers the whole response, and entries that do not fit come back
  as outlines within it (design doc 0033). Search results are unaffected.
- Lexical search was rewritten to be answerable and indexable: a stored
  `search_text` column with a working trigram index, query-fragment
  matching with IDF weighting, and scoring over an id projection.
- Usage recording moved off the read path — buffered and flushed
  periodically rather than written on every search, with flush failures
  logged (design doc 0029).
- Embeddings take their input budget from the model, truncate on rune
  boundaries, refuse to start when the configured dimension changes under
  an existing corpus, and `reembed` now runs to completion, covering
  attachment vectors.
- Export streams from a single snapshot, can leave attachments out, and no
  longer claims a determinism it did not have.

### Fixed

- `create` can no longer resurrect a tombstone.
- Move rewrites the moved entry's own relative links to absolute form,
  folds that into the move revision, and locks the referrers it rewrites.
- Verification timestamps are stamped at stored precision, so what you
  read back is what was written.
- Rejections answer in the documented error envelope; boolean query
  parameters are rejected rather than read as unset.
- `serve` waits for `Shutdown` to finish before returning, and the SIGTERM
  drain leaves room for the final usage flush.
- Empty search queries are a 400 instead of a Postgres 500, and `ILIKE`
  wildcards are escaped.

### Security

- MCP can no longer overwrite or delete an entry a human has decided on —
  the whole human-decided set, not only `verified`. Agents write freely up
  to the moment a person signs off.
- `serve-ui` takes the browser user from the IAP assertion and drops
  client-supplied identity headers unconditionally (design doc 0032).
- MCP resolves the actor from each call's own HTTP headers, on every tool,
  not only on writes.

### Migrations

0016–0019 run at start: the `search_text` column and its index,
delegated-provenance columns, the purge audit table, and
`updated_by`/`stale_after` with a backfill from revision history. They are
additive, but the `search_text` rebuild and the backfill both scan the
full table — expect the first start on a large corpus to take longer than
usual. Existing embeddings stay valid.

## [0.12.1] - 2026-07-21

Drop-in from 0.12.0: no migrations, no schema changes, no request or
response shape changes.

### Fixed

- Type filters match case-insensitively, as 0.12.0 promised but only
  delivered on the write side. `type=metric`, `type=METRIC` and
  `--type "bigquery table"` returned **zero** results against entries
  stored as `Metric` / `BigQuery Table`. `domain.FoldType` (NFC + trim +
  lowercase) is now the single comparison key and the store folds the
  column with SQL `lower()` (design doc 0023 §3.3). Type filters also
  NFC-normalize now, like every other byte-compared key. The stored
  spelling is still exactly what the writer used.
- The Web UI renders entry links as links. 0.12.0 made body markdown links
  the source of an entry's edges, but the renderer only linkified
  `http(s)` targets, so `[revenue](/metrics/revenue.md)`, a bare
  `ochakai://…` and autolinks all showed as plain text. Bundle-absolute,
  relative and canonical-URI forms now resolve by the same rules the
  server's extraction uses.
- Markdown inside an inline code span was rendered as markup. Emphasis,
  images and links now apply outside code spans only — matching how the
  server skips code when extracting links.

## [0.12.0] - 2026-07-20

Two vocabularies leave ochakai: types become the OKF spelling verbatim,
and links become what the body actually says. Both are the same
correction — ochakai had invented structure OKF left to the document, and
paid for it with translation layers that could not round-trip.

### Changed

- **BREAKING** — `type` values are the OKF vocabulary verbatim across
  REST, MCP and CLI (design doc 0023): `models` → `Semantic Model`,
  `metrics` → `Metric`, `queries` → `Golden Query`, `insights` →
  `Insight`, `terms` → `Glossary Term`, `datasets` → `BigQuery Dataset`,
  `tables` → `BigQuery Table`, `references` → `Reference`. There are no
  aliases: `--type tables` does not error, it returns zero results.
  Types now contain spaces, so shell usage needs quoting and REST query
  parameters need URL encoding. `ValidType` relaxes to a one-line,
  slash-free, 128-byte free string, so a foreign bundle's non-ASCII type
  survives as itself, and import/export are identity on type.
- **BREAKING** — `links` in a write payload is ignored, on REST, MCP and
  the Web UI alike. Edges come from the body: write
  `[revenue](/metrics/revenue.md)` in the prose and the entry links to
  `metrics/revenue` (design doc 0024). Five notations are read —
  bundle-absolute, relative, `ochakai://` in a markdown link, autolinks,
  and bare `ochakai://`. Links inside fenced or inline code are examples,
  not edges. The links column survives as a derived index recomputed on
  every write, so backlinks and `get_context` are unchanged.
- **BREAKING** — `Link.rel` becomes `Link.text`, and its meaning changes
  from relationship type to anchor text. Nothing in the codebase ever
  branched on `rel`.
- **BREAKING** — `okf_type` is no longer emitted, and export no longer
  writes a `# Links` section (the links are already in the body, and
  writing them twice would duplicate them on re-import).
- The Web UI's new-entry form takes one id field, breadcrumbs link to each
  ancestor, and the Links tab is read-only.

### Fixed

- Renaming an entry repairs the prose. `Move` used to rewrite the links
  column and `attrs.model` while leaving the author's markdown links
  pointing at an id that no longer existed. It now rewrites the body —
  each notation keeping its own form, code blocks untouched — and
  re-derives links from the repaired text.

### Migrations

0014 renames the eight stored type values and their revision snapshots;
an entry carrying an `okf_type` attr has that spelling promoted back to
`type` first, so the author's original spelling wins. Existing embeddings
stay valid — the embedding text never included type. 0015 writes existing
links back into the body as `- [<rel>](/<target>.md)` before the column
becomes derived; without it those edges would vanish the next time an
entry was written. Revision snapshots are left alone.

## [0.11.0] - 2026-07-20

The filename becomes the name (design doc 0022) — the follow-through on
0017, which made the path the address.

### Added

- The id is searchable: `k.id` joins the lexical haystack and the ILIKE
  substring floor, so `用語/売上` is findable by 「売上」. The id also
  prefixes the embedding text, since paths carry the domain hierarchy.
- Directory index pages in the Web UI: clicking a directory renders the
  equivalent of the `index.md` OKF export generates at each level —
  subdirectories with counts, entries with title and description. The
  browse API projection gained `description` to make that worthwhile.

### Changed

- **BREAKING** — `title` is no longer required, in the OpenAPI document
  and the MCP schemas. The display name is the title when present and the
  **id's last segment** otherwise, applied uniformly across Web UI, CLI
  and export. This is closer to OKF SPEC, which only ever required `type`.
  Clients assuming a non-empty title need a fallback.
- **BREAKING** — an empty title is a missing key, not `"title": ""`, and
  export drops the `title:` line for title-less entries.
- **BREAKING** — byte-compared keys normalize to NFC, so NFD spellings no
  longer round-trip. macOS hands back decomposed path names, so unpacking
  a bundle on a Mac and re-tarring it could silently split one entry into
  two identical-looking ids. Normalization applies at every boundary where
  a byte-compared string enters — service entry points taking an id, link
  targets, attachment ids and names, bundle import paths, search queries.
  Content columns (body, title, description) stay exactly as written.

### Migrations

0013 rewrites stored ids, link targets, revision-snapshot ids, event and
usage keys, attachment names and paths, and embedding-table keys to NFC.
On ASCII data it is a no-op scan. A normalization collision — two ids
becoming one — **fails the migration loudly** rather than merging
silently. Existing entries keep their stored vectors until written again.

## [0.10.3] - 2026-07-20

### Added

- `move`: changing an entry's address (design doc 0021). Because the id
  *is* the address, this cannot be an update to one column.
  `POST /api/v1/move` and `ochakai move <from> <to>` rewrite every place
  the id is recorded in a single transaction — body, revision history,
  attachments (metadata and bytes), usage records and embeddings follow
  the new id, and a `move` revision is appended. Inbound references are
  rewritten each keeping the spelling it was written in (`links[].target`
  in both the bare and `ochakai://` forms, and `attrs.model`), and
  referrers get an `update` revision so the rewrite is visible in their
  history. The destination must be free — a live or soft-deleted entry
  there is a 409. A `GET` on the old id is a 404.
  `move` is deliberately **absent from MCP**: reorganizing the knowledge
  base is a human editorial act.
- Web UI: a Move action and tree drag-and-drop; a **＋** on directory rows
  that opens the new-entry form prefixed to that directory; a sidebar
  toggle whose choice persists.

## [0.10.2] - 2026-07-20

### Added

- Attachment search (design doc 0020). Filenames join the lexical
  haystack, so searching `seeds` finds the entry carrying `seeds.txt` —
  on every instance, embedder or not, effective immediately for existing
  attachments. Attachment contents join hybrid search as a third
  reciprocal-rank-fusion list via a new `attachment_embedding` table; a
  hit is always the owning entry, never a standalone attachment.
- `gemini-embedding-2` support for embedding images and PDFs (set
  `OCHAKAI_VERTEX_MODEL=gemini-embedding-2` and
  `OCHAKAI_VERTEX_LOCATION=global` — the model exists in `global`/`us`/`eu`
  only). `text/plain` embeds through the text path and works with any
  model; on a text-only model, images and PDFs stay findable by name.

### Changed

- The Vertex driver speaks two wire dialects, selected by model name:
  `gemini-embedding-2*` uses `v1 :embedContent` with task instructions
  folded into the prompt and file bytes as `inline_data` parts, while
  earlier models keep `:predict` + `task_type`. The default model stays
  `gemini-embedding-001`.
- No backfill: only new attaches embed, matching the write-time-only
  posture of entry embeddings. Switching an existing knowledge base to
  `gemini-embedding-2` leaves previously stored vectors in the old model's
  space until entries are written again; there is no reindex mechanism.
  Filename search needs no backfill.

## [0.10.1] - 2026-07-19

### Changed

- **BREAKING** — `ochakai serve-ui` requires `OCHAKAI_URL` and fails fast
  without it. The static-page-only mode made no sense once the page
  stopped accepting a base URL. Deployments following the deploy guide
  already set it.
- The folder tree becomes the Web UI's persistent left sidebar rather than
  a tab beside search (revising design doc 0014 §2.2). Opening an entry,
  deep links included, auto-expands its ancestors and highlights it; the
  tree refreshes after create, delete and status changes. Home is a
  tree-first landing with a search box, full search moved under **Search**,
  and the legacy `#/browse` route redirects home. **Export OKF** moved from
  the top bar to the home page's quick links (same `/api/v1/export`).
- The base-URL chip is removed (revising design doc 0006). Both serving
  paths proxy `/api/v1` with the right credentials, so a configurable base
  URL could only break CORS or authentication — and a mistyped value
  persisted until cleared. Opened as a plain file, the page targets a
  local `ochakai serve`.

### Fixed

- Concurrent server starts no longer race on migrations. `Migrate`
  serializes across processes with a Postgres advisory lock. Previously,
  several instances starting at once against an unmigrated database — a
  scaled Cloud Run deployment, parallel CI test binaries — could each see
  a migration as unapplied and apply it twice, the second failing against
  the schema the first had changed, or double-rewriting ids on a database
  with data.

## [0.10.0] - 2026-07-19

A restructuring release: the knowledge hierarchy is freed from the
type-first layout, the type vocabulary aligns with the OKF
knowledge-catalog reference bundles, and semantic models become ordinary
knowledge entries.

### Changed

- **BREAKING** — the path is the address (design doc 0017). An entry's id
  is its full bundle path and the sole primary key; type is metadata, not
  a location. Domain-first (`sales/orders`), type-first
  (`metrics/revenue`) and foreign-bundle-shaped (`ga4/tables/orders`)
  layouts are all first-class and the server forces none.
  - MCP address-taking tools (`get_knowledge`, `get_attachment`,
    `get_knowledge_usage`, `delete_knowledge`) take a single `id`; agent
    configs and prompts need updating. The resource template is
    `ochakai://{+id}`.
  - REST routes are `/api/v1/{knowledge,usage,revisions,backlinks}/{id...}`
    and the legacy `/knowledge/{type}/{id}/usage` alias is gone. `type` is
    required in create and update bodies.
  - CLI refs are ids (`ochakai get metrics/revenue`).
  - Bundle import takes the frontmatter `type` as the only type source —
    files without one are skipped and reported, never guessed from the
    path. Archive unwrapping and `--keep-root` are gone: a wrapper
    directory is how a bundle keeps its namespace.
  - Export drops the `id:` frontmatter line and orders the index
    alphabetically throughout — a one-time diff against old exports.
- **BREAKING** — id segments follow OKF's contract (a concept id is a file
  path, nothing more), so only path safety constrains them: no leading
  `.`, no control characters, 128 bytes per segment (design doc 0019).
  Leading underscores, non-ASCII and spaces are all valid. Types remain
  slugs — they are vocabulary, not paths.
- **BREAKING** — the recommended type slugs become plural and gain two
  more, matching the OKF knowledge-catalog reference bundles
  directory-for-directory: `metrics`, `queries`, `insights`, `terms`,
  `tables`, `datasets`, `references` (design doc 0016). `resource` is
  promoted from attrs to an envelope field.
- **BREAKING** — the compile `dialect` option is gone from REST, MCP, CLI
  and the Web UI: ochakai is BigQuery-only, so output is always BigQuery
  SQL.
- `ochakai import` treats a server 400 as that one document's skip —
  reported, with its attachments — instead of aborting the bundle halfway.

### Removed

- **BREAKING** — `import-ossie` (the CLI command, `POST
  /api/v1/import/ossie`, and the derivation logic) is gone. A semantic
  model is now a `models` entry carrying the Ossie model object verbatim
  in `attrs.spec`, with revisions, provenance, verification, search and
  OKF export/import (design doc 0018). Broken models are rejected at write
  time rather than when someone asks for SQL, and `compile_sql` resolves
  the model from the models entries themselves.

### Migrations

0010–0012 rewrite type slugs and ids, move `resource` to a column, convert
`semantic_model` rows to `models/<name>` draft entries, and drop the
table. Existing ids migrate to `<type>/<id>` — the spelling every
serialized reference already used, so stored references keep resolving.
They abort safely on id collisions; resolve, then re-apply.

## [0.9.1] - 2026-07-19

A documentation and module-metadata release — no behavior changes to the
server, CLI or web UI.

### Changed

- `go.mod` carries `retract [v0.1.0, v0.8.0]`. With this tag published,
  `go install …@latest` and pkg.go.dev treat pre-0.9.0 versions as
  retracted: 0.9.0 is the first release the documentation is written
  against, and earlier images predate the Go 1.26.5 stdlib security fixes.
  The per-version upgrade notes for 0.2.x–0.8.0 collapsed into a single
  summary; the details remain in git history.
- The Cloud Run deploy guide is field-verified end-to-end — every
  previously unverified step exercised on a real deployment and corrected
  where reality disagreed: the IAP web UI path (the manual service-agent
  binding is gone, `cloudresourcemanager.googleapis.com` is now required),
  the "retire the last password" sequence (migration-created tables are
  owned by the runtime service account, so the admin role must be
  `REASSIGN`ed / `DROP OWNED` before deletion), the org-policy guardrails,
  private-IP-only setup, and the backup flags (`--backup-start-time`, not
  a bare `--backup`; PITR must be disabled before `--no-backup`).

## [0.9.0] - 2026-07-19

The first supported release. Upgrading from ≤0.7.x with attachments in
Cloud SQL requires stopping at 0.8.x first — see below.

### Added

- `ochakai serve-ui`: the web UI from `examples/webui` becomes a
  first-class command.
- Attachments beyond images (design doc 0013). The allowlist is now
  png / jpeg / webp / pdf / text-plain — the intersection of what Claude
  accepts as uploads and what `gemini-embedding-2` takes as input.
  `text/html` and `image/svg+xml` stay rejected (XSS).
- Folder browsing (design doc 0014): a one-level browse API and a tree
  view in the web UI.
- CLI parity with REST: `browse`, `revisions` and `backlinks`
  subcommands.
- Idempotent import: unchanged entry content no longer creates a revision
  or bumps `updated_at`.

### Changed

- **BREAKING** — 0.8.x is a required upgrade stop. The one-shot
  bytea→GCS attachment backfill was removed, and 0.9.0 refuses to start
  against unmigrated blobs. GCS is now the only blob store; the Cloud SQL
  bytea column is retired.
- **BREAKING** — legacy nested `attrs:` OKF frontmatter is no longer
  special-cased; `attrs` round-trips as an ordinary extension key.

### Removed

- **BREAKING** — the MCP OAuth connector service is retired (design doc
  0012). `internal/connector`, the `OCHAKAI_CONNECTOR_*` configuration and
  the `oauth_*` tables are gone (migration 0008 drops them).
  `OCHAKAI_CONNECTOR_PUBLIC_URL` is now silently ignored like other
  retired variables.
- **BREAKING** — `image/gif` leaves the attachment allowlist. New GIF
  attaches are rejected; already-stored GIFs remain readable and served,
  and re-importing an old bundle reports GIFs as skipped.

### Security

- Built with Go 1.26.5, which includes upstream stdlib security fixes.
  Hardened CI, Dependabot, and pinned actions. Images are published with
  SBOM and SLSA provenance (mode=max) for linux/amd64 and linux/arm64.

## [0.8.0] and earlier - 2026-07-15 – 2026-07-18

**Retracted and unsupported.** `go.mod` carries
`retract [v0.1.0, v0.8.0]`: 0.9.0 is the first release the documentation
is written against, and earlier images predate the Go 1.26.5 stdlib
security fixes. `go install …@latest` and pkg.go.dev will not offer these
versions.

The one thing to know: **0.8.0 is a required upgrade stop** for anyone
running ≤0.7.x with attachments stored in Cloud SQL, because it carries
the one-shot bytea→GCS backfill that 0.9.0 removed.

This range covers the initial public work — the Cloud Run deploy guide,
passwordless Cloud SQL IAM auth and the Google-Cloud-only posture (design
docs 0002, 0003), the API-only CLI (0004, 0007), OKF bundle interop
(0005), the two web UI serving paths (0006), image attachments and their
move to GCS (0008, 0011), the MCP OAuth connector service and its
retirement (0010, 0012), `get_context`, and the write-back loop
(`report_outcome`, rejected status, usage telemetry). One security fix
worth naming: SQL injection in `compile_sql` through undeclared field
pass-through, fixed in 0.8.0 — v0.7.0 and earlier are affected. Details
are in git history.

[Unreleased]: https://github.com/na0fu3y/ochakai/compare/v0.15.0...HEAD
[0.15.0]: https://github.com/na0fu3y/ochakai/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/na0fu3y/ochakai/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/na0fu3y/ochakai/compare/v0.12.1...v0.13.0
[0.12.1]: https://github.com/na0fu3y/ochakai/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/na0fu3y/ochakai/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/na0fu3y/ochakai/compare/v0.10.3...v0.11.0
[0.10.3]: https://github.com/na0fu3y/ochakai/compare/v0.10.2...v0.10.3
[0.10.2]: https://github.com/na0fu3y/ochakai/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/na0fu3y/ochakai/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/na0fu3y/ochakai/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/na0fu3y/ochakai/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/na0fu3y/ochakai/compare/v0.8.0...v0.9.0
