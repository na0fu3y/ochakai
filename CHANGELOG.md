# Changelog

All notable changes to ochakai are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) — while
the major version is 0, a minor bump may break compatibility, and those
breaks are marked **BREAKING** below. **The OKF core of REST is frozen
at `/api/v1`** — the bundle round trip and search; the rest of
`/api/v1`, MCP, the CLI and the stored shape are still unstable at 0.x,
and only the latest release is supported:
[docs/compatibility.md](docs/compatibility.md) states the policy in
full.

Dates are the tag dates (JST). Each released version also has a GitHub
release with longer prose; this file keeps what an operator upgrading
needs to know. Database migrations run automatically at server start
unless a note says otherwise.

Releases before 0.9.0 are retracted in `go.mod` and unsupported — see the
last entry.

## [Unreleased]

### Added

- **The web UI reads the way a documentation service does — three
  affordances, no new API.** All three are the curation surface doing
  its own job (design doc 0067 §1: search, browse, rulings, history)
  over endpoints that already existed, so REST, MCP and the CLI are
  untouched. Ctrl+K (⌘K on a Mac) opens a **quick-open palette** from
  any page: type, arrows, Enter, and the concept is open — nothing typed
  lists by demand, the same `sort=usage` the search view lands on, and a
  last row hands the words to the full search view. A long body's
  overview tab now opens with its **outline** (目次) — every heading
  gets an anchor id, drawn from the rendered body rather than by parsing
  the markdown twice. And the history tab shows each revision as a
  **diff** against the one before it, `+n −n` beside the change, with
  the unchanged middle elided and the full document still one disclosure
  away — what a reviewer deciding whether to verify actually needs from
  a revision is the three lines that moved.

## [0.27.3] - 2026-08-25

### Added

- **A deployment that cannot hold files says so, and the page stops
  offering one.** Without `OCHAKAI_GCS_BUCKET` an instance stores
  markdown concepts only — every read, write and listing of a file
  answers 501 — and the web UI went on drawing "ファイルを選択…" and
  "ファイルを追加" on every concept. You found out by clicking, and what
  came back was the name of an environment variable. `GET /api/v1/stats`
  now answers `files.enabled`, the page reads it, and the upload
  affordance is gone where it could only have been refused; the tab says
  which deployment this is instead. `ochakai stats` prints one line for
  the same reason it prints the posture lines: whoever never opens a page
  still has to be told (design doc 0131).

- **The variable that would turn it on goes to whoever can set it.** The
  same answer carries `files.variable` — `OCHAKAI_GCS_BUCKET` — but only
  for a caller who holds the whole bundle, which is the test the rest of
  the administrator's operations take, and on a deployment that has
  written no grants is everybody. The web UI turns it into one banner;
  everybody else is simply not handed a sentence they cannot act on. The
  page does not decide who is an administrator: the field is on the
  answer or it is not.

  Upgrading a client: **`files` absent is not `files.enabled: false`**.
  A server that predates this says nothing here, and a client that reads
  absence as "off" would hide a working upload on every older
  deployment.

### Fixed

- **A grant at the root reaches every read now, not only the ones
  addressed by id.** `{"prefix": "", "principal": "*"}` is how a policy
  says *everyone reads the whole bundle* (design doc 0109 §2), and with a
  few `may_write` rows beside it, it is how a deployment says *a few
  people edit it*. It half worked. The read scope travelled into the
  filter as `[""]`, which the store rendered as `id = '' OR left(id, 1) =
  '/'` — a condition no id satisfies, since none is empty and none begins
  with a separator — so a caller granted the whole bundle could fetch any
  concept by name and find none of them by **search**, by **browse**, or
  in the **numbers**. A listing was the one read that worked, which is
  what made the gap easy to miss.

  Nothing had to be spelled unusually to meet it: the empty prefix is
  what `ochakai access` and 0109 both describe as the whole bundle. **A
  policy that names directories was never affected**, and the shape it
  forced — a row per top-level directory, where a directory added later
  stays invisible until somebody adds a row — is no longer the only one
  that works.

- **`ochakai stats` under a grant at the root counts the whole bundle and
  says so.** It counted nothing before, for the reason above. The answer
  now carries no `scope`, which is how *the whole bundle* is spelled
  (design doc 0123 §2 — the field is about the numbers, not about the
  caller), and the unanswered searches travel with it. §3 withholds those
  from a scoped caller so that one team is not told what every other team
  searched for; a caller granted the root has no other team, and on a
  deployment with no policy at all every caller already reads them.

  **Reading everything is still not administering everything.** The
  archive of the base, `move`, `reembed` and the policy itself stay with
  `OCHAKAI_ADMINS` (0109 §3), and a write still needs `may_write`.
  `ochakai access -h` says both halves now, and the operating guide's
  *ディレクトリごとの閲覧者と編集者* carries the shape the fix makes
  writable: everyone reads, a few principals write, one row each.

## [0.27.2] - 2026-08-24

### Added

- **The web UI edits frontmatter in fields again, and still saves a
  document.** The editor is two panes now: the frontmatter on the left,
  the document on the right, and one document under both. Every key OKF
  asks a writer for has a field — `sources` and `usage_window`,
  `parameters`, `executor` and `attester`, `runtime` and `computation`,
  alongside the title, description, tags, status and `stale_after` that
  were already there — so adding a citation or ticking a parameter's
  `required` no longer means counting YAML indentation by hand. What is
  saved is the text in the right pane, exactly as before (design doc
  0130, which folds 0072, 0092 and 0126 into one record).

  **Nothing you did not name moves.** A field edit replaces that key's
  block and leaves every other byte where it was: key order, comments,
  blank lines, the quoting of untouched values, the body, and every
  producer key the form has no field for — which the page names on
  screen rather than hiding, so you can see it is keeping them.
  `generated` and `verified` get no field: provenance is what the
  instance observed, not something to type.

- **`POST /api/v1/frontmatter`** — the face the editor uses, and yours
  to use. It takes an OKF document and hands one back: with only
  `document` it reads the frontmatter as JSON (`keys` in the document's
  own order, `values` as YAML said them, extension keys included), and
  with `set` / `unset` it writes named keys back into the document and
  returns it. **It stores nothing** — no id, no ETag, no precondition —
  so a read-only deployment answers it. The keys the server owns are a
  400 in both directions. Not on the CLI or MCP: it is a tool for
  drawing a form, and an agent writes the document directly (design doc
  0130 §6).

- **A move runs when its rewrite fits inside what the caller may write.**
  `ochakai move`, `POST /api/v1/move`, and the web UI's move — the same
  three addresses, refused until now to anyone an access policy scoped
  (design doc 0129). Three things have to be the caller's to write and
  they are the three the move writes: the concept, the destination, and
  every live concept that links at the concept. A team renaming a
  concept inside its own directory is that case, and it no longer needs
  an administrator.

  **Refused whole, never narrowed.** When something the caller may not
  write links at the concept, the move does not happen — not even the
  part of it that would have been in scope. Skipping those rewrites
  would leave links pointing at an id that is gone, which is the one
  thing a move exists to prevent, and a half-done move is worse than a
  refused one. Ask an administrator, whose move is unbounded.

  The refusals are three and they say different things. A destination
  outside the caller's scope is a **403 naming it** — nothing is stored
  there, so saying so answers no question about what the bundle holds.
  A concept outside the scope is the **404** a read outside the scope
  gets. A concept referenced from outside is a **403 that names no
  referrer**: what the caller learns is that something, somewhere, links
  at their own concept — no address, no count, no content.

  **Nothing changes on a deployment with no access policy**, which is
  every deployment that has not written a rule: there is no scope, so
  there is nothing for a move to fit inside. No endpoint, parameter,
  flag or tool is added — `export` of the whole base, `reembed` and the
  access policy itself are what stay with the administrators.

### Changed

- **The web UI's access policy is edited as rows, not as JSON in a
  textarea.** Deciding who may read `personnel/` is an operator's
  judgement, and it was costing them a hand-spelled `human:` prefix, a
  pair of booleans whose implication they had to remember, and a JSON
  syntax error as the first thing they met when they got one character
  wrong. The editor is the same table the policy is read as: a directory
  (completed from the tree, empty for the whole bundle), who it is for
  (the actor kind as a choice, the name beside it), and what they may do
  there as one of 読取のみ / 読取・書込 / 読取・書込・付与 — the
  containment spelled out instead of inferred from two flags. Rows are
  added and removed in place; the row a refusal is about is named *and*
  marked.
  **Nothing about the wire moved** (design doc 0109 §5): the save is
  still one `PUT /api/v1/access` carrying the whole document with the
  `If-Match` it was read at, so a policy is still replaced as a set and
  a concurrent edit is still refused rather than clobbered. The document
  itself is one disclosure away under 「JSON として扱う」 — written from
  the rows whenever it is opened, which is what `ochakai access --json`
  prints and `-f` takes, so the git review path is unchanged and a
  policy prepared there is pasted back with 「行に取り込む」. The
  read-only rule is unchanged too: on a frozen deployment the whole
  editor is absent, as it was.

### Fixed

- **A frontmatter this instance's keys could not be appended to no
  longer breaks the document.** The trust family ochakai publishes
  (`generated` / `verified` / `created_by`) is appended to a stored
  document's frontmatter as lines, and the lines were written at column
  0 whatever column the frontmatter was at. A mapping indented by a
  space — unusual YAML, and valid YAML — is *ended* by a key less
  indented than itself, so what came back out of `GET` with
  `Accept: text/markdown`, out of `ochakai get`, and out of a bundle
  export was a document that no longer parsed, carrying keys the write
  path could then no longer take off again. The same append ran on the
  way in, where it stored such a document in a form that no longer
  parsed and dropped the claim it had just taken out of it.

  The line surgery beside it had the same shape of fault twice more, and
  both times what was lost was a key the writer owned rather than one of
  ours. A flow mapping's keys do not have lines of their own, so taking
  out "the lines this key occupies" took the neighbour with it:
  `{type: Metric, generated: x}` arrived with a type and was stored
  without one. And the parser counts a bare carriage return as a line
  break where the surgery counts only `\n`, so a frontmatter holding one
  was numbered a line ahead of where the cut landed — `generated` was
  read on one line and a *different* line was removed.

  The keys are now written at the frontmatter mapping's own column, and
  the append checks its own work: it goes on only if taking it off again
  gives back the document that came in. A frontmatter whose lines do not
  answer to keys is not addressed at all — a flow mapping, one numbered
  in line breaks the cut does not count, and one carrying a `...` or
  `--- ` marker, where the mapping stops before the block does — and one
  the keys cannot be appended to is left exactly as it arrived: no
  surgery in either direction — nothing is taken out of it on the way in and
  nothing is put back on the way out, so it still round-trips byte for
  byte, and what it loses is only this instance's provenance *in the
  markdown export*. Every other reader of provenance is unaffected: the
  JSON representations, the trust tier and `trust=` read the ledgers,
  never the document. Nothing ochakai writes and nothing written by hand
  in the ordinary way is one of these shapes, so no stored document
  moves — the only documents this reaches are the ones it was
  corrupting.

- **A rejection note whose prose opens with `#` no longer leaves a line
  behind.** The same line surgery reads a `#` at the start of a line as a
  comment introducing the key that follows, and hands it to that key
  rather than to the one above. Indented, it is not a comment: a
  `rejected_note: |` whose last line of prose opens with `#` is inside
  the block scalar, and taking `rejected_note` off the export form left
  that line orphaned in the frontmatter of a document the writer then
  stored. A comment is now a `#` in the first column, and a blank line
  after the *last* key stays with it — `note: |+` keeps its trailing
  blank line, and there is no next key for it to introduce.

- **Normalizing a stored document is idempotent, so sending back what a
  read handed you answers `unchanged`.** The two normalizations a write
  applies — CRLF to LF, and exactly one newline at the end (design doc
  0075 §3) — did not hold on a text whose carriage returns sat against a
  line ending. `\r\r\n` had its CRLF replaced and was left holding a new
  one, and a document ending in a bare CR kept it and had a newline
  appended after it: both stored a CRLF, out of a document the CRLF rule
  had just been applied to, and only the *next* normalization folded it.
  The user-visible effect was a `PUT` of the exact bytes a read returned
  answering `plan: updated` and writing a second revision of identical
  content.

  **This moves the content hash of a stored document that ends in a bare
  CR or contains a `\r\r\n`** — the first write after upgrading rewrites
  it once, with the carriage returns gone, and settles there. A carriage
  return in the middle of a line is nobody's line ending and still
  stays.

## [0.27.1] - 2026-08-23

### Fixed

- **A subtree archive names itself in Japanese too.** The filename half
  of design doc 0127 folded every non-ASCII character to a hyphen, so
  the two shapes it exists to prevent came back for the directories
  this product's first audience actually has: `営業` arrived as
  `ochakai-okf-.tar.gz`, one hyphen from the whole-base backup's name,
  and `teams/成長` arrived as `ochakai-okf-teams.tar.gz` — **an archive
  of one directory under the name of every directory beside it.** Those
  characters are percent-encoded now, and the unescaped name travels
  beside them in `filename*` (RFC 6266), which is the one browsers
  prefer. **The whole base and a subtree the ASCII name already spelled
  exactly send the byte-identical header they sent in 0.27.0**, and the
  root `index.md`'s `bundle_scope` — the other half of 0127, which was
  correct throughout — is unchanged. No endpoint, parameter or header
  name is added.

## [0.27.0] - 2026-08-23

### Added

- **An archive of one directory can be taken by anyone who may read it,
  and says that is what it is.** `ochakai export --prefix teams/growth`,
  or `Accept: application/gzip` on that bundle path — the address that
  has always read as a subtree. The whole base stays with the
  administrators (design doc 0127).

  The refusal this lifts was aimed at a danger of a different shape, and
  measuring said so: `ochakai import` never deletes, so restoring an
  incomplete export does not lose the rest — and **the hazard it did
  name was already reachable**, because a subtree archive arrived with a
  root `index.md` and the filename `ochakai-okf.tar.gz`, indistinguishable
  from a whole-base backup. So the archive now names itself twice: the
  root `index.md` carries `bundle_scope` in its frontmatter and a
  sentence saying what is missing, and a subtree downloads as
  `ochakai-okf-<subtree>.tar.gz`. **Whole-base archives and every deeper
  `index.md` are unchanged.**

  For an organization living in a directory of a shared deployment, this
  is the exit that C1 promises, without the operator having to run it.


- **A directory can have its own administrator.** An access rule gains
  `may_admin`: the rules *under that prefix* are that principal's to
  edit (design doc 0124). Until now handing a team its own directory
  meant handing it `OCHAKAI_ADMINS`, and therefore the power to delete
  every other team's boundary — and an automation that places one grant
  per organization had to hold that permanently. A prefix administrator
  reads only the rules they may edit, **and the rules they never saw
  survive their write**, so the ordinary `ochakai access --json > f;
  edit; ochakai access -f f` flow is safe to hand them. The `ETag` they
  get covers what they read, so a rule they cannot see moving does not
  fail their write; a rule they send outside their prefixes is a 403
  rather than a silent drop. **`may_admin` at the root is refused** —
  who may edit the whole policy is what `OCHAKAI_ADMINS` answers, and
  putting that answer inside the policy is the revolving door 0109 §3
  named. It implies `may_write`. Migration 0045 adds the column,
  additive and defaulting to false, so every existing policy means
  exactly what it meant.

  One thing it does not reach, stated rather than closed: a rule
  *shallower* than the delegated directory still governs it and stays
  invisible to that directory's administrator, so the effective access
  to a subtree can be wider than the rules its own administrator can
  see.


- **`GET /api/v1/stats` answers a caller who holds part of the bundle,
  and the answer says what it counted.** Design doc 0109 §3 pooled these
  numbers on the administrator because a subset would give the loop's
  numbers a second meaning — but `prefix` could already produce that
  subset, so what made it a second meaning was the answer never saying
  which one it carried. It says now: a `scope` field carries the
  prefixes counted, absent when they were the whole bundle, and an empty
  list means the caller can see none of it (so the zeros are zeros for
  that reason, not because nothing happened). An administrator asking
  about one subtree gets the same declaration — the field is about the
  numbers, not the caller. `ochakai stats` prints it as a `scope` line
  and the queue hints name the same subtree (design doc 0123).
- **The unanswered searches are withheld from a scoped caller rather
  than narrowed**, and say so: a miss carries no concept id, so there is
  nothing to narrow it by, and showing it unnarrowed would tell one team
  what every other team searched for. `misses.withheld` on the wire,
  `misses withheld` in the CLI — a different word from the existing
  `misses -`, because the remedy is to ask an administrator rather than
  to change `OCHAKAI_RECORD_MISSES`. `export`, `move`, `reembed` and the
  access policy stay with the administrators.


- **The access policy can be replaced conditionally.** `PUT
  /api/v1/access` takes an `If-Match` precondition and both operations
  answer with an `ETag`; a stale one is a 412 and nothing is written
  (design doc 0120). The policy is one document read, edited and put
  back whole, so two administrators who each add a rule left only the
  second one's rule — no error, and the only signal was the person who
  got dropped saying they could no longer read something. It matters
  most where grants are placed by automation rather than by hand, which
  is the shape design doc 0119 describes. Absent means last write wins,
  as before, so nothing that works today stops working. The version is
  in the body as `version` too — the way a concept's
  `summary.content_hash` sits beside its ETag — so the document
  `ochakai access --json` prints carries what a conditional write of it
  needs, and `ochakai access -f` takes it straight back. `If-None-Match`
  on this address is a 400: the policy always exists, so there is no
  create to guard.
- **`ochakai access --if-match <version>`**, and the printed policy ends
  with a `version` line. No new spelling: it is the flag `put` and
  `delete` already carry.
- **The web UI's access editor sends the version it read.** A save that
  would overwrite somebody else's edit now stops and says so, leaving
  what you typed in the box — what the concept editor already did.

### Changed

- **The MCP listings say how long they hold.** `tools/list`,
  `resources/list` and `resources/templates/list` now answer with a TTL
  hint of five minutes; `resources/read` keeps answering zero (design doc
  0118 §7). This is a correction rather than an addition: protocol
  2026-07-28 added the field, the SDK has been filling it in on every
  response with zero, and the spec reads zero as *consider this
  immediately stale* — so ochakai was telling every client its tool list
  was already out of date, which nobody had decided. The three lists are
  registered once at startup and never change while the process runs, so
  what the number really bounds is how long a client may go on believing
  a process that has since been replaced; five minutes keeps a deploy
  visible inside its rollout and sits well inside the one release a
  renamed tool name keeps answering for. A concept is knowledge and
  changes when somebody writes it, which is why the read is not cached.
  Nothing is added to an agent's context and no counted surface moves.
- **The web UI starts where the knowledge does.** A caller granted one
  directory (design doc 0109) opened the page on a root holding one
  directory holding one directory, and two clicks stood between them and
  their own knowledge every time it loaded. The sidebar and the home
  listing now walk past the levels that hold **one directory and nothing
  else**, and both begin at the first level with something to choose
  between. A line above the tree names the path walked and links back to
  the bundle's root.

  It hides nothing: the step is taken only when the level has no concepts
  and no files of its own, so what is skipped is a corridor rather than a
  room. Ids are unchanged everywhere — a walked page still spells
  `teams/growth/metrics/revenue` in full, because a second spelling of an
  address is what this must not buy. An administrator whose base lives
  under a single directory gets the same walk for the same reason, and no
  setting turns it on or off.


- **BREAKING (non-core REST): a JSON request body is matched exactly and
  may name each key once.** `{"RULING":"verified"}` used to be a 200:
  design doc 0064 §2 decided an undeclared body key is a 400 naming it,
  and `encoding/json` matched names without regard to case, so a
  spelling the contract never declared reached the handler anyway —
  `DisallowUnknownFields` reports keys that match no field and says
  nothing about a key that matches one it should not. The same decoder
  silently took the last of a repeated key, so
  `{"ruling":"verified","ruling":"rejected"}` was answered as
  `rejected`: a decided answer where the caller cannot know which of the
  two values decided it, on the operation that records a ruling. Both
  are now a 400 naming the field, the shape an unrecognized query
  parameter already had (design doc 0125). The four operations that read
  a body — `POST /api/v1/move`, `POST /api/v1/review/{id}`, `POST
  /api/v1/usage/{id}`, `PUT /api/v1/access` — are outside the frozen OKF
  core (design doc 0107), so this is BREAKING here and carries a record.
  The CLI, the web UI and any client generated from `api/openapi.yaml`
  produce neither shape. Bodies are read through `encoding/json/v2`,
  whose defaults are these two refusals; responses keep v1, because v2's
  marshaling defaults are bytes the frozen surface actually ships.


- **The Terraform module moves to the Google provider 7.x, and the web UI
  stops going through `google-beta`.** IAP on Cloud Run went GA in March
  2026 and `iap_enabled` landed in the GA provider at 7.21, so the one
  reason the `webui` service was declared against the beta provider is
  gone. `google-beta` is still declared, for one resource that is still
  beta — `google_project_service_identity`, the IAP service agent — and
  the module README now says that instead of the old reason. Checked with
  `terraform init` and `terraform validate` against the resolved provider
  (7.45.0); none of 7.0's breaking changes touch this module, and the one
  that came closest — `google_project_service.disable_on_destroy` losing
  its default — is a value this module already set explicitly. The
  operating guide's `gcloud beta run deploy --iap` and `gcloud beta iap
  web` become plain `gcloud`, both verified present on the GA surface.
- **Both services ask for a startup CPU boost.** `min_instance_count` is
  0, so a request arriving after an idle period pays for a start — the
  pool connects, the migration table is read, Vertex AI is asked once
  whether it answers. The boost is full CPU for that window and billed
  for that window. No readiness probe: Cloud Run already withholds
  traffic until the process listens, and ochakai listens only after
  migrating, so a probe on `/health` would restate what the port already
  says.
- **The image's runtime base is `distroless/static-debian13`.** distroless
  resolves a bare tag to whatever it currently considers newest, and
  debian13 is where that points today; naming the version keeps the base
  from moving on a day nobody chose. Nothing else about the image
  changes — still static, still nonroot, still no shell and no package
  manager — and the built image was checked end to end, including
  reaching a deployment over TLS.
- **Built on Go 1.27**, and migration 0044 carries the Unicode 17
  character class that comes with it. The CJK class the lexical index
  splits on is generated from Go's own
  `unicode.Han`/`Hiragana`/`Katakana` tables and pinned to them by
  `TestCJKClassAgreesWithGo`; Unicode 17 gives Han 645 more characters
  than Unicode 15 did, and the pin reported every one of them the first
  time the suite ran on 1.27 — which is what it is for. **The migration
  recomputes every row's search index**, the way migration 0043 did;
  rehearsed on a base written at 0043, where Japanese, halfwidth and
  English searches all answer the same before and after. Nothing an
  operator sets changes.
- **`go.mod` now names 1.27 as a floor rather than a preference**, which
  is new: a build older than the class in the database windows fewer
  characters than the index holds. The pin fails rather than letting that
  ship, and the floor is what keeps it from being reached. Everything
  else about 1.27 was checked rather than assumed — the whole suite
  passes, frozen wire golden files and OpenAPI contract validation
  included, with `encoding/json` on its new v2 implementation.
- **What the new characters still cannot do**, recorded in
  `TestWhatPostgresCanIndexAboveTheBMP`: all 645 live above the BMP, and
  what becomes a lexeme up there is decided by PostgreSQL's Unicode
  knowledge rather than by ochakai's class. PostgreSQL 17 reads CJK
  extension I as blank on both the document and the query side, so a term
  written in it is found by nothing — no class can change that, and the
  day a PostgreSQL lifts the limit is a day that test fails and says so.
- **A filtered semantic search answers with more of the answer.**
  pgvector's HNSW scan runs before the `WHERE` clause, so a search for
  ten concepts narrowed by type, tag or path got only whatever survived
  of its `ef_search` candidates — on a 3,000-row fixture whose filter
  matched one row in a hundred, one of the ten asked for, with nothing in
  the response saying the other nine were never looked for. Sizing
  `ef_search` to the limit, which is what ochakai did, is a guess at how
  much a filter will discard and is wrong for some filter by
  construction. Searches now ask for an iterative scan
  (`hnsw.iterative_scan`, pgvector 0.8.0), which keeps walking the graph
  instead of stopping at those candidates. **It lifts the ceiling rather
  than guaranteeing the answer**: measured over twelve fixtures it was
  never worse, better in half of them, and complete in one, and what ends
  the scan is the graph running out of reachable matches rather than a
  budget — raising `hnsw.max_scan_tuples` and `hnsw.scan_mem_multiplier`
  moved none of it. `strict_order` rather than the faster
  `relaxed_order`, because a hit's position is what the fusion ranks on. **Nothing to configure**: the
  store reads the installed pgvector's version once while migrating, and
  a server older than 0.8.0 keeps exactly today's behaviour, so this adds
  no requirement to run ochakai (Cloud SQL ships 0.8.1). Deployments
  small enough that PostgreSQL prefers a sequential scan were never
  affected — there the answer was always exact.
- **Log lines carry `severity` and `message`, the two names Cloud
  Logging reads.** Everything ochakai logs is JSON through `slog`, whose
  own keys are `level` and `msg` — ordinary fields as far as the platform
  is concerned, so an entry arrived with no severity of its own and every
  line ranked equal. An operator could not ask for the warnings, and the
  warnings are the point: semantic search silently fallen back to
  lexical, an export truncated after the response began. `severity>=WARNING`
  now returns exactly the table in [the operating
  guide](docs/guides/operating.md), and its saved queries, which already
  spelled the field `jsonPayload.message`, are true as written. `WARN`
  is spelled `WARNING` on the way out because that is Cloud Logging's
  word for it; nothing else about a line moves, `time` included. The
  destination is unchanged and stays stderr — stdout is the wire for
  `ochakai mcp-stdio` and the output of every client command, which two
  comments and one guide paragraph had said backwards.
- **`/mcp` is stateless, and answers at MCP protocol version
  2026-07-28** (design doc 0118). ochakai speaks MCP through the Go SDK,
  and that SDK serves the current protocol only from a stateless handler
  — a session-holding one caps every client at 2025-11-25. ochakai built
  its handler with default options, so it shipped the newest SDK and
  refused the newest spec, silently: negotiation succeeds, the tools
  work, and a client that has moved is never told it was moved back. The
  mode being given up was never used — no roots, no sampling, no logging
  (all three deprecated at 2026-07-28), no server-initiated request, and
  ochakai's state is the knowledge base rather than the connection. No
  `Mcp-Session-Id` is issued or read, and GET and DELETE on `/mcp` are
  405 with `Allow: POST`. **Nothing an operator sets changes**, and no
  counted surface moves.
- **What this costs, for a client still on 2025-11-25**: at that version
  a client names itself only in `initialize`, which a stateless
  deployment answers in a throwaway session, so its `clientInfo` no
  longer reaches the `producer` field beside the actor — measured, a call
  that recorded `claude-desktop/1.2` now records nothing there. The
  identity is untouched: `created_by` is the same person or process it
  always was, and what falls is the self-declaration design doc 0065 §4
  calls a record rather than an identity. The `Ochakai-Producer` header
  is the contract for that field and covers it on any client, and a
  client that moves to 2026-07-28 gets the fallback back automatically —
  there, the name rides on every call. Sessions also stopped
  accumulating (the SDK never expires an idle one), and a deployment is
  free to run more than one instance, which sessions in one instance's
  memory had ruled out.
- **The MCP tools say whether a retry is safe, and that the world they
  reach is closed.** Two hints were left at their spec defaults, and both
  defaults were wrong here. `openWorldHint` defaults to true — "this call
  goes somewhere unbounded" — where every tool on this surface reads or
  writes this deployment's own base and reaches nothing else; it is false
  on all six now. `idempotentHint` is where the two writes stop being one
  thing: `put_concept` is a replace at an address, so the same document
  written twice lands the same concept and an identical write is recorded
  as no revision at all, while `report_outcome` is another outcome every
  time it is called, which is the point — a client that retried it would
  move a counter the curator reads. They no longer share an annotation.
  Nothing an operator sets changes, no tool is added or removed, and the
  hints are annotations rather than schema, so no counted surface moves.
- **`scripts/check core` runs `go fix -diff`**, and the tree it now holds
  still. Since Go 1.26 `go fix` is not the pre-1.0 API renamer its name
  comes from: it runs the modernizers, and `-diff` prints the patch
  instead of applying it, exiting non-zero when it is not empty — the
  shape design doc 0035 §3.1 asks of a check, silent on a clean tree, so
  a finding always means new code written in an older idiom. Unlike the
  linter there is no version to pin, because it ships with the toolchain
  `go.mod` already names. Applying it once touched twelve files and
  changed no behaviour: `new(expr)` for the four `&x`-returning helpers,
  which are gone now that their callers say it directly; `reflect.Pointer`
  for `reflect.Ptr`; one `if` that was spelling `min`; a loop-variable
  copy Go 1.22 made redundant; and embedded struct literals written as
  the fields they set. What it did *not* report is worth as much, because
  the fixers for those ran as well: `slicessort` read twelve
  `sort.Slice` and `sort.SliceStable` calls and left every one, and
  `stringsseq` left all eight `range strings.Split` loops — seven range
  over an index, which a value-only iterator cannot hand back. Both had
  been proposed as sweeps by a reading of the tree; the tool is what
  settled them.

### Fixed

- **A deployment that verifies its own tokens (`OCHAKAI_OIDC_ISSUER`)
  now reads `Authorization`, so it can run behind Cloud Run IAM.**
  It could not before: the header precedence written for the
  Google-verified path applied to both, so such a deployment picked up
  the token Cloud Run forwards — signature stripped — tried to verify it
  against the operator's issuer, and answered **401 to every request**
  while the caller's real credential sat unread in `Authorization` of
  the same message. The failure read as cryptography for what was a
  header in the wrong place. That configuration is Google's own
  two-header split working as documented, and it is a useful one on its
  own: reachability from Cloud Run IAM, identity from your own IdP. The
  Cloud Run path is unchanged, where the precedence still has its
  reason. A request carrying only `X-Serverless-Authorization` to an
  OIDC deployment is now refused by name (design doc 0121).


- **A caller who is not an administrator can no longer write the first
  access rule.** On a deployment with no grants every caller passes the
  whole-bundle check — that is design doc 0109 §2's promise that no rules
  means no boundary — so `PUT /api/v1/access` used to accept a first rule
  from anyone, commit it, log `access policy replaced`, and *then* answer
  **403**, because re-reading the policy on the way out ran the same check
  against the boundary that had just been created. The status code is the
  one that says nothing was written, so nothing that saw it could tell a
  refused write from a landed one, and an automated caller retrying
  replaced the policy again each time. The refusal now happens before the
  write, naming the lockout: a policy is edited by the principals
  `OCHAKAI_ADMINS` names, and placing the first rule is what hands the
  policy to them (design doc 0122, amending 0109 §3). **Nothing that
  worked stops working** — every call this refuses already ended in a 403,
  since the check on the way out could never pass. Clearing the policy is
  unchanged, as is the separate 400 a deployment that names no
  administrators gets. The web UI's access editor now says why a save was
  refused instead of passing the message through.
- **`ochakai import` no longer reads through a symlink out of the bundle,
  and `ochakai export` no longer writes through one out of the directory
  it was given.** A bundle is ordinary content — cloned, unpacked, handed
  over on a drive — so a link inside one named `revenue.md` and pointing
  at `~/.ssh/id_rsa` used to be read and written to the knowledge base
  under that id: a file the importer never looked at, published to
  everyone the deployment serves. The walk declined to *descend* a
  symlinked directory and said nothing about reading a symlinked file,
  which is the half that reads something. On the way back out,
  `filepath.IsLocal` reads the name an archive carries and cannot see
  that a directory already on disk is a link somewhere else. Both paths
  now go through `os.Root`, so the kernel refuses a name that leaves the
  directory instead of the walk's shape implying it; the name check
  stays, because the two answer different questions. An ordinary bundle
  imports exactly as before.
- **`GET /api/v1/access` answered `{"rules": null}` on a deployment with
  no grants**, where `api/openapi.yaml` declares `rules` a required
  array. Every deployment starts in that state; no integration test had
  read an empty policy over the wire, so the contract check had nothing
  to check, and a client generated from the spec would have refused the
  most ordinary response there is. It is `[]` now (design doc 0120 §6).

### Security

- **Both web-UI commands refuse a cross-site write.** `ochakai ui` has
  refused one since it was written; `ochakai serve-ui` — the deployed,
  IAP-fronted one — had no such check, and design doc 0072 §1.2's line
  that the two modes invert in "whether a browser guard is needed" is
  what left it that way. What inverts is the bind address and where
  identity comes from, not whether a write arriving from a browser can
  be trusted, and the side without the check is the side where a browser
  holds a credential it did not have to be given. A non-safe request
  (anything but GET, HEAD and OPTIONS) that a browser says came from
  another site is now a 403 before the proxy signs and forwards it
  (design doc 0126). **Nothing that is not a browser changes**: the
  check reads `Sec-Fetch-Site`, falling back to comparing `Origin` with
  `Host`, and a request carrying neither header is allowed — so the CLI,
  an agent over MCP, curl and a generated client send exactly what they
  sent before, and no endpoint, parameter, header, flag or environment
  variable is added. Reads are still served, `/health` included.
  `ochakai serve` is deliberately unchanged: no configuration today lets
  a browser reach it holding a credential. One tightening on `ochakai
  ui`: an `Origin` naming a different loopback spelling than the `Host`
  it reached (`http://localhost:PORT` against `127.0.0.1:PORT`) is now
  refused — no browser produces that pair.


- **A release carries its attestations as a file beside the archives**,
  `ochakai_X.Y.Z.intoto.jsonl`. The archives and the image have carried
  provenance attestations for a long time, but the attestations lived only
  in GitHub's attestation store, which `gh attestation verify` reads and
  two readers cannot: an OSS
  review that walks release assets — OpenSSF Scorecard's Signed-Releases
  scored this repository 0 while every artifact was signed — and anyone
  verifying without reaching github.com, who now has `gh attestation
  verify <file> --bundle <this>` offline. The guarantee is unchanged; what
  changed is that it is reachable. The upload happens before the release
  is published, for the reason the step above it already gives.
- **Both base images are pinned by digest as well as tag.** A tag is a
  name its owner can repoint — `nonroot` is rebuilt in place — so the same
  commit built twice was not necessarily the same image. Dependabot reads
  the tag-and-digest pair and opens the pull request that moves both, so
  the pin is a value somebody bumps in a commit rather than one that goes
  stale.
- **The release archives are byte-reproducible.** goreleaser stamped the
  build's wall clock into the binaries, so rebuilding a tag produced
  different archives and a different `checksums.txt`; they now carry the
  commit's own timestamp, which is a property of the tag rather than of
  the runner. `-trimpath` was already removing the other half.
- **No workflow leaves its token in the checkout.** `persist-credentials:
  false` on every one of them now — it was set on the Scorecard job alone
  and missing from the other eight, none of which pushes with git: the
  release jobs talk to GHCR through the registry login and to `gh`, which
  reads its token from the environment. The token in `.git/config` was
  readable by every later step and used by none of them.
- **govulncheck runs weekly as well as on every commit**, because a
  vulnerability published against an already-pinned dependency arrives on
  somebody else's schedule. The other four jobs in that file decline a
  scheduled run.
- **CodeQL analyses the Go on push, on pull requests and weekly.** The
  linters in `.golangci.yml` are local by design and none of them can see
  a request parameter reach a SQL string; this is the analysis that
  follows a value across packages. It reports into code scanning beside
  Scorecard's SARIF and does not gate a merge. Design doc 0035 §3.1's
  rule applies to it too: if it reports on a clean tree, it comes out.

## [0.26.3] - 2026-08-22

### Added

- **The loop's numbers on the review page can be asked about a period
  and a path.** `GET /api/v1/stats` has taken `days` and `prefix` all
  along (design doc 0069 §5); the page asked for neither, so the only
  reading a browser offered was the last 30 days over the whole base —
  a team sharing a deployment could not see its own month, and nobody
  could look back over a quarter without the CLI. Two controls sit above
  the numbers now, and the numbers are drawn as two bands: what the base
  *is*, and what happened *inside the window* — the split design doc
  0069 §5 already made field by field, which is what makes it visible
  that the period moves the second band and not the first. Two flow
  numbers the wire carried and the page did not draw join them: concepts
  created, and outcomes reported worked and failed. **Scoped, the
  unanswered searches beside them are still the whole instance's** and
  cannot be otherwise — a search that found nothing found it nowhere, so
  there is no id to scope it by (0069 §5.1) — and the reading says so
  rather than letting that number pass as the team's. Nothing is added
  to the wire: no endpoint, no parameter, no word. The review trend
  stays the one shape with no axes that design doc 0095 §3 gave it, and
  the queue depths a curator works from are untouched.

### Changed

- **The re-verification feed has one name again.** `sort=failed` was
  called four things at once: 「再検証フィード」 in five design records
  (0025, 0067, 0076, 0090), 「要レビュー」 in the manual, 「誤りの報告」
  in the web UI, and 「再検証のフィード」 in that same UI's own banner —
  which told the reader to uncheck a box it had just called something
  else. Design doc 0090 and `docs/loop.md` made the same claim, that
  this is the feed verifying empties, under two of those names. The
  records cannot move, so everything else moved to them: the filter
  chip, the queue badge, the home page and the review page say
  「再検証」, and the manual stops naming the pair of feeds at all —
  「再検証キュー」 covered both while 「再検証フィード」 meant one.
  Nothing changes about what the feed holds or how it empties.

- **The web UI's Japanese is rewritten to one register.** The page was
  translated in a single pass and had been reading like two: prose in
  ですます, tooltips and menu items in 常体, and at least one tooltip in
  both at once. Register now follows the element — prose and toasts
  polite, buttons and tile labels 体言止め, a tooltip one polite sentence
  under 40 characters — and the labels that were relative clauses are
  nouns: a stat tile says 「結果報告 / 利用」 rather than 「使われた
  concept のうち結果が返ってきたもの」, and the queue is 「再検証」
  rather than 「間違いと報告された」. **`concept` no longer reaches the
  reader**: the page says 「ナレッジ」 for one, 「ナレッジベース」 for all
  of them, and 「ドキュメント」 for the stored OKF text, while `concept`
  stays where a reader actually meets it — ids, `put_concept`, and
  `api/openapi.yaml`. Untranslated metaphors are gone (a feed called
  itself 「カナリア」 twice), and **the dev banner had been saying the
  wrong thing**: 「認証が切れています」 describes a session that expired
  and can be renewed, where `OCHAKAI_MODE=dev` means authentication is
  off entirely. It says 「認証が無効になっています」 now.
- **The stray spaces inside the Japanese are gone.** A source line break
  between two Japanese characters renders as a space, and the page held
  61 of them — three in the first paragraph of the home page, reading as
  「ナレッジの id が そのままパスになる」. Nothing had looked, because
  the defect only exists once a browser lays the text out. Japanese
  paragraphs are one source line now, the provenance line stopped
  wrapping particles around an interpolated date (「2026年8月1日 に
  👤tanaka が作成」 → 「作成 2026年8月1日 👤tanaka」), and the check
  holds the rule.
- **A failure message names its operation.** 「失敗しました: 」 was one
  sentence shared by verifying, rejecting, withdrawing a ruling,
  changing status, deleting, and detaching a file; each says which it
  was, because a curator reading the first cannot tell what to retry.
- **The rules are written down, and checked.** CONTRIBUTING.md's *The
  web UI's Japanese* carries the register table and the terms that stay
  English, and `internal/webui/jstest/copy.test.js` reads that same list
  back. It pulls the copy out of `static/` — string literals only,
  including the ones inside a template's `${...}`, which is where the
  half-translated tooltips were hiding — and fails on English left in a
  reader's sentence, on a plural branch (`n === 1 ? 'draft' : 'drafts'`
  cannot survive translation into a language with no plural, so it marks
  copy nobody touched), and on a half-width `?` after kana. Nothing in
  this repository had ever read the page's copy: the previous
  translation shipped 40-50 English strings behind a changelog line
  saying every one of them was done.

- **A deployment that verifies tokens itself says when it is recording a
  person as a process.** `OCHAKAI_OIDC_ISSUER` deployments record a
  caller by `sub`, as a process, when the token carries no verified
  email
  ([0086](docs/design/0086-a-second-way-to-say-who-is-calling.md) §4) —
  written for machine tokens, which legitimately look like that. A
  person's token can arrive the same way: issuers commonly put `email`
  in the ID token rather than the access token, so somebody who
  completed a browser authorization writes concepts attributed to a
  process and **nothing fails**. The server now logs one `WARN` naming
  the issuer and the subject, the first time it happens in a process —
  once, because a machine-only deployment is correct and an alarm on
  every request is one nobody reads
  ([0117](docs/design/0117-a-person-recorded-as-a-process-says-so.md)).
  Nothing else moves: the recorded name, the trust tier and every
  response are unchanged, and the Cloud Run path already refused such a
  token outright. If the warning appears and your callers are people,
  map `email` into the access token at the issuer.

### Fixed

- **The request log says who called on `/mcp`.** The line carries one
  fact about the caller — whether it was a person or a process, which is
  what an operator reads a rate by — and on `/mcp` it read `process` for
  everybody, a person included. The cause is the wiring rather than the
  log: `restapi.RequestLog` was wrapped *around* `httpauth.Middleware`,
  so it read the request as it arrived, before the actor was resolved
  into a derived context, and `httpauth.Actor` handed it its
  `process/unknown` default. `/api/v1` was never affected — its log sits
  inside the middleware, because `restapi.Handler` builds its own mux
  around it — and **nothing stored was ever wrong**: provenance on
  concepts, revisions and rulings is resolved separately and named the
  right actor throughout. `/mcp` now nests the same way, which also
  means a request the middleware refuses (a 401 with no usable token) no
  longer produces a line there, exactly as it has never produced one
  under `/api/v1`.

- **`put_concept` no longer says a ruling exists outside the caller's
  scope.** On a deployment with an access policy (design doc 0109), the
  two guards MCP's `put_concept` runs before it writes — the ones that
  refuse to replace or revive knowledge a human has ruled on — read the
  ledger without the scope check the write itself has. Against an id
  outside the caller's grant they answered "cannot replace `<id>`: it is
  rejected" instead of the 404 that hides the address, so an agent could
  learn that a concept exists there, or existed, and that somebody
  verified, rejected or deprecated it. The concept's content was never
  reachable and no write ever landed; what leaked was the address and
  the ruling. Both are scoped now, and §6's exhaustiveness walk covers
  them, which is why they were the two it did not. A deployment with no
  access rules — every deployment before 0109 and every one that has not
  written a policy since — was never affected.
- **`ochakai get --download` checks the filenames it is handed.** A
  server names the files it hands back, and the client wrote them under
  the download directory without asking whether the name was one path
  segment. Any ochakai holds every filename to that rule on the way in,
  so this could only fire for an answer that did not come from one — but
  believing such an answer would have put bytes wherever the name
  pointed. The name is checked now and a refusal names it.
- **A Japanese question stops asking for the particles it is made of.**
  `queryFragments` kept a run of one or two characters whole without
  asking whether it held a content character, and a one-character run
  was asked for as a prefix — so a question written with spaces or latin
  words in it (`AI が verify を打ってよいか`) sent `が:*` to the index,
  which matches nearly every Japanese document there is. `contentWindows`
  had dropped hiragana-only windows for exactly this reason since 0089
  §4; the branch above it never asked. A run that *is* a word keeps its
  fragment wherever the content character sits in it (与信, お茶), and a
  query with nothing else to look for still asks for its grammar
  (ください, か) rather than for nothing.
- **A katakana word is asked for whole instead of cut into windows.** A
  run of Han and hiragana is a sentence nobody here can find the word
  boundaries in, which is why it is cut into two-character windows — but
  **a stretch of katakana is not a sentence**, and the script change on
  either side of it is a word boundary somebody wrote. A question about
  a スクリーンショット was spending most of its weight on セッ, ット and
  ショ — fragments belonging to データセット and セッション — and reaching
  most of the base. A katakana run of five characters or more now leaves
  the query whole and asks the index for all of its windows at once
  rather than any of them. The stored side is untouched, so a word
  inside a longer one still answers (`セッション` finds リモートセッション)
  and nothing has to be rewritten.
- **Neither of the two above moves a ranking**: over the golden set
  every case landed on the rank it already had, and what changed is what
  stopped arriving underneath it — the candidate set is a seventh
  smaller and a third less of it comes from the domain the question was
  not about. Both are query-side, so no migration and no re-embedding.
- **Halfwidth katakana and fullwidth latin are found by a question
  spelled normally, and the reverse.** They are what a system upstream
  of the reader writes — a warehouse export from something fixed-width,
  an IME that yields ＢｉｇＱｕｅｒｙ mid-sentence, a CSV that came
  through a mainframe — and each spelling used to be its own island:
  「データセット」 found only the normally-written document, 「ﾃﾞｰﾀｾｯﾄ」
  only the halfwidth one, and neither ever found the other. That was
  recall, not ranking: the document could not be found at all. Both
  sides now fold with NFKC, the compatibility normalization the standard
  defines for this, and the two implementations are pinned against each
  other over 7,377 code points. **Migration 0043 recomputes the search
  index for every stored object**, so the first start after upgrading
  writes once per row before it serves.

## [0.26.2] - 2026-08-21

### Fixed

- **A claim keeps the timestamps the document wrote.** A received
  document's trust family is kept as its own claim under `received:`
  ([0075](docs/design/0075-the-bundle-is-the-address-space.md) §3.1) —
  but YAML types an unquoted date-time, and rendering that back had to
  pick a spelling again: `at: 2026-07-01T00:00:00Z` was stored as the
  bare date `2026-07-01`, and `at: 2026-07-01 09:30:00` as
  `2026-07-01T09:30:00Z`. A claim now carries the text as written, which
  is what a claim is for. Only documents from another producer were
  affected — this instance's own export form quotes its timestamps, so
  the export → review → import loop is unchanged — and a claim already
  stored keeps the spelling it landed with, because the bytes it arrived
  in are gone; re-importing the bundle it came from restores it.

- **A katakana word with a long vowel in it is one search term.**
  ー belongs to no script — Unicode calls it Common, because it is
  written after hiragana as readily as after katakana — so both halves
  of the lexical search treated it as a word boundary. データ was two
  runs of one character, asked for as the prefixes `デ:*` and `タ:*`,
  and that is most of the vocabulary a data team writes in: テーブル,
  ロード, パーティション, カレンダー, レポート, ユーザー. Measured over
  the demo bundle as it then stood, a katakana question touched 11.00 of
  the 12 concepts in the base — the right one first, and the rest
  trailing it on a shared first character; it now touches 2.60 (over the
  rewritten bundle below, 8.60 of 19). The halfwidth voiced
  marks (ﾞ ﾟ) and halfwidth ｰ join for the same reason. **Migration 0042
  recomputes the search index for every stored object**, so the first
  start after upgrading writes once per row before it serves; a base of
  a few thousand concepts takes seconds, and nothing else is affected.

- **An honorific prefix no longer takes the word behind it out of the
  query.** Japanese is cut into two-character windows, and a window was
  kept only when it began with a content character — grammar is written
  in hiragana, and that rule holds right up until the content word
  itself begins with one. お盆 and ご要望 are that shape: the window
  spelling the word was dropped from every query, leaving only the next
  window along (`盆の`, `望の`), which carries whichever particle the
  document happened to use. `ochakai search "お盆"` found the concept
  and `ochakai search "お盆は"` found nothing — one particle, and the
  question was gone. A window beginning お or ご now survives when a
  content character follows it, and both halves of that test carry
  weight: `おく` (〜しておく) has to stay grammar. Measured lexical-only
  over 22 Japanese questions against a 20-concept base, top-3 recall
  goes 21/22 to 22/22 and MRR 0.77 to 0.81, and no other question's
  rank moved. **This is the query side only** — the stored index has
  always kept every window — so nothing is reindexed for it. The cost,
  named: a politeness formula (`ご覧のとおり`) is now a fragment too,
  and scoring weights rare fragments highest, so a base holding a lot
  of polite prose is where to watch the ranking.

### Changed

- **The demo bundle is now an ontology over a real dataset.** The
  eighteen concepts under `examples/demo` (was eleven) describe Google's
  public `bigquery-public-data.thelook_ecommerce` dataset instead of an
  invented shop, so every `# Computation` fence actually runs. The
  bundle now covers the kinetic half of a Palantir-style ontology too:
  an action concept with parameters, validation bounds and a
  decision-draft write-back, its executor skill, and a
  `glossary/ontology` concept mapping Foundry's core concepts onto what
  ochakai and an executing agent hold. Anything written against the old
  invented-shop ids (`tables/shop-orders`,
  `queries/sales/revenue-by-channel`,
  `references/order-channel-codes`, `insights/着地見込み`) needs the new
  bundle; the load-bearing ids (`metrics/revenue`,
  `glossary/completed-order`, `queries/sales/monthly-revenue`,
  `insights/reading-revenue`, `policies/revenue-recognition`,
  `skills/run-bigquery-query`, `metrics/repeat-purchase-rate`) are
  unchanged.

  **Its Japanese was rewritten before release.** The first draft leaned
  on the dash, the bold and the aphoristic closing line often enough
  that the prose read as generated rather than written; every concept
  now says the same things in plainer sentences. No id, no
  frontmatter key, no SQL and no threshold moved, and the search
  evaluation over the bundle is unchanged where it matters (lexical
  recall 1.00 / MRR 0.87 and reach 10.54 both before and after; fused
  MRR 0.86 to 0.87, so `evalFusedMRRFloor` rises to 0.85). An
  instance that already imported the bundle sees the bodies replaced
  and the old ones kept as revisions. The same pass rewrote
  [最初のひと月](docs/guides/onboarding.md) and the demo half of
  [examples/README.md](examples/README.md), and the two sentences
  README.md quotes verbatim from `insights/reading-revenue` were
  re-quoted to match.

  **Two things the bundle said about itself were wrong, and running it
  said so.** The action's `min_sold` accepted 5 to 50 while arguing that
  the denominators here are small: measured against the dataset, a SKU
  has at most 5 resolved items in a 90-day window and 12 over the whole
  history, and no candidate meeting the threshold had more than 9, so
  anything above 10 returns an empty list at every window. The bound is
  5 to 10 now, with those numbers as its reason. README.md also promised
  that `ochakai search traffic_source` lands on the concept listing the
  column's values; it lands on the computation that splits revenue by
  it, with the table concept below — the sentence now says what comes
  back.

## [0.26.1] - 2026-08-19

### Added

- **The MCP bundle now carries an icon.** A desktop app draws a tile for
  an installed bundle before it has ever started the server, and
  `ochakai_X.Y.Z.mcpb` gave it nothing to draw — the one thing a reader
  sees first was a blank square in a list where the other entries have a
  mark. The bundle now ships `icon.png`, the tea bowl from
  [docs/images/logo-mark.svg](docs/images/logo-mark.svg) at 512x512, and
  the manifest names it. Nothing about the wire, the binary or the
  `mcp-stdio` bridge changed; a bundle from an earlier release keeps
  working and simply stays blank.

### Fixed

- **`sandbox = true` could not be applied at all.** The Terraform module
  widened the `allUsers` grant to cover the disposable sandbox but left
  its plan-time precondition demanding `OCHAKAI_MODE=public`, so a
  sandbox produced a plan that asked for the binding and then refused
  itself. The precondition now names both postures that justify a public
  binding — `public` and `sandbox` — and still refuses every other one.
  Nothing changes for a deployment that was already applying.
- **`terraform output` called a sandbox private.** `posture` fell through
  to "private read-write" and `demo_url` returned null for a service that
  `allUsers` can reach, so the output that exists to answer "is this one
  public?" answered it wrongly for the one posture that is public *and*
  writable. Both now follow the `allUsers` grant rather than
  `var.public_read_only` alone.
- **The recommended deployment pinned a nine-release-old image.**
  `deploy/terraform/terraform.tfvars.example` is copied to
  `terraform.tfvars` and applied as written, and its `image_tag` had
  stayed at 0.17.0. It tracks the current release again, and
  `TestReleaseVersionsAgree` now reads it — and the deployment guide's
  hand-set `export VERSION=` — back against the changelog.

## [0.26.0] - 2026-08-18

### Changed

- **The write-back habit now says to link a draft to what it came from.**
  A relationship between concepts is a markdown link in the body and
  nothing else, so a draft written without one is reachable by search and
  by nothing else — it never appears under `linked_from` when somebody
  reads the concept it is about. Both automated write-back paths said to
  write a draft and neither said to link it: `get_concept`'s hint on the
  MCP surface, and the Stop hook in `examples/claude-code`. Both do now.
  Nothing about the wire changed — the hint is per-response prose, so the
  MCP resident-byte budget is untouched at 11,788 — and the CLI's
  `CLAUDE.md` already taught it.

- **The read half of that loop now says where an answer came from.**
  An agent that searches ochakai, reads a definition and answers a
  person had no citation convention: nothing told it to name the
  concept it used, or whether a human had confirmed that concept. A
  verified definition and a guess assembled on the spot then arrive
  wearing the same face, and the reader who spots a wrong number has
  no id to open, to re-verify, or to report failed against.
  `examples/claude-code/CLAUDE.md` now asks for both, and the recall
  hook carries one sentence of it, because an instruction only works
  where the agent still remembers it. Two claims about the CLI are
  corrected in the same pass: the search line advertised a score
  column, which design doc 0110 dropped, and both layers said to judge
  a draft by `created_by`, which is not on a search hit at all — it is
  on the full read. **What to do about it**: nothing, unless you
  copied `examples/claude-code` into your own repository, where the
  paragraph is worth copying again.

- **BREAKING (deployment): a variable ochakai does not read stops the
  start** (design doc 0112). `ochakai serve` and `ochakai serve-ui` now
  refuse to start when the environment carries a variable beginning
  `OCHAKAI_` that ochakai does not read, naming every one of them and
  listing what it does read. **What to do about it**: if a deployment
  sets an `OCHAKAI_*` variable that is not in
  [docs/configuration.md](docs/configuration.md)'s table — a typo, or an
  old spelling a fold retired, such as the `OCHAKAI_VERTEX_*` and
  `OCHAKAI_EMBEDDING_DIM` variables `OCHAKAI_EMBEDDINGS` replaced in
  `0.18.0` or the `OCHAKAI_READ_ONLY` /
  `OCHAKAI_PUBLIC_READ_ONLY` / `OCHAKAI_INSECURE_DEV` booleans
  `OCHAKAI_MODE` replaced in `0.17.0` — remove it before upgrading, or
  read its name off the failed revision's startup log. On Cloud Run a
  revision that does not start takes no traffic, so the previous one
  goes on answering. Until now those variables were read by nobody and
  said so to nobody: `OCHAKAI_RECORD_MISSES` typed one letter short left
  the recording on, with nowhere to see that it had. The refusal is the
  one design doc 0064 §2 already makes for an unrecognized query
  parameter, on the other surface an operator spells by hand.
  `OCHAKAI_TEST_*` is this repository's own harness namespace and is
  ignored, and so are the two variables ochakai ships a reader for that
  is not the server — `OCHAKAI_RECALL_LIMIT` in the recall hook of
  `examples/claude-code`, `OCHAKAI_ID_TOKEN` in the BigQuery catalog
  job: a shell that exported one of those and then starts a server is
  not a misconfigured deployment. A misspelling of either is still
  refused, which is why those two are a list and not a prefix. The
  client commands are not checked, since a wrong `OCHAKAI_URL`
  announces itself at once. No variable was added or removed.

### Added

- **A write says what it read differently, in the response body** (design
  doc 0113). The answer to a concept write now carries `notes` — one
  string per value the server accepted but read differently than the
  document wrote it (an unrecognized `status`, a date it could not parse,
  a trust family kept as the document's own claim). These are the same
  strings the repeated `Ochakai-Note` header has always carried, and
  **that header is unchanged**: its name is inside the freeze, so
  dropping it is 1.0's work, and it stays the only carrier on a
  `text/markdown` response, whose body is the document itself. Nothing
  else moved — a read still has no `notes`, the CLI still reads the
  header, and the MCP tools already returned the same strings in their
  result. **What this changes for you**: the web UI's save now says what
  the server made of the document you wrote, instead of leaving it in a
  header nothing read, and `curl | jq` sees it too.

- **A search that lost its embeddings says so** (design doc 0114). When
  the query cannot be embedded, the ranking degrades to the lexical list
  alone — it always has, rather than failing the search — and the answer
  now carries `degraded: true` instead of only writing a line to the
  server's log. A deployment with **no** embedder configured is not
  degraded and never sets it: there, lexical is the whole answer rather
  than a fallback from one. Each surface words it for its own reader:
  `ochakai search` prints a `note:` line on **stderr** (stdout stays one
  hit per line, so pipes are unchanged), the web UI says it above the
  results, and MCP's `search_concepts` description names the word.
  **What this changes for you**: a search that comes back thin during an
  embedding outage now says that it is the deployment and not your
  question — which matters most in Japanese, where a trigram index alone
  cannot retrieve a two-character word. MCP's answer to `search_concepts`
  no longer carries `cursor` (a search has never paged), and
  `list_concepts` does not carry `degraded`.

### Fixed

- **The day-one catalog could arrive with 100 rows of it and no
  complaint.** Every documented `bq query` that feeds `ochakai seed` or
  the query-history drafter now passes `--max_rows`: `bq` prints the
  first 100 rows and says nothing about the rest, so a dataset whose
  columns add up past that seeded only the tables those rows reached —
  no error, no warning, and a bundle that looks exactly like a small
  warehouse. `ochakai seed` now prints a `note:` on **stderr** when its
  input is exactly 100 rows, naming the flag; it stays a note rather
  than a refusal, because 100 rows is also just 100 rows. **What to do
  about it**: if a catalog was seeded before this, rerun the query with
  the limit raised and `ochakai import --dry-run` the result first — the
  tables that were missing come back as *created*, and anything counted
  as *updated* is an entry the projection would replace with a fresh
  skeleton (the version somebody wrote is kept as a revision, but it is
  no longer what the entry says). Where drafts have been filled in
  already, `ochakai seed cols.json > catalog.tar.gz` and import the
  missing tables out of it instead. Reported by a user.
- **The MCP bundle could not start the server it carries.** The
  `.mcpb` manifest named its command `server/ochakai` — a path relative
  to whatever directory the desktop app happens to run from, not to the
  unpacked bundle — so the bundle installed, appeared in the app's
  settings, and died at the first message with
  `Failed to spawn process: No such file or directory`. The command is
  now `${__dirname}/server/ochakai`, the token the format defines for
  the bundle's own directory. **What to do about it**: reinstall the
  bundle from a release carrying this fix; nothing else about it
  changes, and the configured server URL is kept. Every released
  `.mcpb` up to and including `0.25.0` is affected — the manual's
  hand-written Claude Desktop JSON
  ([docs/guides/mcp-clients.md](docs/guides/mcp-clients.md)) was never
  affected, since it already tells the reader to use an absolute path.
- **The compatibility policy said a new error `code` could be added,
  and it cannot.** [docs/compatibility.md](docs/compatibility.md) now
  says what the checks have enforced since the freeze: the twelve-word
  `code` vocabulary (design doc 0083) is written into the `Error`
  response the frozen core operations reach, so a thirteenth word moves
  a line of [api/openapi.frozen.txt](api/openapi.frozen.txt) — a change
  to the core rather than one of the two additions design docs 0082 and
  0101 place outside the freeze. Nothing in the product changes; a
  client that already treats an unrecognized code as its status alone
  keeps doing exactly the right thing.
- **On a phone, opening an entry from the tree looked like it did
  nothing.** Where the layout is one column, the knowledge tree is a
  disclosure that sits above the view in the document, so an entry
  opened with the tree left open put the page it asked for a screenful
  below the tap that asked for it. It now closes on navigation, the way
  a drawer does, and the entry starts under the top bar. In the same
  pass the search row wraps instead of sharing one line: the query
  field and a feed's banner were down to half a phone's width beside
  the create button, which left the banner reading a word to the line.
  Wide screens are unchanged.

## [0.25.0] - 2026-08-17

### Added

- **The CLI's human-facing lines carry bold and dim at a terminal**
  (design doc 0111). `ochakai search`, `ochakai list` and
  `ochakai browse` now print the address in bold — the URI, and
  `browse`'s directory names, segments and file names — and dim the
  prose that trails the row, `— description` and the `· snippet` that
  says why a hit matched. Everything between stays plain: `status`,
  `type` and a feed's first column are values you compare down the
  column. **Nothing changes for anything but a terminal.** The escapes
  are written only when stdout is a character device, so
  `ochakai search q | cut -f1`, `> hits.txt` and `--json` receive the
  same bytes as before, and stripping the escapes from a styled line
  gives back today's line exactly. Set `NO_COLOR` to any non-empty
  value to turn it off at a terminal too; there is no `--color` flag.
  A terminal that does not render dim shows the plain line.

### Changed

- **BREAKING (CLI): `ochakai search` no longer prints a score column**
  (design doc 0110). A hit is now `uri, status, title — description`,
  and the order of the lines is the ranking. The number that used to
  lead each line was on a different scale depending on the deployment:
  a base with no embeddings scores a hit on weighted fragment coverage,
  roughly 0 to 3, while a hybrid one scores it by reciprocal rank
  fusion, which `rrfK = 60` caps near 0.049 — so the same query printed
  `1.730` against one deployment and `0.033` against another, with
  nothing on the line saying which, and a deployment whose query
  embedding fails degrades to lexical with only a server-side warning.
  0068 §3 removed `min_score` from the wire for exactly this reason and
  the printed column outlived it. `ochakai list --source` and
  `--links-to` lose the column too, where it was always `0.000`: a
  reverse lookup orders by address. **The feeds keep theirs** —
  `ochakai list usage|verified_at|failed|stale_after` still lead with
  the date or count they ordered by, so those pipelines are unaffected.
  **What breaks**: `ochakai search q | cut -f2` for the id becomes
  `cut -f1`; `cut -f1` for the score has no replacement on the human
  output. `--json` and the REST `score` field are unchanged, so a
  caller that has calibrated one still reads it with
  `--json | jq '.hits[].score'`.

## [0.24.1] - 2026-08-17

### Added

- **A directory can have readers and writers** (design doc 0109).
  An access policy of grants: one row says a principal —
  `human:<name>`, `process:<name>`, or `*` for every authenticated
  caller — may read under one directory prefix, and write there when
  `may_write` is true. Prefixes match on segment boundaries, so `sales`
  covers `sales/orders` and does not cover `sales-legacy/orders`. There
  are no deny rules and grants only add up, so a rule on `sales/sample`
  cannot take back what a rule on `sales` gave. **A deployment with no
  rules behaves exactly as it did before**; writing the first rule turns
  the boundary on for everybody at once. Set `OCHAKAI_ADMINS` first —
  administrators come from the deployment's configuration rather than
  from the policy, and a deployment holding rules that names no
  administrator refuses to start. The operations that take the bundle as
  a whole (`stats`, export, `move`, `reembed`, and the policy itself)
  are an administrator's. A read outside a caller's scope answers 404
  rather than 403, and one gap is stated rather than closed: a readable
  concept whose prose names a hidden id leaks that id.
  [docs/guides/operating.md](docs/guides/operating.md#access-boundaries)
  has the procedure.
- **The policy is on three surfaces**: `GET|PUT /api/v1/access`,
  `ochakai access` (show, and `-f` to replace), and the web UI's
  アクセス tab at `#/access` — the table, and the same whole document
  replaced in one go. The tab appears only for a caller the server let
  read the policy. MCP does not carry it: a tool schema is paid for out
  of an agent's context window, and since enforcement sits in the
  service layer, what an agent can reach narrows on its own. The web
  UI's editor says who a save is recorded as, because `serve-ui`
  without IAP in front folds every caller into one service account —
  where that account is an administrator, everyone who can reach the
  URL can edit the boundary.

### Changed

- **The demo knowledge base is written in Japanese.** ochak.ai's demo is
  a Japanese demo, and [demo.ochak.ai](https://demo.ochak.ai) serves
  `examples/demo`, so the bundle a reader meets first is now written the
  way the team it is pretending to be would write it: titles,
  descriptions, bodies, `question` fields and `status_note` in Japanese,
  and the names that come out of the warehouse — `total_price`,
  `status = 'completed'`, the `channel_code` values, the SQL in the
  `# Computation` fence, the type vocabulary and the tag slugs — left in
  English, because that is where they came from. It was bilingual by
  halves before, with English definitions and Japanese naming notes; the
  halves are now drawn where a real base draws them, between the
  warehouse and the judgment. The eleven concepts, their ids, their links
  and the deliberate unevenness (two drafts, one deprecated reference,
  one draft already past its `stale_after`) are unchanged, so anything
  addressing them by id still works, and `insights/着地見込み` keeps the
  id it always had. `examples/golden-query.md` — the single document the
  quick start registers with `ochakai put -f`, about the same invented
  shop — went with it.
- **`metrics/revenue` no longer claims "net sales" as a synonym.** The
  concept spends a paragraph saying 純売上 — net sales — is a different,
  ex-tax number that will not reconcile with it, and then listed it as
  another name for itself. The synonyms are now `トップライン`,
  `top line` and `revenue`, which are names for this metric, and the
  search-evaluation harness keeps a case on the one of them that appears
  nowhere else in the bundle (design doc 0105).
- **The web UI screenshots were re-shot** against the Japanese bundle,
  and are Japanese-dated now that the page around them is (CONTRIBUTING,
  *Web UI screenshots*, which carries the new heights and the locale to
  force).
- **The search-evaluation golden set is asked in Japanese**, against the
  shipped bundle, and the two inline fixtures that had come to duplicate
  it (売上の読み方, 受注) are gone; 解約率 stays, because the invented
  shop sells no subscriptions. Four English cases stay for the words the
  warehouse supplies. Recall reaches 1.00 on both halves — nothing falls
  out of the top ten — while MRR reads 0.78 lexical and 0.77 fused,
  against 0.90 and 0.85 for the bilingual corpus and a different set of
  questions. The floors moved with them. Both effects behind the drop
  are the corpus rather than the ranking: a monolingual Japanese base
  about one shop shares 売上 across every concept, so the concept *named*
  売上 takes the name rule for 「なぜ売上が落ちているのか」 and the
  insight that answers it lands second; and an English keyword now sits
  in Japanese prose, so `total_price` ties seven concepts and is settled
  by the tie-breaks.

## [0.24.0] - 2026-08-17

### Added

- **A read carries what points at it** ([design doc
  0106](docs/design/0106-a-read-carries-what-points-at-it.md)). A
  single-concept read — the REST JSON, MCP `get_concept`, `ochakai get`
  — now carries `linked_from`: rows (id, type, title, description,
  status, trust) for the concepts whose bodies link at the one being
  read, in address order, capped at 20, rejected linkers excluded.
  Links are derived from bodies, so the forward direction was always
  visible in the document itself; the reverse lived in other people's
  documents, and a reader of a metric could not see the insight
  explaining it. The complete, pageable reverse lookup stays on
  `?links_to=`. Rows are pointers, not delivery — nothing is recorded
  as fetched for being named — and the addition is response-only,
  outside the freeze (design doc 0082). `ochakai get` prints the rows
  on stderr beside the file hints; the MCP schema does not move a byte.

### Changed

- **BREAKING: the REST freeze narrows to the OKF core** ([design doc
  0107](docs/design/0107-the-freeze-holds-the-okf-core.md)). What stays
  frozen is the bundle round trip — `GET`/`PUT`/`DELETE
  /api/v1/bundle/{path}` — and `GET /api/v1/search`; the ruling, usage,
  stats, move and reembed operations return to the 0.x-unstable regime
  MCP and the CLI were already under, where a breaking change is marked
  BREAKING here and carries a design record. **Not a byte of the wire
  moves in this entry** — what changes is the promise: since 0.18.0 this
  page and [docs/compatibility.md](docs/compatibility.md) said all of
  `/api/v1` was frozen, and that scope is retracted, not reinterpreted.
  The record says why the boundary was wrong (fourteen days of
  corrections, all pointing at OKF) and why it cannot move again: the
  core can only ever widen. If you built against a non-core endpoint,
  nothing breaks today; from here on, watch the BREAKING entries the way
  you already do for the CLI.

### Removed

- **BREAKING: the context pack is retired from every surface** ([design
  doc 0108](docs/design/0108-the-context-pack-retires.md)). `GET
  /api/v1/context`, the MCP tool `get_context` and `ochakai context`
  are gone, and with them the `budget` parameter and flag. The pack was
  built for a world where round trips were expensive for agents: it
  made the server decide what should be read — greedy packing, outline,
  excerpt, a two-direction link hop — and an agent that iterates
  search → get on its own was having that judgment taken away from it.
  What the pack bundled survives in primitives: the reverse hop arrives
  as `linked_from` on the concept itself (design doc 0106), and the
  write-back hint now rides on `get_concept`'s answer. What a client
  does about it: call `GET /api/v1/search` and read the concepts worth
  reading at `GET /api/v1/bundle/{id}.md` (MCP: `search_concepts` then
  `get_concept`; CLI: `ochakai search` then `ochakai get`). The bundled
  recall hook now injects search pointers instead of a pack, and
  `OCHAKAI_RECALL_BUDGET` became `OCHAKAI_RECALL_LIMIT` (rows, default
  8). This is a removal, not a rename, so no one-release window
  applies; the REST removal rides on design doc 0107 having moved
  `/context` outside the frozen core, so `api/openapi.frozen.txt` does
  not move. MCP resident bytes drop 13,460 → 11,629 and the MCP-BYTES
  ceiling goes down to 12,000.

## [0.23.0] - 2026-08-17

### Added

- **A concept is found by the other names its writer gave it** ([design
  doc 0105](docs/design/0105-a-concept-answers-to-its-other-names.md)).
  The lexical haystack reads `synonyms` from a concept's frontmatter, so
  a metric written as

  ```yaml
  synonyms: [net sales, top line, 売上]
  ```

  answers to all three. It did not before: the type vocabulary describes
  a `Metric` as holding "its definition and synonyms", `examples/demo`
  writes them that way, and the haystack was id, title, description,
  tags, body and the filenames in the concept's namespace — a producer
  key sits in `attrs`, which nothing read. Sharpest in Japanese, where
  the warehouse column is `revenue` and the word in the room is `売上`.

  Either a list or a bare string is read. **Only `synonyms`**: every
  other producer key is a value *about* the concept, and indexing those
  would make a search for one concept return the concepts that merely
  mention it — the question `--links-to` and `--source` answer properly.
  The key stays the producer's: no validation, no wire field, no filter
  of its own, and it round-trips as written.

  What an operator does about it: nothing. Migration `0040` recomputes
  the stored haystacks once, and the search evaluation harness measures
  the result (lexical MRR 0.90 → 0.91 over 42 cases, recall unchanged
  at 1.00).

- **A directory listing pages.** `GET /api/v1/bundle/<dir>/index.md` read
  as JSON now carries a `cursor` when rows remain, and takes it back as
  `?cursor=` to read the next page — `ochakai browse --cursor` on the CLI,
  which prints the next one to stderr the way `ochakai list` does. A level
  wider than 1000 rows used to end at `truncated: true` with no way past
  it ([design doc 0101](docs/design/0101-a-level-can-be-walked.md);
  [0068](docs/design/0068-how-a-face-is-added-and-removed.md) §2.1 had
  already decided a listing must not do that). **Nothing changes for a
  caller that fits under the cap**: the page is still 1000 rows of each
  kind, and `truncated` still says what it always said. The markdown
  `index.md` does not page — OKF SPEC §8 has no spelling for "continued" —
  so `?cursor=` there is a 400, as it is on every other mode of the
  address.

  This is the first **optional query parameter** added since the freeze,
  which 0101 §5 records as something the freeze was never holding still:
  0064 §2 made an unrecognized query key a 400 precisely so that one could
  be added safely afterwards, and 0082 had written down only the response
  half of that. `api/openapi.frozen.txt` grows by two lines.

### Changed

- **BREAKING: `?history` answers JSON, whatever the `Accept` says**
  ([design doc 0102](docs/design/0102-one-history-in-one-spelling.md)).
  The markdown rendering of one object's history is gone. OKF SPEC §9
  defines one markdown history — the reserved `log.md` beside the
  objects — and this address rendered a second one from the same ledger
  rows, which is what left a single `?history&limit=500` on a concept
  answering 400 as JSON and 200 as markdown (0064 §14.1 wrote that split
  down rather than closing it). A concept's `?history` is now 50 rows by
  default and 200 at most on every read.

  What a client does about it: if you fetched `?history` without an
  explicit `Accept: application/json`, you now receive JSON instead of a
  markdown document — the same rows, in the representation the other
  three surfaces already read. `log.md` is unchanged, and is still the
  markdown history of a directory. Reading **one object's** history as
  markdown is no longer available; nothing in ochakai used it (`ochakai
  revisions` and the web UI's history panel both read JSON).

  This is the third reason the freeze may be broken, added by 0102 §3:
  folding away a second spelling of something OKF already defines,
  admitted only when the spec defines the spelling, ochakai carries a
  second one, and the one folded is ochakai's. `api/openapi.frozen.txt`
  does not move — the `text/markdown` line is shared with the concept
  export — so this entry and the record are where the change is written
  down.

- **MCP hands back one copy of a tool's answer, and declares no output
  schema** ([design doc
  0103](docs/design/0103-the-tool-result-travels-once.md)). Every tool
  result used to arrive twice — the same JSON in `content[0].text` and in
  `structuredContent`, 10,384 and 10,439 bytes for one `get_context`, in
  a 22,009-byte response — and the seven output schemas the tool list
  carried came to 14,986 bytes, more than everything the MCP-BYTES
  budget was counting. Both were go-sdk defaults following from the
  handlers' return types rather than anything written by hand. A client
  that read `structuredContent` now reads the text block instead; the
  JSON is identical, and the budget counts both schemas from here on,
  which does not move its number because the output side is empty.
- **A rejection's reason travels with the ruling** ([design doc
  0104](docs/design/0104-a-ruling-travels-with-its-reason.md)).
  `ochakai get` prints who rejected a concept, when, and the `--note`
  they left — on stderr, beside the provenance line, so stdout stays the
  document. `ochakai search --rejected` (and `list`) says on stderr that
  the rows were turned down and where the reason is. An exported bundle
  carries `rejected_note` beside `rejected_by` / `rejected_at`; like
  those two it is never read back on import, so nothing about a bundle's
  meaning changes — the reason simply stops being the one part of a
  ruling that could not leave.
- **`ochakai import` prints a repeated note once.** A bundle written by
  another instance produces the same sentence for every concept in it —
  ten identical lines for `examples/demo`, one per table for a seeded
  catalog. Notes are now grouped by text, with the concepts each one was
  true of listed under it (up to five, then a count). The `N notes`
  summary and what `--strict` fails on are unchanged.
- **`ochakai context` and `ochakai search` say when nothing matched.**
  They printed nothing at all and exited 0, which reads the same as a
  server that did not answer. The line goes to stderr, so a pipeline is
  unaffected, and `context` names the write-back that would fix it.
- **An unknown command answers with a suggestion instead of the whole
  command list.** `ochakai serach` now offers `search`; `ochakai --url X
  whoami` says that a flag goes after the command rather than printing
  forty lines that do not mention the problem.
- **The demo knowledge base is bilingual, and gained an eleventh
  concept.** `examples/demo` was entirely English, so a Japanese reader
  following the quick start — including `examples/README.md`'s own
  `ochakai context "なぜ売上が落ちているのか"` — got silence, and the
  Japanese-search claim could not be tried without writing your own
  concepts first. The metric now records what the number is called
  (`売上`, and the two things it is not), the glossary carries the
  order states' Japanese names, the seasonality table names お盆 and
  年末商戦, and `insights/着地見込み` exists only in Japanese. The search
  evaluation harness measures the shipped bundle for it, in place of the
  inline Japanese fixture that stood in for it.

- **BREAKING: a `.md` path holds a concept and nothing else**
  ([design doc 0100](docs/design/0100-md-is-how-a-concept-is-spelled.md)).
  A markdown document with no `type` used to be stored as a *file* at its
  own `.md` path; every export carrying one handed out a bundle that
  fails OKF SPEC §11.1 and §11.2, which make parseable frontmatter with a
  non-empty `type` the condition of conformance for every non-reserved
  `.md` in the tree. **This breaks the REST freeze**, under the second
  reason [0064](docs/design/0064-rest-stops-at-api-v1.md) §11 now carries:
  output that does not conform to OKF. `api/openapi.frozen.txt` does not
  move — OpenAPI cannot spell a `requestBody` that differs per address —
  so this entry is the only place the change is written down.

  What an operator does about it: **migration `0039` renames stored
  markdown files off concept addresses** (`notes/meeting.md` →
  `notes/meeting.markdown`, taking the suffix twice if that name is
  occupied), moving each file's history rows with it so `?history` keeps
  answering. The bytes are untouched. Markdown links in concept bodies
  that pointed at an old spelling break, which is the one break OKF
  requires a consumer to tolerate (SPEC §6.1); the migration logs nothing
  per row, so `ochakai export` before and after is the way to see what
  moved.

  What a client does about it: a `PUT /api/v1/bundle/<path>.md` whose
  body carries no `type` is now `400 invalid` instead of `201`, and the
  sentence names the other way out — store those bytes at a path that
  does not end in `.md`. A `GET` or `DELETE` at a `.md` path with no
  concept at it is `404` rather than the file half's answer (which, on an
  instance with no bucket, was a `501`). `ochakai import` renames a
  typeless `.md` on the way in and says so in a note; `--strict` reads
  that note as the refusal it is.

### Security

- **Built with Go 1.26.6.** The 1.26.5 standard library carries seven
  vulnerabilities this code reaches — quadratic `resolvePath` in
  `net/url`, JavaScript regexp context tracking in `html/template`,
  unbounded post-handshake messages in `crypto/tls`, a missing
  `ReadHeaderTimeout` on the unencrypted HTTP/2 check in `net/http`,
  recursion depth in `encoding/xml` and `encoding/asn1`, and
  ASCII-only Punycode labels via `golang.org/x/net/idna` — all fixed
  upstream in 1.26.6. The `toolchain` line and the build image move
  together; the `go` directive stays `1.26`, so no minimum rises for
  anyone installing with `GOTOOLCHAIN=local`.

## [0.22.1] - 2026-08-14

### Added

- **A guide for the first month with an empty base**
  ([docs/guides/onboarding.md](docs/guides/onboarding.md), Japanese).
  Every step of a cold start already shipped — `ochakai seed`, the
  day-one catalog procedure, the loop's four prompts, the golden-query
  canary — but nothing said in what order to do them, how much of each
  kind to write, or what to do between them. The guide is that spine:
  scope it to two datasets, write down the ten to twenty questions your
  team actually asks *before* seeding anything, project the skeleton,
  then fill it in by type in an order where each kind can cite the one
  above it, and stop at roughly twenty to thirty verified concepts
  rather than planning to rule on everything.
  Its §3 is the step no knowledge-base manual writes down: **fire those
  questions back at `ochakai context` and check that what you wrote is
  what comes out.** Nothing returned is already counted — `ochakai
  stats`'s gap lines rank the questions that came back empty by how
  often they were asked, which is the list of what to write next; the
  wrong thing returned is a vocabulary problem in the concept body. The
  questions that miss stay in the file and get fired again.
  `docs/loop.md`'s cold-start section shrank to a pointer, so the two
  pages no longer hold half of one story each. DOC 26 to 27, DOC-LINES
  6,500 to 7,000.

### Fixed

- **The web UI's Japanese reads as Japanese.** The home page's opening
  paragraph, the three feed banners and the review page's prose had
  sentences whose halves did not agree — a clause ending in `に` where a
  verb belonged, an id called "そのパス" with nothing for その to point
  at — and the home page's shortcut row broke wherever the window
  happened to end, leaving the separator at the head of a line and a
  "link — what it is" pair split across two. That row is now a list
  whose entries are flex items, so a break falls between entries rather
  than inside one. Two of the sentences were also untrue: the draft
  queue ranks on fetches and not on search hits (design doc 0090), and
  verifying a concept drops it from 間違いと報告された but only re-sorts
  it in 検証の古さ, which lists the whole base and never empties.
- **A sandbox now says so to agents too, not only to people.** The CLI
  and the web UI announce the disposable posture; MCP did not, so an
  agent pointed at one — which the bundled `.mcpb` does by default, its
  prefilled URL being the public demo — wrote drafts that the next
  restore erased, with nothing having told it. A sandbox's `initialize`
  instructions now carry one sentence saying it. It is not a tool:
  MCP's default is no because a tool schema is resident in every agent's
  context on every turn (design doc 0067 §4), and this is a sentence
  only the deployment it is true of ever sends — an ordinary
  deployment's resident bytes do not move, and `MCP-BYTES` is unchanged.
  A `dev` deployment stays out on purpose: what it costs is the name on
  a ruling, which is the operator's to know and the CLI already prints.
- **The quick start's first three commands failed on a machine that has
  gcloud but cannot mint a token.** A session due for reauthentication,
  no account selected, or any non-interactive shell (an agent's, a CI
  job's) — the CLI committed to the gcloud path on the mere presence of
  the binary, and the minting failure then aborted every request before
  it was sent, including requests to the public demo, which reads no
  identity and would have answered anybody. 0.19.1 deferred this
  judgement to the server for the machine that has no credentials at
  all; the machine that has some it cannot use is the same case and now
  takes the same path. The request goes out bare, and where an identity
  *was* wanted the 401 carries gcloud's own message, `gcloud auth login`
  included. `ochakai whoami` reports the identity such a client will
  actually present — `human:anonymous` — rather than an error, and
  `ochakai mcp-stdio` no longer fails the bridge for the same reason.
- **Nothing on the CLI said a deployment was a sandbox.** The disposable
  posture announces itself on `GET /api/v1/stats` and the bundled web UI
  turns that into a banner — but the public demo serves no pages, and
  the quick start reaches it with the CLI, so the announcement that
  design doc 0087 §3 requires ("a sandbox that does not say so takes the
  work of whoever wrote there") reached nobody who followed the README.
  `ochakai whoami` now carries a `posture:` line and `ochakai stats` a
  first line, for a sandbox and for a `dev` deployment alike. README
  said the demo "writes nothing", which stopped being true when it
  became a sandbox; it now says what the demo takes and what it erases.
- **The web UI served an empty-looking page wherever it shows a banner.**
  `.shell` is a two-column grid — sidebar, then the view — and the dev
  (`OCHAKAI_MODE=dev`) and sandbox (`OCHAKAI_MODE=sandbox`) banners sat
  in it as siblings of `<main>`. A shown banner therefore took the
  content cell, and the view was auto-placed one row down in the
  *sidebar's* column, where a `position: sticky` sidebar covered it: no
  scroll position reached it, on any screen wider than the 800px
  breakpoint below which the shell stacks. The review queue is where the
  human half of the loop happens, and for the two postures meant for
  trying ochakai out — a local `docker compose` base and a public
  sandbox — it could not be reached from the page at all. The banners
  are now inside the content pane, which is the one grid cell they were
  always meant to share with the view.
  The browser smoke walked all of this and passed: everything it asks
  about was in the DOM, laid out where nobody could see it. It now runs
  at a stated desktop viewport (headless opens 800x600, which is exactly
  the stacked layout) and asks where the view actually is.
- **Documentation that outlived 0.22.0's import change.** A base created
  by following the README held nothing `human-reviewed`, because an
  import no longer rules on what it writes — so README's own
  `--trust human-reviewed` example returned nothing, and so did the
  golden-query canary's first command. The quick start now says that the
  first verification is a move somebody makes, and the pages that answer
  "why is this empty" say it too.
- **The manual described a quick start the README no longer has.**
  `docs/configuration.md` and `docs/README.md` said a local trial needs
  Docker and nothing else, from when the only client command ran inside
  the compose container. README's steps run the CLI on the host, so they
  need the binary; the container route still exists and is now written
  down where somebody looking for it would find it.

## [0.22.0] - 2026-08-11

### Fixed

- **BREAKING (CLI): `ochakai import` no longer verifies what it writes.**
  A document whose frontmatter carried `verified:` was confirmed on
  arrival, with whoever ran the import recorded as the verifier — so a
  bundle of ten concepts came out `human-reviewed` by the importer,
  timestamped the moment the import ran, and the reviewer nobody
  performed was indistinguishable from the one somebody did.
  **Three places already said it should not**: design doc
  [0009](docs/design/0009-provenance-portability.md) §3.2 — *merge は
  verify ではない*, and a verification comes down from
  `POST /api/v1/review/{id}`; [the Git-review
  guide](docs/guides/git-review.md), under that same heading; and the
  note the import itself prints on every such document, which says the
  keys stay a claim that no trust tier answers from. Only the code
  disagreed.
  It disagreed for a reason that leaves no trace in a diff: the call
  landed in 2026-07-28 citing 0009 §3.2, and **that section then read
  「Git はレビュー経路、verifier は取り込んだ者」** — it endorsed exactly
  this. A week later the record was settled and its body replaced, and
  the citation was left pointing at the sentence that now forbids it.
  Nothing failed, because no test watched the ruling surface during an
  import; `TestImportDoesNotRuleOnWhatItCarries` does now.
  **Nothing is lost.** What the document asserted is still on the
  concept, under `received`, exactly as before — a claim, held apart
  from this instance's ledger. What changes is that the ledger stays
  empty until a human rules: `ochakai verify <id>`, or the web UI.
  Anyone who was relying on an import to confirm a reviewed bundle has
  to make that a verify. The demo is the case that found it: a public
  sandbox is anonymous and writable, so **any visitor could import a
  bundle and have every concept come out `human-reviewed`** — by
  `human:anonymous`, which is the tier's whole meaning inverted.

## [0.21.1] - 2026-08-11

### Fixed

- **A release assembles as a draft and is published last, so the MCP
  bundle actually ships with it.** A published release in this
  repository is immutable — GitHub answers `422 Cannot upload assets to
  an immutable release` — and the workflow was adding the bundle, the
  notes and the archive attestations *after* goreleaser had published.
  **v0.21.0 is what that cost**: no `ochakai_0.21.0.mcpb`, no build
  provenance on its archives, and a bare tag where the notes should have
  been, because the upload failed and `set -e` took the two steps behind
  it down. The notes were restored by hand (a body can still be edited
  after publication; assets cannot), and v0.21.0's checksums, image and
  binaries are unaffected — `checksums.txt` verifies and the binary
  prints its own version. goreleaser now leaves a draft, and a final
  step flips it to published once everything is attached.

## [0.21.0] - 2026-08-11

### Fixed

- **Thirteen error responses were missing the `code` the entry below
  already claimed every one of them carries.** `api/openapi.yaml`
  declared `code`'s enum on `components/responses/Error` alone;
  `searchConcepts`'s 400, `reviewConcept`'s 400 and 409, six of
  `getBundlePath`/`putBundlePath`/`deleteBundlePath`'s own 404/409/412,
  and the shared `Forbidden` response wrote `properties: {error}` with
  no `code` at all. That is exactly where the conditions 0083 exists to
  separate live: the three sharing 409 (`already_exists`/`not_deleted`/
  `no_rejection`), the two sharing 412 (a stale `If-Match` versus an
  occupied path under `If-None-Match: *`), and 403's
  `forbidden`/`read_only` split — a developer embedding the REST API
  (C6) had the same prose-matching problem 0083 was written to close,
  on the very statuses it names.
  `TestErrorCodesMatchTheContract` only read the shared `Error` schema
  and could not see any of them. Fixed by declaring `code` at all
  thirteen sites — narrowed to the codes each one can actually answer,
  several carrying two — and a new `TestEveryErrorResponseDeclaresACode`
  that finds an error envelope by shape (any JSON body carrying `error`)
  instead of by which component wrote it, so an inline schema cannot go
  quiet again
  ([#601](https://github.com/na0fu3y/ochakai/issues/601)).

- **A generated REST client works, and the contract was what stopped it.**
  `/api/v1/bundle/{path}` declared its `path` parameter on `get` only —
  `put` and `delete` did not — so the contract said three different
  things about three operations at one address, and every generator
  stopped at the first one it could not resolve. Declared once on the
  path item, which OpenAPI shares across the address: `oapi-codegen`
  now produces a client that compiles, and GET, PUT and DELETE against a
  slash-bearing id all work against a running server. Design doc 0064
  §11 had recorded that *no generator produces a working client for this
  one address*, and that turns out to have been the missing declaration
  rather than the address; the half of it that stands — the empty root
  and the slashes are genuinely inexpressible in an OpenAPI path
  template — is unchanged. **Nothing a caller sends is different**: the
  two lines added to the frozen fingerprint say what every client was
  already sending ([#538](https://github.com/na0fu3y/ochakai/issues/538),
  design doc 0098).

- **The bundled UI drew footnotes as characters, on a bundle it ships
  with.** `sources[].id` exists so a footnote in a concept's body can
  cite one of its sources — `docs/architecture.md` says so, and
  `examples/demo`'s own `metrics/revenue` is written that way — so the
  first concept most readers open showed `systems.[^rev-policy]` in the
  prose and the definition line as a stray paragraph. The server
  recommended a notation its own web UI turned into litter. Footnotes
  render as footnotes now: a numbered marker, a list at the foot in the
  order the prose refers to them, and a link back. A reference nothing
  defines is left as the writer's text, the way an unresolvable link
  already is, and a definition nothing refers to puts no number in the
  margin.

  Three more constructs came with it, because a renderer that drops what
  a writer wrote is the same defect each time: **headings below h3**,
  **block quotes**, and **nested lists** — which were flattened, so a
  runbook's sub-steps read as steps.

- **Following a footnote left the concept.** The router treated every
  hash as a route, so `#fn-…` — an anchor inside the document being read
  — resolved to no route and landed the reader on the home page. Routes
  begin `#/`; anything else is the browser's to scroll to
  ([#536](https://github.com/na0fu3y/ochakai/issues/536)).

- **Deleting a file at a bundle path left its vector behind, still
  ranking the concept that showed it.** `DELETE` on a bundle path
  removed the object and its bytes but nothing dropped the embedding, so
  semantic search went on returning the concept for content that was no
  longer there — and no surface said why. Replacing the bytes had the
  narrower version of the same problem: the row was overwritten only if
  the re-embed that followed the write succeeded, so a provider outage
  or a media type the model declines left the old vector describing
  bytes that are gone. Both writes now drop the vector in their own
  transaction, which is what made the fix a line rather than a lookup:
  the vector is at the path being written.

- **`ochakai reembed` reported work outstanding forever on a bundle
  holding one zip file.** Anything not embeddable — an archive, a
  parquet file, a font, and every image on a text-only embedder — sat in
  the pending queue permanently, because nothing would ever write it a
  vector. Every pass ended non-zero saying *"N items still missing; see
  the server log for why they failed"*, and the server log said nothing,
  since declining a file the model does not take is deliberately silent.
  The queue now holds only the media types this deployment's model can
  be handed.

- **The requirements said `pg_trgm` as though search used it.** It has
  not since migration 0036 moved the lexical index to a `tsvector`
  column: a virgin database ends up with the extension installed and
  zero trigram indexes, because 0001 and 0016 build one and 0036 drops
  it. So the extension is still required — the schema replays from its
  first migration and those two need the operator class — but for the
  replay and for nothing else, and the README, the configuration page
  and the Cloud Run bootstrap SQL now say which. It read worse after the
  README started explaining that Japanese search is two-character
  lexemes: three lines later, `pg_trgm`.

- **The web UI's editor overwrote whatever was saved while it was open.**
  It PUT the document with no precondition, so two curators on the same
  concept — or one who left a tab open over lunch — produced one winner
  and silently lost the other's work. The REST API has had `If-Match`
  since [0030](docs/design/0030-optimistic-locking.md) and the CLI sends
  it; this page was the one client that did not — on the surface this
  product says only a person can operate, losing the one thing an agent
  cannot redraft. The editor saves against the version it opened at now,
  and a save that would have overwritten stops with what happened and the
  text still in the box: the answer is to read what the other person
  wrote, not to retry. The version is the ETag the read carried, not the
  content hash rebuilt from the body — the header is the validator, and a
  page reassembling it would be a second opinion about quoting that
  nothing checks.

- **Archiving a bundle that holds files, on a deployment with no bucket,
  answered `internal error`.** The store refuses that archive on purpose
  — one that silently omitted the files would be a backup missing what it
  claims to carry — but the sentinel it raises never became a client
  error, so the one endpoint that exists to *be* a backup answered 500
  with the reason left in the server's log. It is 501 `unsupported` now,
  naming `OCHAKAI_GCS_BUCKET`, which is what design doc 0013 says a
  missing bucket is and what every path through the service already
  returned. An operator meets this by removing the bucket from a database
  that has file rows. `?files=false` asks for the concepts alone and
  still succeeds. No contract change: the address already declared 501
  ([#546](https://github.com/na0fu3y/ochakai/issues/546)).

- **The web UI read the error sentence to decide what to offer.** The
  editor decided whether to show *"view the existing concept"* by
  matching `/already exists/` against the message — the half the
  compatibility policy says may be reworded in any release, and which
  the server has since rewritten twice, most recently to name what holds
  the id and what to do about it. The condition has been a code on the
  wire since 0.20.0's error envelope; ochakai's own client was the one
  integrator still not reading it, which is the case that made the code
  worth having. `api()` now carries `code` up to its callers, and a test
  reads the page back: every code a branch names has to be one the
  product can actually say, and the shapes prose-matching returns in fail
  the build ([#534](https://github.com/na0fu3y/ochakai/issues/534)).

- **A purge now erases the file bytes too.** Purge destroyed every row
  keyed by a concept's id and left the bytes behind its files in the
  bucket forever — the store said so in a comment and told the operator
  to write the sweep themselves. It contradicted what a purge promises
  ([0031](docs/design/0031-purge.md)) and the first of the eight
  conditions ([docs/surface.md](docs/surface.md): the asset leaves whole,
  returns whole, and is deleted on request), which mattered most for
  exactly the files an erasure request is about. Blobs are
  content-addressed and shared, so no single delete can reclaim them as
  its own side effect: the store now answers the global question
  instead, deleting every blob no object and no attachment references
  any more, and the service runs that sweep after a purge and after a
  file delete. Bytes go before the row that names them, so an
  interrupted sweep finishes on the next run; writers and the sweep
  share an advisory lock, so bytes uploaded for a row not yet committed
  are never swept out from under it. No migration, and no new surface —
  `purge` and `DELETE` mean what they already said. 0031 §3.2 had decided
  the opposite, weighing a blob's storage cost against the risk of
  deleting a live attachment; what belonged on that scale was the
  promise, and the risk is closed rather than accepted, so the record
  saying so is [0099](docs/design/0099-a-purge-reaches-the-bytes.md).
  **v0.13.0's notes for `purge` say it does not reclaim attachment
  bytes** — true then, false from this release.

### Added

- **A write now says what it did in the body, not only in a header.** The
  answer to `PUT /api/v1/bundle/{path}` for a concept carries `plan`:
  `created`, `updated`, or `unchanged` — the same word `Ochakai-Plan`
  gives a dry run and `Ochakai-Unchanged` reports after a real write.
  Absent on a read, which did none of the three. **MCP's `put_concept`
  carries it too**, at `knowledge.plan`, and that surface has no headers
  at all: an agent writing knowledge back had no way to tell a
  replacement from a write that changed nothing without reading the
  concept again. The bundled web UI stops saying *保存しました。* after a
  write that stored nothing.

  **The headers stay.** Removing them is not available: design doc 0082
  §2 puts header names inside the frozen address space, so that half is
  1.0's work — while adding a property to a response-only schema was
  never what the freeze held. The three words moved to `internal/domain`
  from the two private copies that had drifted apart in `internal/restapi`
  and `internal/apiclient`, and that alone moved `VOCAB` from 46 to 49:
  not new words — `Ochakai-Plan`'s enum is in the frozen contract and
  `ochakai import` prints all three — but words no count could see
  ([#539](https://github.com/na0fu3y/ochakai/issues/539), design doc
  0097).

- **A deployment with authentication off says so on every page.** `GET
  /api/v1/stats` answers `insecure_dev: true` under
  `OCHAKAI_MODE=dev`, and the bundled web UI carries a banner while it
  does — design doc 0087's argument for the sandbox, applied to the mode
  beside it. What is at stake differs: a sandbox erases what you wrote,
  while dev keeps everything and authenticates nobody, so the identity
  recorded on a verification or a rejection is whatever the caller asked
  for. The strongest claim this product makes is *a human confirmed
  this*, and on such a base it means nothing. `dev` tells its own
  readers not to deploy it, which is exactly why the page says it too:
  the deployments that need telling are the ones that did it anyway
  ([#535](https://github.com/na0fu3y/ochakai/issues/535), design doc
  0087).

- **The review loop has a shape, not only a number.** `GET
  /api/v1/stats` answers `review.weekly` — eight seven-day buckets of
  verifications, oldest first — and the review page draws them as one
  figure with no axes, no gridlines and no legend, answering one
  question: is reviewing picking up, holding, or stopping. `ochakai
  stats` prints the same row of integers.

  **Verifications and not queue depth**, because queue depth over time
  is not recoverable: all three queues are counted from current state
  and nothing snapshots how deep one was last month. Rebuilding it from
  revisions would be a number that looks right and is not. The ledger
  keeps every ruling with its time and is never pruned, so this half is
  honest for as long as the base exists — and it is the half that
  answers the question, since a queue says what is left and never what
  went through. Absent on a base nobody has ruled on yet: eight zeroes
  read as *"review stopped"*, and *"review has not started"* is a
  different thing to say ([#536](https://github.com/na0fu3y/ochakai/issues/536),
  design doc 0095).

- **The web UI is served under a Content-Security-Policy, and the page a
  screen reader meets is no longer a lesser one.** The policy became
  writable when the page stopped carrying an inline script: `script-src
  'self'` with nothing inline is most of what CSP is for, and one inline
  script forecloses it. Tightening the policy in a browser produces
  violations for inline *style* only and none for script — the browser's
  own confirmation that the half that matters is satisfied.
  `frame-ancestors 'none'` keeps the surface where human rulings happen
  out of somebody else's frame; embedding is what REST is for.
  `style-src` keeps `'unsafe-inline'` for the page's literal style
  attributes and says so rather than hiding the gap.

  Four accessibility gaps go with it, chosen as what makes the page
  unusable rather than as an audit: what the page announces — a save, a
  refusal, a failure — is announced to everyone, focus follows a
  navigation into the main region instead of staying in content that
  left, the current tab says so with `aria-current` and not only with
  colour, and error banners are alerts
  ([#536](https://github.com/na0fu3y/ochakai/issues/536), design doc
  0094).

- **The best match is no longer withheld behind a second round trip.** A
  concept larger than the whole budget became an outline row even at rank
  1 — so at the MCP default of 12,000 bytes the single most relevant
  concept could come back as nothing but its name, forcing exactly the
  round trip `get_context` exists to prevent. The top outline row now
  carries an `excerpt` of the concept's opening when no budget-conforming
  pack could have delivered it. Concepts themselves are still whole or
  absent, never truncated: the reason for that rule — half of an attested
  computation's SQL still looks executable — is satisfied rather than
  set aside. The excerpt is the body and not the front of the document
  (the frontmatter is already the fields on the row), it stops before the
  first code fence so it can never contain part of a query, it prefers a
  paragraph boundary, and `bytes` and `id` beside it say how much more
  there is and where. Top row only, and never more than a quarter of the
  budget ([#533](https://github.com/na0fu3y/ochakai/issues/533), design
  doc 0093).

- **The cold start has a second half: draft golden queries from the
  warehouse's own job history.** `ochakai seed` answers *what tables
  exist*; a base full of columns still does not know what anybody is
  asking of them — and the warehouse has been writing that down all
  along. `examples/bigquery-catalog` gains a second job: it reads
  `INFORMATION_SCHEMA.JOBS` rows as JSON on stdin, groups the runs that
  differ only in their literals, comments or whitespace, and writes an
  OKF bundle of drafts on stdout, one per query somebody keeps running.
  It connects to nothing — you run the query with your own identity and
  pipe the answer in, the same shape `seed` takes and for the same
  reason. **There is no LLM in it**: what a query *means* is the one
  thing a fingerprint cannot produce, so every draft carries an empty
  "The question this answers" heading, which is where your own agent
  belongs. Everything lands as a draft, because a query run forty times
  is evidence that it matters and not evidence that it is right
  ([#537](https://github.com/na0fu3y/ochakai/issues/537)).

- **A release now carries `ochakai_X.Y.Z.mcpb`, the one-click install for
  Claude Desktop.** Connecting a desktop app meant finding a JSON file
  the app does not show you, writing a `command` that has to be an
  absolute path because the app inherits no `PATH`, and restarting it —
  four steps in the manual, and the first one is where people stop. The
  bundle is the same `ochakai mcp-stdio` bridge with that JSON written
  for them: open the file, and the app asks for one thing, the server
  URL, with the public demo already filled in. macOS and Windows, which
  are the platforms that install a bundle; Linux keeps `go install`, the
  archives and the image. **It does not remove the gcloud requirement**
  against Cloud Run — the bundle carries ochakai's binary, not your
  Google identity — and the manual says so where it used to say no such
  bundle existed. About 38 MB, because it carries a macOS universal
  binary and a Windows one; the manifest can vary its command by
  operating system and not by CPU, so the architecture is settled before
  packing. Not in `checksums.txt` (goreleaser writes that first) and
  covered by the build provenance attestation instead
  ([#526](https://github.com/na0fu3y/ochakai/issues/526)).

- **The README is one staircase, and it says the product is good at
  Japanese.** The quick start ran demo → your own server → an agent, but
  the rungs were unlabelled and production was not one of them, so a
  reader still had to work out which entrance was theirs; it is three
  named steps now (sixty seconds, ten minutes, for real), and the last
  one carries Cloud Run and Cloud SQL instead of leaving them to a link
  in the intro. Filling a base with your own tables leads with `ochakai
  seed` — shipped for exactly that and named nowhere on the front page —
  and the ten-minute rung uses the CLI the first rung installed rather
  than reaching into the container. **The word "Japanese" appeared in
  this README only as a warning label on documentation links**, never as
  something the product does, so a Japanese-speaking evaluator learned
  only that the manual might be unreadable to them. There is a *Why*
  bullet now: no tokenizer extension to install (which managed Postgres
  would not allow anyway), 売上 answered from an index rather than a
  scan, latin words stemmed, nothing to configure
  ([#527](https://github.com/na0fu3y/ochakai/issues/527)).

- **A renamed MCP tool or CLI command keeps answering for one release**
  ([0088](docs/design/0088-a-retired-name-answers-for-one-release.md)).
  The policy used to be that there was no deprecation window for
  anything but REST. That was honest, and it sent the notice to the
  wrong reader: an agent's configuration names tools literally, a CI
  script names commands literally, and it is people — not agents — who
  read a changelog. From this release, a rename leaves the old spelling
  answering until the next release. An MCP call under the old name is
  forwarded and comes back with a line naming the tool to use; a CLI
  command says the same on stderr.

  **Callable, but not listed.** `tools/list`, `ochakai help`,
  `docs/cli.md` and the completion scripts stay quiet about the old
  name, so an agent that never learned it never will — a tool
  description is paid for out of an agent's context window, and one
  about to stop existing is not worth the room. Nothing is retroactive:
  names dropped before this release stay dropped. Names only — flags,
  argument names, printed shapes and the stored form have no window,
  and REST is frozen so nothing there is renamed at all.

  The window closes on schedule because closing it is checked: each
  retired name carries the release that retired it, and CI fails once a
  later release is cut with the entry still in place, which puts the
  deletion in the release PR (`TestARetiredNameLastsOneRelease`). No
  surface count moves — `MCP` reads `tools/list` and `CLI` reads the
  command table, which is what a user actually pays for. This is the
  counterweight to what [0082](docs/design/0082-what-the-freeze-holds-still.md)
  loosened on the REST side; `docs/compatibility.md` states both.

- **`stats` says how much of the base vector search can actually see.**
  A concept longer than the embedding model's input window is embedded
  from its front and the rest is dropped — and the result is
  indistinguishable from success: it has a vector, it ranks, it comes
  back, and it is simply unfindable by anything said in its second half.
  The longer and more valuable the document, the more of it is gone, and
  no surface said this was happening. `GET /api/v1/stats` now answers
  `embedding` with `enabled`, `vectors` and `truncated`; `ochakai stats`
  prints the pair (`-` where there is no embedder, as with misses); the
  web UI's review page draws a line about it only when it is not zero,
  as it does for dropped observations. Read the two together — three in
  four is the shape of the corpus and wants a wider model, three in four
  hundred is one long runbook and wants splitting. **Chunking is
  declined for now**, and [0089](docs/design/0089-a-half-embedded-concept-says-so.md)
  §4 states the condition for reopening it. The vector rows gain a
  column, filled by the next write or by `ochakai reembed`; existing
  rows read `false`, which is "not known to be truncated" rather than a
  claim. Concepts, not files
  ([#532](https://github.com/na0fu3y/ochakai/issues/532)).

- **`OCHAKAI_MODE=sandbox`: a public deployment anyone can write to, and
  that says so** ([0087](docs/design/0087-a-sandbox-says-it-is-one.md)).
  The loop is what this product is about — an agent drafts, a human
  rules, an outcome comes back — and it was the one thing nobody could
  try: the public demo is `public`, which implies read-only, so seeing a
  single write-back meant standing up Postgres first. A sandbox is the
  fourth cell of the posture square deployed on purpose: anonymous like
  `public`, writable like the default, and restored on a schedule by
  whoever runs it. It is not `dev` renamed — `dev` says in its own
  documentation that it is not for a deployment, an operator reading a
  config cannot tell an accident from an intent, and the Terraform module
  grants `allUsers` only to a posture that asked for it (`sandbox = true`
  now does, beside `public_read_only`).

  **A sandbox that does not announce itself steals the work somebody
  curated into it**, so the product says it rather than a README hoping
  to be read: `GET /api/v1/stats` answers `sandbox: true` — a
  response-only addition, outside the freeze
  ([0082](docs/design/0082-what-the-freeze-holds-still.md)) — and the
  bundled web UI carries one banner on every page. Misses are not
  recorded, for the reason a public deployment does not record them. No
  new environment variable: one more word for the one that already says
  what a deployment is. The restore job is the operator's, and
  [the operating guide](docs/guides/operating.md) has it.

- **A deployment can authenticate with its own OIDC issuer**
  ([0086](docs/design/0086-a-second-way-to-say-who-is-calling.md)).
  Outside Google Cloud there were two postures and neither is a
  deployment: `dev` authenticates nobody, `read-only` authenticates
  nobody and refuses every write, and everything in between rested on
  Cloud Run's IAM check having already happened in front of the process.
  Setting `OCHAKAI_OIDC_ISSUER` and `OCHAKAI_OIDC_AUDIENCE` makes ochakai
  verify the bearer token itself — signature, issuer, audience, expiry —
  against the issuer's published keys.

  **No secret is introduced.** Verification reads public keys over
  HTTPS and the algorithm allowlist is asymmetric only, so there is still
  nothing to issue and nothing to rotate — which is the property
  [0003](docs/design/0003-gcp-only.md) was protecting rather than Google
  Cloud itself. Also unchanged: there is still no authorization, and
  embeddings are still Vertex AI or nothing, so search off Google Cloud
  is lexical — which since migration 0036 looks Japanese terms up in an
  index rather than scanning for them. A verified email is recorded as a
  person; an email the issuer will not vouch for is never used, and the
  subject is recorded as a process instead. Half a pair, or a pair beside
  `OCHAKAI_MODE=dev` or `public`, is refused at startup. `ENV` 11 → 13.

  **The contract now declares the 401 it was already sending.** Both
  paths answer it — the Cloud Run posture when no token was forwarded,
  this one when verification fails — but `api/openapi.yaml` had never
  said so, and its own prose claimed *"ochakai itself verifies
  nothing"*, which in-process verification makes false. The 401 is
  declared on every operation (`components/responses/Unauthorized`,
  carrying `code: invalid` in the ordinary envelope) and the prose now
  states both postures — the same retroactive declaration design doc
  0064 made for the 400 and 413 the server already sent. Nothing a
  deployment answers changed.

- **One page of the manual is in English** ([docs/en.md](docs/en.md)).
  The manual is Japanese by decision — condition C8 — and the front door
  and the contract are the seven pages that stay English, which left an
  evaluator who reads neither Japanese nor Go at a wall on step two: the
  pitch was in their language and everything needed to run it was not.
  This is not a translation and there is still no mirrored page, because
  two texts saying the same thing go out of step and nobody can tell
  which half is stale. It is the page that makes the Japanese ones
  usable: what each holds, in what order to read them, and the handful of
  names — the environment variables, the five postures of
  `OCHAKAI_MODE`, the two shapes of an MCP connection, the absence of
  authorization — that you cannot find by skimming a page you cannot
  skim. Machine translation handles prose well; what it cannot tell you
  is which page to open. The environment-variable list is the one
  duplicate, and it is checked against the build rather than trusted
  (`cmd/ochakai/surface_test.go`). `DOC` 25 → 26 and `DOC-LINES`
  5,900 → 6,000.

- **`ochakai seed` fills an empty base without ochakai touching a
  warehouse.** A new deployment starts empty, and catalog products solve
  that with connectors that crawl the warehouse — which ochakai does not
  have and will not
  ([0081 §1](docs/design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md)).
  `seed` is the third way: you run the `INFORMATION_SCHEMA` query
  yourself, with your own client and your own identity, and pipe the rows
  in; it prints an OKF bundle of `BigQuery Table` **drafts**, and the
  existing `ochakai import` writes them.

  ```sh
  bq query --format=json --nouse_legacy_sql '…' | ochakai seed - | ochakai import -
  ```

  No warehouse client is linked into the binary and no credential is
  read, so the refusal is untouched and the command serves any warehouse
  that can spell those rows. Every concept comes out `draft` with an
  empty description on purpose: what a table is for, which column lies
  and when the load is late are the reasons anybody reads the entry, the
  schema knows none of them, and a sentence nobody wrote is one everybody
  would trust. A connector produces thousands of rows nobody vouches for;
  this produces thousands of questions, which land in the review queue.
  CLI 24 → 25 and FLAG 28 → 29, said out loud
  ([0085](docs/design/0085-the-empty-base-and-what-fills-it.md)).

- **Every request now logs one structured line.** `/health` was the whole
  observability surface, so "search got slow" had no answer short of
  attaching to Cloud SQL by hand and "what is failing" had none at all.
  Each `/api/v1` and `/mcp` request writes a `request` line when its
  response ends, carrying the method, the **route pattern** the mux
  matched, the status, `duration_ms`, the bytes written, the actor's kind
  and — on a failure — the error `code`. A log-based metric can be built
  from it, which is why this is not a `/metrics` endpoint: the platform
  the product already requires is the one that aggregates, and REST stays
  the only address space ([0067 §1](docs/design/0067-four-faces-and-what-they-decline.md)).
  Two things are deliberately absent, and both are the same rule: the
  route rather than the path, because a bundle path is a concept's
  address and the log is not a second copy of the knowledge base's shape;
  and the actor's kind rather than their name, because who wrote what is
  provenance and lives on the concept
  ([0065](docs/design/0065-identity-and-provenance.md)). Still no
  telemetry — nothing is reported anywhere, the line goes to this
  deployment's own stdout. No new endpoint, environment variable or wire
  field; [the operating guide](docs/guides/operating.md) says how to read
  it.

- **A search hit now says why it matched.** `snippet` carries the passage
  of the body where the query's own words land, cut to about 140
  characters and marked with … where it was cut. Title and description
  say what somebody wrote about the concept as a whole, not what answered
  this question, so choosing among ten hits meant fetching bodies — for
  an agent, nine documents' worth of context spent to find the one it
  wanted. It is absent when there is no passage to point at: the words
  landed in the name or the description, where the hit already shows the
  match, or the concept was found by meaning rather than text, where the
  opening of the body would not be the reason. Listings have no query and
  so no snippet. REST, the CLI (` · ` after the description; the
  tab-separated fields are unchanged) and the web UI show it; MCP
  responses carry it and its tool descriptions do not grow
  ([0084](docs/design/0084-a-hit-says-why-it-matched.md)). No new
  parameter, flag, command or environment variable, and no migration —
  the bodies are already fetched for the hits being returned.

- **Every error response now carries a machine-readable `code`.** The
  envelope was `{"error": "<English prose>"}` and nothing else, so a
  client that wanted to tell "this id is taken" from "there is no
  rejection to withdraw" — both 409 — had to match a sentence
  [docs/compatibility.md](docs/compatibility.md) says may be reworded in
  any release. `code` is a closed vocabulary of twelve words beside the
  sentence, declared as an enum in
  [api/openapi.yaml](api/openapi.yaml) and checked against the product's
  own list, so the two cannot drift. It separates the three conditions
  that share 409 and the two that share 412 (a stale `If-Match`, and an
  occupied path under `If-None-Match: *`). The sentence stays exactly as
  free to change as it was; branch on the code. This is an addition to a
  response-only schema, which
  [0082](docs/design/0082-what-the-freeze-holds-still.md) places outside
  the freeze — its rule now reads both places a response schema can live,
  which is how the envelope qualified. `VOCAB` rises 34 → 46: prose that
  becomes something a client branches on is prose that stopped being free
  to reword, and `docs/surface.md` had named error wording as the next
  place surface would hide ([0083](docs/design/0083-an-error-carries-a-code.md)).

- **The positioning page now answers the graph-database claim.**
  "Operationalizing ontology takes a graph engine" is the pitch
  NebulaGraph and Neo4j publish under; the neighbours table had no row
  for that category, though it already answered Palantir's FDE model. A
  row and a short section map the claim onto decisions already made —
  untyped links ([0074 §2](docs/design/0074-the-document-and-the-vocabulary-that-asks-it.md)),
  the open type vocabulary ([0038](docs/design/0038-type-vocabulary-realignment.md)),
  and the human review loop their maturity model sells as its final
  stage. `DOC-LINES` crosses a boundary, 5,700 → 5,800
  ([#542](https://github.com/na0fu3y/ochakai/issues/542)).

- **The README now shows what `ochakai context` actually returns.** The
  quick start had a reader point the CLI at the public demo and run the
  one call an agent makes before a data question, without ever showing
  the answer — seeing it meant installing first. A condensed excerpt from
  the demo bundle now stands beside it: the seasonal shape that explains
  a 15% August, the escalation threshold, and the draft metric that comes
  back marked `draft` with its three unsettled questions and the agent
  that wrote it still named in its frontmatter. No surface moved: the
  nine dimensions in [docs/surface.md](docs/surface.md) are unchanged,
  and the added prose fits under the `DOC-LINES` ceiling 0.20.0 already
  raised, so no ceiling moves either.

### Changed

- **BREAKING (MCP): `list_concepts` splits off `search_concepts`, which
  no longer takes `sort` or `cursor`.** A call that passed either is now
  refused — by schema validation, before any handler runs, which is the
  point. One tool held two capabilities and one schema could not say so:
  `query` was required unless `sort` was set, `cursor` was refused unless
  it was, and `limit` meant 10/50 one way and 100/1000 the other. None of
  that is expressible in JSON Schema, so all of it lived in prose an
  agent had to obey unaided, and getting it wrong cost a turn to
  discover. Each tool now states its own rules in its own shape: search
  takes a query and ranks, list takes a feed and pages.

  **No capability is added, and the price is stated rather than
  absorbed.** The seven filters are on both tools, so the resident bytes
  every connected agent holds go 11,829 → 13,433 (+13.6%) and
  `MCP-BYTES` rises 12,000 → 13,500 — the first time that ceiling has
  moved, which is what it exists to make visible. There is no grace
  window: a retired *name* is forwarded for one release (design doc
  0088), and what changed here is arguments
  ([#530](https://github.com/na0fu3y/ochakai/issues/530),
  [#539](https://github.com/na0fu3y/ochakai/issues/539), design doc
  0096).

- **`go install` works again on a toolchain that is not the newest
  patch.** `go.mod` named `go 1.26.5`, and a `go` directive naming a
  patch makes that patch a *requirement*: a CI pinned with
  `GOTOOLCHAIN=local` on 1.26.2 failed at `go install` instead of
  building, which is the first command the README gives. The patch moves
  to a `toolchain` line, where it is the preference it was meant to be —
  the local setting may decline it, and a toolchain free to fetch still
  uses 1.26.5 ([#535](https://github.com/na0fu3y/ochakai/issues/535)).

- **BREAKING (MCP, REST semantics): `get_context`'s byte budget now
  governs the whole response, `hits` included.** The ranking rode outside
  it, on the grounds that a rank carries no body and the count is capped.
  Both are true and neither is enough: `hits` is up to 40 rows and about
  5 KB, so a caller that asked for 12,000 bytes received 12,000 plus an
  amount it could not compute — which is not a number a context window
  can be sized by. The same argument had already moved `outline` inside.
  The budget governs the response and what it **buys** is whole concepts;
  the ranking and the outline are the floor and always come back, because
  only a caller that knows something was there can raise a budget for it.
  Nothing is trimmed to make room. At the default `limit` of 5 the
  ranking is about 10% of the default budget, so most callers will see a
  concept or two fewer; a caller that asked for `limit=20` pays what it
  chose ([#533](https://github.com/na0fu3y/ochakai/issues/533), design
  doc 0093).

- **The web UI is ES modules, and its tests run.** The page was one 2,438
  line file, and its thirteen tests were greps over that file as a
  string — nothing asserted what any of its code returned, on the surface
  where a person decides whether to trust a concept. It is `index.html`,
  `app.css` and eighteen modules now, cut by direction of dependency so
  that the ones that never touch the document — escaping, formatting, the
  markdown renderer, the vocabularies, the document edits — can be
  imported and held to examples. **Nothing about how it is built or
  served changes**: no build step, no framework, no CDN, no npm
  dependency, and the same two commands serve it. The split found four
  functions that were declared and never called, which are gone. The
  assets carry an ETag and `Cache-Control: no-cache`, so a load costs
  twenty 304s rather than twenty downloads
  ([#536](https://github.com/na0fu3y/ochakai/issues/536), design doc
  0092).

- **BREAKING (stored shape): a file's vector is keyed by its path, and
  the old vectors are dropped at startup. Run `ochakai reembed` once
  after upgrading.** The key was `(concept id, filename)` — the shape
  from when a file was something a concept *had*, addressable only at
  `<id>/<name>`. Since files became objects of the bundle, attributed by
  any body that links them, the name has been the path's last segment,
  so **a concept whose body showed `charts/q1.png` and `tables/q1.png`
  wrote both to one row**: the second vector overwrote the first and one
  of the two files silently left semantic search. Silently is the word —
  the file was still stored, served, exported, and found by name; what
  was missing was the "ask it another way" path that embeddings exist
  for. The key is the path now and **which concept a hit belongs to is
  read at search time from the bundle**, so a file the body links today
  ranks that concept today with nothing re-embedded, a file the body
  stops linking stops ranking it at once, and one file two concepts show
  ranks both from a single vector. The old rows are dropped rather than
  migrated: the overwritten side no longer exists, and a surviving row
  cannot say which file it described. Until `ochakai reembed` runs,
  files are found by name — where a deployment without embeddings
  already is — and concept vectors are untouched. A reembed cursor
  issued by an earlier release is refused with a 400; the table it would
  resume into is empty, so there is nothing to resume
  ([#539](https://github.com/na0fu3y/ochakai/issues/539), design doc
  0091).

- **The promotion queue and the re-verification feed rank on the last 90
  days.** The usage counters are running totals and never decay, so a
  concept read a lot in a knowledge base's first month sat at the top of
  `sort=usage` for as long as the deployment lived — a queue whose job is
  *"look at this next"* answering *"look at what you already looked at"* —
  and on `sort=failed` a long-ago repeat offender outranked the concept
  failing this week, on evidence the instance had already pruned. Both
  feeds order by the window now, with the lifetime totals kept as the
  tie-break, and the counts reach the wire as `usage.recent` carrying
  their own `days` so an order nobody can see is not mistaken for a
  broken sort. The window is a constant, not a setting, and has to fit
  inside the raw events' retention
  ([0090](docs/design/0090-a-queue-ranks-on-what-happened-lately.md)).
  `ochakai usage` prints three more lines, the review cards read
  `fetched 20 (3 in 90d)`, and MCP does not carry it — ordering a queue
  is the curator's job. Cursors from before this release are refused with
  a sentence, as a cursor whose listing changed shape always has been.

- **BREAKING (MCP): the `trust` filter on `search_concepts` and
  `get_context` is now `trusts`.** This surface takes its filters as
  arrays of values, and the other four say so in their name — `types`,
  `statuses`, `tags`, `prefixes` — while this one alone was singular. It
  is the same rule with one spelling broken, so no capability, argument
  or response changed, and the descriptions now say "several are OR-ed"
  the way `prefixes` already did instead of REST's "repeat to OR". REST's
  `trust=` and the CLI's `--trust` are unchanged: those are repeatable
  singular parameters, aligned there with `type` / `status` / `tag` /
  `prefix`. OKF is untouched — SPEC §5.3's trust is a tier derived from
  `verified` rather than a frontmatter key, and the `trust` a response
  carries (one concept, one tier) keeps its singular name and its three
  values. A client still sending `trust` fails the call rather than
  silently searching unfiltered — the tool schemas are
  `additionalProperties: false`, so the rename surfaces the same way an
  unknown REST query parameter does (design doc 0064 §2). Check any MCP
  caller that pinned a tier: design doc 0088's one-release window is for
  tool and command *names*, and it says so — arguments, flags and output
  shapes are outside it (§2), so this takes effect on the release that
  carries it.

- **The bundled web UI is in Japanese.** The manual has been Japanese
  since the C8 translation, and the interface it describes was not: the
  reader the product is aimed at was reading a Japanese page about an
  English screen. The pages are translated — the navigation, the forms,
  the banners, the empty states, the feed explanations, the placeholders
  and the titles — and the page declares `lang="ja"`.

  **The words the page says back are translated too**, in a second pass
  rather than the same bulk one, because a toast is read at the moment
  somebody is deciding whether their write landed: every action toast
  and confirmation (saving, verifying, rejecting, moving, deleting,
  attaching), every failure banner, the stat-tile labels on the usage
  and review views, `load more`, the editor's two buttons, the
  provenance line that says who created and confirmed a concept, and
  the relative dates (今日 / 昨日 / N 日前). With them went two the
  page was contradicting itself over: the search bar's three filter
  chips, which said `verification age`, `reported wrong` and `stale`
  while the feed banner above them told the reader to uncheck
  検証の古さ, 間違いと報告された and 期限切れ — and the review page's
  stale toggle, named in Japanese prose and drawn in English. What
  stays in Latin script is the contract's own vocabulary, because it is
  what the API answers and what the page filters on: the lifecycle
  values (`draft`, `stable`, `deprecated`), the trust tiers, the
  rulings, the OKF type names, and `concept` itself.

  Nothing else moved: no
  route, no id, no `data-` marker, no API call, and the tests that pin
  the write affordances read markers rather than words, so they still
  say what they said. The three Japanese pages that quoted a button by
  its English label now quote the Japanese one.

  Two things are deliberately left: comments in the source stay English
  (CLAUDE.md's rule for code), and **the screenshot in the README and
  [the loop guide](docs/loop.md) still shows the English UI** — it is a
  binary this change cannot regenerate, and a caption that lies is worse
  than one that is late.

- **`DOC-LINES` moves on a 500-line grid instead of a 100-line one.** The
  ceiling on how much manual a reader gets through was widened once
  before, from 10 lines to 100, because a line every documentation PR
  rewrites is a line nobody reads and a collision every parallel PR has.
  The same thing happened again at the new width: between 2026-08-03 and
  08-09 it went 5,300 → 5,400 → 5,500 → 5,600 → 5,700 → 5,800 → 5,700 →
  5,800 → 5,900 → 6,000. **Adding one feature and writing down how to use
  it crosses 100 lines by itself**, so the paragraph each crossing earns
  stopped being worth reading — which is the failure the mechanism names
  for itself ("an alarm that rings every time tells you nothing"). The
  record corpus already made this call at 500
  ([CONTRIBUTING.md](CONTRIBUTING.md)), and two ceilings on two amounts
  have no reason to ring at different widths. The cost is accepted and
  stated: up to 500 lines of headroom can now go unreturned. Retiring the
  ceiling was considered and declined — of the ten crossings only one or
  two were the escape hatch it exists to catch (folding a surface and
  writing prose about the fold), and a wider grid is what leaves only
  those ringing. **`DOC-LINES` ends this release at 6,500**: the grid
  rounds up to the next 500, so the same change that widened it is the
  last one to move it here.

- **A confirmation is worth one rank position in the hybrid ranking,
  where it used to be worth 7.6.** The addend a verified concept got in
  the fused score was 0.002 and its comment called it small; against
  reciprocal rank fusion's own step — 1/61 for first place, 1/62 for
  second, so 0.000264 per position — it was large enough that a concept
  the words put eighth overtook one they put first on the strength of
  having been checked. That is a re-ranking, and no record asked for one:
  verification says somebody looked, not that this answers your question.
  The golden set could not see it because nothing in the corpus was
  confirmed, so a confirmed concept is now part of the fixture, and it
  measured the constant at once — **fused MRR 0.83 with the old addend
  against 0.90 with one rank position**, the confirmed policy climbing
  over the concepts that actually answered eight questions. Expressed in
  ranks, a confirmation wins the ties it is in and closes a gap of less
  than one place, which is what the comment always claimed. The lexical
  ranking is unchanged; hybrid deployments will see confirmed concepts
  move less than they did.

- **The store's score now settles the ties reciprocal rank fusion
  leaves.** RRF reads rank and discards the number, which is what makes
  it safe to merge lists whose scores share no scale — and why a fused
  list arrives full of exact ties: placing second and fifth in the
  opposite lists sums to the same thing either way round. Those fell to
  verification recency, which is not a statement about the question. The
  lexical score is, so it decides first, and only where RRF decided
  nothing. Four percent of adjacent pairs in the golden set are such
  ties; the measured numbers do not move, and the rule is pinned by a
  unit test rather than by them.

- **docs/architecture.md said Japanese search reads the table.** It has
  not since migration 0036 replaced the trigram index with a `tsvector`
  column whose lexemes are the same two-character windows a question is
  cut into. The page still carried 0016's 16 ms measurement and its
  "that scan is the price", which is the product's main differentiator
  described as the defect it used to be. ROADMAP was already corrected;
  this page was missed.

- **The search evaluation harness now measures the merge, and asks 36
  questions instead of 14.** Fourteen cases put MRR inside its own noise
  — one case moving from rank 1 to rank 2 moved it by 0.036, which is the
  size of the differences people were reading — so the golden set gained
  a second pass over the same corpus, phrased the way somebody asks
  rather than the way a title reads. Beside it, a deterministic stand-in
  encoder gives the harness a second ranking to fuse, so RRF's
  arithmetic, the named-concept sort key surviving it and the
  verification tie-break under it are measured rather than reasoned
  about: lexical 1.00 / 0.92, fused 1.00 / 0.89, and dropping the lexical
  list from the fuse takes it to 0.80. The stand-in shares the lexical
  side's vocabulary, so it stands in for the merge and not for an
  encoder — what a trained one buys is matching text that shares no words
  at all, and no number here says how much that is. Tests only; no
  surface, no wire, no behaviour change.

- **`put_concept` now tells an agent to link the concept its draft would
  replace.** The instruction was already there — a verified concept that
  is wrong gets `report_outcome failed`, or a better draft at a different
  id for a human to promote — but nothing joined it to the sentence about
  links, so the relationship that makes a replacement reviewable was left
  to chance. A body link is how any relationship is written here
  ([0074 §2](docs/design/0074-the-document-and-the-vocabulary-that-asks-it.md)),
  and it is two-way, so the reviewer reaching either concept sees the
  other. No tool, no argument, no ceiling moved: the MCP schema budget
  goes 11,733 → 11,812 bytes, under the 12,000 `MCP-BYTES` stood at when
  this landed — the tool split above has since raised it to 13,500.

- **Search ties are broken by verification recency instead of by
  whatever the scan produced.** A short question leaves several concepts
  holding exactly the same score — five of them, for the README's own
  "why is revenue down" — and the order among them decided what an agent
  reading the top of the list under a byte budget saw. When the text
  cannot separate two concepts, the loop's own signal can: the one
  somebody confirmed most recently leads, and the id closes the order so
  two concepts equal in every way still arrive the same way twice. Both
  the store's ranking and the fused hybrid ranking follow the rule. It
  breaks ties only — no weight, no addend — so nothing the text does
  distinguish is outranked. The golden set barely moves (one case rose
  from rank 5 to 4; recall@10 1.00 and MRR 0.90 both hold), which is
  expected: what this buys is that the answer no longer depends on the
  scan.

- **The MCP client guide writes out only the two setups anyone here
  runs.** Six clients — Cursor, VS Code, Windsurf, Cline, Zed, Gemini CLI
  — carried a full configuration example each, copied from their own
  documentation in July 2026 and, as the page itself said, run by nobody
  here. That is design doc 0068 §3's rule (an entrance nobody arrived
  through comes down) landing on the *description* of an entrance rather
  than an entrance, and an unrun example goes stale with no one to
  notice. The two forms are identical across clients anyway — a URL, or
  the `ochakai mcp-stdio` bridge — so what survives is one row per client
  naming its config file and the key it spells the URL with
  (`serverUrl`, `httpUrl`, `servers`, `context_servers`), plus the two
  wrong-key failures that fail silently. Nothing became unreachable, no
  tool moved, and the README now says MCP is for clients that cannot
  shell out — Claude Desktop, and your own services — instead of naming
  hosted agents, which the guide has always said cannot reach an
  IAM-restricted deployment at all. `DOC-LINES` drops 5,800 → 5,700.

- **`DOC-LINES` is back at 5,800**, because the fold above landed in
  parallel with a change that has no entry of its own — [the dogfooding
  entrance](kb/README.md), development wiring an operator upgrading does
  not need — and the two only met on `main`: the fold was real
  (−176 lines) and that entrance was a page-sized decision (+27), but
  neither PR could see the other, and the merged total is 5,720 — past
  the ceiling the first of them had just lowered.
  [docs/surface.md](docs/surface.md)
  already described two PRs' prose merging across a boundary in the
  upward direction; this is the first time it happened against a ceiling
  on the way down, and the paragraph it earns is in that document.

- **`ochakai import` keeps eight requests in flight instead of one.** An
  import (and its `--dry-run`, which makes the same round trips) was one
  sequential HTTP call per document, which priced a real catalog
  projection — thousands of documents — in tens of minutes against a
  Cloud Run service. The counts and the exit code are unchanged; output
  lines now land in completion order, so the summary line is the stable
  thing to read. A fatal error (auth, network, 5xx) still aborts: it
  cancels the requests not yet started and returns once the ones in
  flight finish.

- **A transient Vertex AI failure no longer silently degrades search on
  the first try.** An embedding call now gets three attempts with a short
  backoff on a 429, a 5xx or a dropped connection, and a 30-second
  per-attempt deadline instead of none — a call that hung held the
  search that was waiting on it. A 4xx is still never retried.

- **Daily Cloud SQL backups are on by default in the Terraform module.**
  The database is the knowledge base, and the copy-paste starting
  configuration protected everything except it. The cost example rises by
  under a dollar; a throwaway evaluation can still opt out with
  `database_backups = false`.

- **The server now bounds request reads and idle keep-alive.** A body
  must arrive within a minute (the largest accepted body is the 4 MiB
  PUT cap) and an idle connection is closed after two; previously only
  header reads had a deadline, so a slow-drip body or a hoarded
  connection was held open indefinitely. Response writes stay unbounded,
  deliberately: MCP's streamable GET is a hanging stream by design.

- **A search for a name now finds the concept with that name.** Ask for
  売上 and the concept *called* 売上 comes first, ahead of the reports
  that mention the word — which is not what happened before: the lexical
  haystack is one column (id, title, description, tags, body, filenames
  concatenated), so a concept whose whole name is the query and a monthly
  report that says the word eight times in five kilobytes contained the
  same fragments and the same whole query, scored identically, and the
  order between them was whatever the scan produced. Two terms fix it.
  Fragments landing in the name count half again, weighted the same way,
  so it works for a question and not only for a keyword; and a name that
  *is* the query outranks everything that merely contains it. Either name
  serves — the title, or the id's last segment, since a filename is a name
  somebody chose (design doc 0022). The rule survives fusion, which reads
  rank alone and would otherwise hand the top back to whatever the vectors
  liked, and every face reads the same ranking, `get_context` included.
  No migration, no reindex, no reembed: both terms are expressions on rows
  the scan already reads, and the candidate set is unchanged (a Japanese
  keyword scan measured about 4% slower on 5,000 entries).

- **A compound question now finds the concepts it names.** 「EC事業の
  アクティブ会員の継続率を計算して」 is the name of nothing, so the rule
  above never fired for it, and coverage decided instead: the score is the
  fraction of the query's fragments an entry contains, a weekly memo that
  co-mentions every term covers them all, and each definition covers only
  its own third. Measured on a seeded corpus, the pack `get_context`
  returned for a question chaining three 社内用語 was five meeting memos
  and none of the three definitions. The name rule now reads the query's
  *terms* — the maximal runs of letters and digits between the hiragana,
  so EC事業, アクティブ会員, 継続率 and 計算 fall out of the example with
  no morphological analyzer — and a concept named by any of them outranks
  the prose that merely says all the words, in the store's ranking and
  through fusion both.

  A run counts as a term only when it carries a Han or Katakana
  character, and that line was measured, not reasoned: scripts written
  with spaces make every word of a question a run — why, is, down — and
  a question saying revenue is not a lookup of the concept called
  revenue. Granting those the lift cost the golden set six points of
  lexical MRR (0.92 to 0.86), questions losing their answers to the
  definitions of their own words, so search in spaced scripts is
  unchanged. The stated cost: a purely latin name (SaaS), an all-digit
  one (2026), or one with hiragana inside it (売り上げ) is lifted only
  when the query is the name whole — the reach the rule already had.
  Same candidate set, no migration; the golden set reads the same
  lexical 0.92 as before, and fused 0.90 against 0.89.

- **The write faces now say what each recommended type holds**, one line
  per type, instead of handing over nine spellings and nothing to tell
  them apart: `put_concept`'s description and `ochakai put -h` both render
  the same sentences from one place in `internal/domain`, so a tenth type
  cannot arrive on one face and not the other. The scattered advice
  `put_concept` had grown for four of the nine types is folded into it —
  including the line that still told agents to write `attrs.question`,
  which 0.16.0 flattened away — the guard that pins this for the shipped
  prose now reads the MCP tool too.

## [0.20.0] - 2026-08-07

### Added

- **Each CLI release archive ships with an SPDX SBOM** (`*.sbom.json`,
  attached to the GitHub release beside the archive), so a dependency
  scanner can read what is inside without unpacking anything. The
  container image already carried an SBOM in its manifest; the archives
  now match ([docs/guides/operating.md](docs/guides/operating.md)
  (Japanese) has the verification commands).

### Changed

- **BREAKING (deployment behavior)** — text is now embedded in the region
  you deployed to, not in `us-central1`. A deployment that names no model
  discovers its project from the metadata server and turns semantic search
  on; until now the
  *region* of that call was a constant the product held, so a service
  running in `asia-northeast1` sent every concept body and every search
  query to Vertex AI in the United States to be embedded. That is a
  data-residency decision, and ochakai was making it silently on an
  operator's behalf. The region is now discovered alongside the project
  ([0080](docs/design/0080-search-and-how-a-deployment-embeds.md) §1.2).
  **What an operator does: nothing, and no `reembed`** — the model is
  unchanged, and the same string embedded in `asia-northeast1`,
  `us-central1` and `europe-west4` came back bit-identical (3072 values,
  maximum difference 0.0), so vectors already stored stay comparable.
  Two things do change: the startup line now carries `location=`, which
  is the answer to "where did the text go", and a deployment in a region
  with no `gemini-embedding-001` (`asia-northeast2`, for one) falls back
  to lexical search with a log line saying so, where before it would
  quietly have embedded in the US. A named resource name still wins over
  all of this — and naming `gemini-embedding-2` still means
  `global`/`us`/`eu`, so that choice moves the call out of your region on
  purpose.

- **The two records a newcomer opens first no longer describe a world
  that is gone.** `0001` (the founding architecture record) named six MCP
  tools and three REST paths that do not exist, a four-value `status`
  vocabulary that became three plus two ledgers, and a repository layout
  with directories nobody has: the quarter of it that was still a live
  decision moves to
  [0081](docs/design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md),
  whose §5 says which record owns each of the rest. `0073` and `0078`
  were one subject split across a record and its amendment, and reading
  only the first told you to set four variables that were folded away a
  release ago; both become
  [0080](docs/design/0080-search-and-how-a-deployment-embeds.md). Four
  records become two, the corpus drops 278 lines, and no decision
  changes — except that 0073 §6's claim of a write-side media-type
  allowlist is retired, because there has been none since the bundle
  round-trip rule replaced it. **What an operator sees:** two error
  messages cite a new number — a model that cannot take file input now
  names design doc 0080 §5, and semantic search being off names 0080 §1.
  The retired records stay in the tree as tombstones pointing at their
  replacements, so every existing link still lands somewhere.

- **The BigQuery catalog example answers a deployment that grows its
  catalog on demand.** Ownership of a projected entry is now keyed to the
  `Ochakai-Producer` stamp rather than to the account that ran the job, so
  any run recognizes every other run's entries while a person's edit still
  takes an entry out of the projection. A table that disappears between the
  listing and the read is recorded as `missing` — an outcome the
  conservation check expects, not a failed night. `OCHAKAI_ID_TOKEN` lets a
  person run the job by hand, because user ADC cannot mint an ID token. And
  each dataset gets its own entry at `<prefix>/<project>/<dataset>`, written
  only by a run that enumerated the whole dataset, so its table list never
  recites tables the run did not see. The bundle ships in the release
  archives ([examples/bigquery-catalog](examples/bigquery-catalog)).

### Fixed

- **A description keeps the line breaks it was stored with in the web UI.**
  OKF stores a multi-line description as a `description: |-` block and the
  round trip keeps every line, but the page put that text into an element
  as escaped plain text, where HTML collapses newlines into spaces — so a
  description written as a heading and a paragraph arrived on screen as one
  run-on line, hash marks and all. Nothing was ever lost in the store; it
  went missing only on the page a person reads. Every place a description
  is shown now renders it through the same markdown renderer the body goes
  through: the Overview tab, the search and browse cards, and the review
  queue. References written in a description are not drawn as concept
  links, because the server derives edges from the body rather than from a
  description (design doc 0024) and drawing one would claim an edge that
  does not exist; an `http(s)` target is a link as it is anywhere else.

- **The web UI's type dropdown does something again.** It had been inert
  since `0.16.0`: reseeding was gated on the form's dirty flag, and a
  `<select>` raises `input` before its own `change`, so the flag was always
  up by the time the handler read it — picking a type silently did nothing,
  on the one control that tells a writer an Attested Computation needs a
  `runtime` before the server's 400 does. The gate now asks the document
  whether it is still the one that was seeded. Choosing a type in a document
  somebody has already written in edits the type line in place instead of
  doing nothing, carrying the keys that type is refused without, and leaving
  keys the writer already named alone. The label reads "Type" rather than
  "Start from", because it now edits the document in front of you.

## [0.19.1] - 2026-08-06

### Fixed

- **The CLI can reach a public deployment without a Google account.**
  Against any `https://` URL it used to resolve a Google ID token before
  sending anything and abort when it found none — so `ochakai use
  https://demo.example` saved fine and the next command died with "no
  Google credentials", on the one posture that reads no identity and
  never answers 401
  ([0066](docs/design/0066-four-postures-one-word.md) §3). The client no
  longer predicts the server's answer: holding no credentials it sends
  none, and a deployment that wanted an identity says so with a 401 —
  which is where the `gcloud auth login` advice now appears, carrying the
  reason the request went out bare. Nothing changes for a caller who has
  credentials, and `ochakai ui` and `ochakai mcp-stdio` reach a public
  demo by the same path. No new flag, variable or command
  ([docs/guides/operating.md](docs/guides/operating.md#public-demo)).

## [0.19.0] - 2026-08-05

### Added

- **`examples/bigquery-catalog`** gained a day-one procedure, a sync job,
  and the first attester in the repository that verifies a run *after* it
  happened — OKF's kinetic half, which no other example demonstrated.
  Docs now point at it from the three pages a new user actually reads —
  the quick start, the Cloud Run guide, and the manual's index — where
  before the only mention was a parenthetical inside a refusal table.
- Release archives now carry `examples/` alongside `LICENSE` and
  `README.md`; the demo and catalog bundles the top-level README tells a
  reader to import are reachable without cloning the repository.

### Fixed

- **BREAKING (deploy/terraform)** — the module's attachment-era names are
  retired to match the wire's "file"
  ([0064](docs/design/0064-rest-stops-at-api-v1.md) §6, which reached
  REST/MCP but never this corner): `enable_gcs_attachments` →
  `enable_gcs_files`, the `attachments_bucket` output → `files_bucket`,
  and the `google_storage_bucket.attachments` resource → `.files`. An
  operator applying this module after upgrading gets a rename Terraform
  wants to apply as a destroy-and-recreate.
- The web UI's markdown renderer had no notion of GFM pipe tables; one in
  a document body rendered as a paragraph with the pipes and dashes shown
  literally. `md()` now parses tables directly, including alignment and
  escaped pipes.
- `get_context`'s hint taught `search statuses=["rejected"]`, a spelling
  the status vocabulary retired for `rejected=true` back in
  [0043](docs/design/0043-document-first.md) §§3.2-3.3; an agent that
  followed it got a 400 instead of the rejection ledger. The hint is
  fixed, and the two guards that missed the drift now catch it.

## [0.18.0] - 2026-08-04

### Added

- **The contract declares the authentication it always required.**
  `api/openapi.yaml` gained `components.securitySchemes.GoogleIDToken`
  (`http` / `bearer` / JWT) and a top-level `security:` listing that
  scheme and the empty requirement `{}`. Nothing about the server
  changed: authentication is Cloud Run IAM and has been since
  [0003](docs/design/0003-gcp-only.md), the container never verifies a
  signature itself, and the empty requirement is `OCHAKAI_MODE=public`
  and `dev`, where nothing is checked and every caller is
  `human:anonymous` ([0066](docs/design/0066-four-postures-one-word.md)
  §1). What changed is that the contract said none of it, so a generated
  client had no way to know a token was needed. **This ships inside the
  freeze** — the three lines are in `api/openapi.frozen.txt` — which is
  why it had to land now rather than after. **What a client does:** a
  regenerated client now takes a bearer token; pass the Google-signed
  identity token audience-bound to the service URL that Cloud Run IAM
  already demanded (`gcloud auth print-identity-token --audiences=$OCHAKAI_URL`).
  A deployment on `public` or `dev` passes none, as before.
- **[docs/guides/rest-integration.md](docs/guides/rest-integration.md)**
  — a manual page (Japanese) for the application embedding ochakai's
  REST API in its own web service: minting the identity token, carrying
  an end user's identity with the delegation headers and the IAM grant
  that permits it, declaring who wrote with `Ochakai-Producer`,
  concurrent writes with `ETag`/`If-Match`, and running against a local
  instance. C6 is "a small REST API you can embed", and until now the
  only instructions were the contract itself. The `GoogleIDToken`
  scheme's own description links to it.
- **The shipped Claude Code hooks close the loop's third move.**
  `report_outcome` existed only as prose in
  `examples/claude-code/CLAUDE.md` — recall and write-back had hooks
  that fire every time, so `sort=failed` (the evidence-based
  re-verification feed, [0069](docs/design/0069-the-loop-and-what-measures-it.md))
  stayed at zero however stale the knowledge actually was. The Stop hook
  now nudges the agent to report what worked and what did not, and the
  recall hook records the concept ids it surfaced in
  `${TMPDIR:-/tmp}/ochakai-recalled-$session` so the nudge can name
  them (issue [#378](https://github.com/na0fu3y/ochakai/issues/378)).
  **Anyone running the shipped hooks sees a new end-of-session prompt
  and a new temporary file**; both are `examples/`, so nothing changes
  for a deployment that does not use them.

### Fixed

- **A refused concept no longer takes its files down with it.** When the
  server declines a document as a concept (an Attested Computation with
  no `runtime`, which the write path refuses by design), `ochakai import`
  also dropped every file that document's body pointed at — one refusal
  cost every object the concept named, though a file is an object at its
  own path and attribution is derived rather than stored. Those files are
  written now, reported as belonging to no concept. The document itself
  is still not stored: the skip line now names the path, the reason, and
  the fact that the bundle you imported from still holds it. Keeping its
  bytes instead would have meant a new rule on
  `PUT /api/v1/bundle/{path}`, which freezes permanently — design doc
  [0079](docs/design/0079-taking-the-document.md) §1 says why that trade
  was declined.
- **BREAKING (stored shape)** — `ochakai put` and `ochakai import` sent a
  canonical rendering built from the parsed fields instead of the
  document they were handed, so a producer's comments, key order, scalar
  style and everything the family readers had no field for were dropped
  on the way in. They now send the document's own bytes. The stored form
  is therefore different from what the same file produced before, and a
  CLI write and a REST write of the same file finally store the same
  thing. An operator re-importing an old bundle gets the bundle's bytes
  where they previously got ochakai's rendering, which moves the ETag of
  every concept whose document was not already canonical; nothing else
  has to be done about it. Design doc
  [0079](docs/design/0079-taking-the-document.md) §5.
- **BREAKING (stored shape)** — a document is no longer refused for a
  non-string scalar under a key OKF gives no YAML type: `title: 2026`,
  `description: 3.14`, `status: 3` and `tags: [1, 2]` are read as the
  text their producer wrote, with a note, rather than failing the parse.
  On import that failure was silent — the file was demoted to a loose
  markdown object with nothing in the notes or the skip list, so it kept
  its bytes and stopped being a concept. Such a document now imports as a
  concept: it appears in search, listings and trust where it did not
  before, and a `--strict` sync that used to pass now reports the notes.
  Only SPEC §11's own conditions still refuse a document — unparseable
  frontmatter and a missing or empty `type`. Design doc
  [0079](docs/design/0079-taking-the-document.md) §3.
- `executor` no longer requires a `receipt`. SPEC §10.2 marks only
  `runtime` REQUIRED and describes `receipt` with no requirement word;
  §11 forbids rejecting a concept for a missing optional field. ochakai
  dropped the whole `executor` and refused the write, in a note and an
  error that both cited §10.2 for a rule §10.2 does not state. A
  `resource` is still required — an executor naming nothing to run says
  nothing — and a document that wrote no `receipt` no longer gets one
  written back. Design doc
  [0079](docs/design/0079-taking-the-document.md) §2.
- A hand-written `index.md` or `log.md` was discarded on import without a
  word. ochakai still regenerates both from the bundle (SPEC §8 and §9
  make them derived), but now says so in a note — which means
  `ochakai export | ochakai import --strict` of ochakai's own bundle
  fails, one note per directory. That is deliberate: the alternative is a
  rule that only stays quiet about files ochakai wrote, which is no
  guarantee for anybody else's bundle. Design doc
  [0079](docs/design/0079-taking-the-document.md) §4.
- The MCP `write`/`propose` document schema described `sources` without
  SPEC §5.1's per-entry `usage_window` override, which the parser and the
  renderer both support, and described `executor` as always carrying a
  `receipt`.
- An entry with no actor exported `created_by: ':'` — a value OKF's actor
  convention (SPEC §7) has no form for. Unreachable for a stored row
  since migration `0022_actor_process.sql`; entries composed in memory
  still reached the renderer.

- **Security** — the web UI's delegation-header filter (`stripDelegation`)
  deleted only the current `Ochakai-On-Behalf-Of` spelling design doc
  [0064](docs/design/0064-rest-stops-at-api-v1.md) introduced, not the
  retired `X-Ochakai-On-Behalf-Of` it replaced. The web UI is a separate
  Cloud Run service from the API (design doc
  [0032](docs/design/0032-webui-iap-identity.md)) and the two can be
  upgraded one at a time: during that window a *new* web UI in front of
  an API that has not yet adopted 0064 would forward the retired
  spelling straight through, and the old API would still honor it —
  letting any IAP-authorized browser attribute a write to whichever
  identity it named. Any build carrying this fix now strips both
  spellings (issue [#410](https://github.com/na0fu3y/ochakai/issues/410)).
  This does **not** close the reverse case — an old web UI, without this
  fix, in front of a new API — since an old binary cannot run code it
  does not contain. The only mitigation there is upgrade order: upgrade
  the web UI first, or both together, never the API alone while the web
  UI lags. See [Upgrades](docs/guides/operating.md#upgrades) (issue
  [#418](https://github.com/na0fu3y/ochakai/issues/418)).
- **Security** — the Terraform module made that order impossible: the
  webui's `OCHAKAI_URL` reads the server's `uri`, so a single
  `terraform apply` bumping `image_tag` always settled the API first —
  the one order #418 says never to use. `webui_image_tag` now lets an
  operator stage the two: apply it ahead of `image_tag` to land the web
  UI first, then apply again with `image_tag` caught up (issue
  [#426](https://github.com/na0fu3y/ochakai/issues/426)).
- The quick start's demo import needed the Go toolchain
  (`go run ./cmd/ochakai import examples/demo`) despite the README
  promising "Docker and nothing else locally", and there was no
  Docker-only path: `deploy/compose.yaml` mounted no volume the import
  could read from. It now mounts `examples/`, and the quick start runs
  `docker compose exec ochakai /ochakai import /examples/demo` instead.
- `OCHAKAI_MODE=dev` — what makes the compose stack usable with no
  authentication — was undiscoverable outside a code comment and one
  clause of the configuration reference. The quick start now names it.
- The committed `.mcp.json` pointed at the Cloud Run proxy's port
  (8787), so opening the repo in Claude Code after the quick start
  (port 8080) connected to nothing, with no error saying so. It now
  matches the compose default; a Cloud Run deployment uses the bridge
  or `ochakai ui` instead (docs/guides/mcp-clients.md), not this file.
- The operating guide's web UI section still showed delegated writes as
  `agent:ochakai-webui@…`, the actor-kind spelling design doc
  [0043](docs/design/0043-document-first.md) §3.8 retired for `process:`
  back in 0.15 — a caller that sent it now gets a 400. The retired-spelling
  guard (issue [#275](https://github.com/na0fu3y/ochakai/issues/275))
  checked addresses and OKF lifecycle values but not actor kinds, so three
  lines kept teaching a request that cannot succeed. Both are now
  guarded.
- **BREAKING** — `POST /api/v1/reembed`'s `cursor` is now opaque, versioned
  and base64url-encoded, the same shape a listing's `cursor` has had since
  design doc [0050](docs/design/0050-listings-page-rankings-do-not.md). It
  used to be the last concept id and attachment name joined with a raw NUL
  byte and handed back unencoded — a hazard through proxies and WAFs, not
  URL-safe on its own, and published as an example in `api/openapi.yaml`
  right after the same paragraph called it opaque. A cursor from a pass
  running when this deploys is invalid after the upgrade; a reembed pass
  is minutes long and restartable, so resuming means starting the pass
  over, not losing work.

- **BREAKING** — `Accept: application/gzip` on a concept's or a file's
  own bundle path answers 409 instead of an empty, valid `tar.gz` with a
  200 on it. Dots are legal inside a path segment, so an object's own
  address — `metrics/revenue.md` as much as a reserved name — normalized
  as a legal prefix nothing lives under, and the archive of nothing that
  produced was the same defect fixed one release ago for `index.md` and
  `log.md` (design doc 0046 §3.5): the refusal covered the two reserved
  names and not every other object. It matters because the archive is
  what `api/openapi.yaml` tells an operator to verify a backup by
  reading rather than by the status code, and this one read fine — valid
  gzip, valid tar, empty. A backup script pointed one path too deep
  succeeded forever. This covers an object's own address; a prefix that
  matches no object at all — the same typo one segment shallower —
  still answers 200 with an empty archive, indistinguishable from a
  directory that is legitimately empty. That is a deliberate scope
  limit, not an oversight.

- **BREAKING** — a REST error response used to leave the documented
  `{"error": "..."}` envelope in two ways. A request net/http's own
  routing turned away before a handler ran — an unmatched path, or a
  path matched by a different method — answered plain text instead of
  JSON (404 `not found`, 405 `method not allowed`; issue #365). And a
  500 echoed whatever the store or a driver said, which could carry SQL
  text or a connection string. Both now stay inside the envelope: 404
  and 405 are JSON, and a 500 is logged for an operator and answered as
  `{"error": "internal error"}`. A caller parsing a 500 body for a
  driver message now finds `internal error` and needs the server log
  for detail. `api/openapi.yaml` declares 405 alongside the 500 already
  on all eleven operations.

- Issue [#470](https://github.com/na0fu3y/ochakai/issues/470) audited
  the not-yet-tagged freeze (design doc
  [0064](docs/design/0064-rest-stops-at-api-v1.md)) against
  `api/openapi.yaml` and found three places the spec and the code
  disagreed, closed here as 0064 §14 (none of the three are breaking):
  - `?history`'s `limit` was one number in the spec (1000, default and
    max) and two in the code — 50/200 for a concept's own history read
    as JSON, 1000/1000 for log.md and every markdown rendering. The code
    was already right for a reason (a concept's JSON history carries the
    whole document at every revision); the spec now names both ceilings.
  - A concept's GET computed an ETag but never compared it against
    `If-None-Match`, even though both file branches on the same address
    already answered a match with 304 and the spec declared the header
    for the GET as a whole. A concept now answers 304 too.
  - A file `PUT`'s 200 and 201 responses have declared an `ETag` header
    since design doc 0064 (to match a concept `PUT`, which already sends
    one); the code never set it. It does now.

### Changed

- **BREAKING** — a file has one address, and the CLI stops having a second
  one for it (design doc
  [0077](docs/design/0077-one-address-for-a-file.md), issue
  [#470](https://github.com/na0fu3y/ochakai/issues/470)):

  ```
  ochakai attach <id> <file>                →  ochakai put <id>/<file> -f <file>
  ochakai attach <id> <f1> <f2>             →  a shell loop over ochakai put
  ochakai attach <id> <file> --name <name>  →  ochakai put <id>/<name> -f <file>
  ochakai detach <id> <name>                →  ochakai delete <id>/<name>
  ```

  Both commands named a file by `<concept id> <name>` rather than by the
  bundle path it lives at, in a word design doc
  [0075](docs/design/0075-the-bundle-is-the-address-space.md) §1 had
  already retired — and `PUT`/`DELETE` on a bundle path, which is what
  they sent, is what `ochakai put` and `ochakai delete` already send.
  **No capability is lost.** `ochakai put` writes either kind of object
  and the bytes decide which, exactly as they do on the wire: frontmatter
  carrying a `type` is a concept at `<id>.md`, anything else is a file at
  the path given. `ochakai delete` removes either kind, reading an
  argument with no filename extension as a concept id and one that
  carries its own extension as a file's path; the other address is tried
  only when the first holds nothing, so a dotted id and a file whose name
  has no extension both stay reachable.

  **The paste-me link moved, it did not go.** `ochakai put` prints the
  same relative markdown link on stderr that `attach` did — attribution
  is derived from the bodies that link a file (0075 §5), so a file
  nothing links is a file nobody finds. What folds away is convenience:
  several files per invocation is a shell loop, and `--name` has no place
  now that the path is the name (the spelling survives on `ochakai use`,
  so no flag is retired).

  The web UI's buttons follow the CLI's word — `Attach` is now
  `Add files` and `Detach` is `Remove` — which is what design doc 0064 §8
  asked for when it kept the two symmetric. **The wire is unchanged**:
  `PUT`/`DELETE /api/v1/bundle/{path}`, the `add_file`/`remove_file`
  revision verbs and the stored form are all untouched.

- The web UI says `concept` everywhere a reader meets a word for one —
  feed banners, truncation notes, empty states, tooltips and hint text
  still said `entry`, the word design doc
  [0057](docs/design/0057-concept-is-the-word-a-reader-meets.md) retired
  and the rest of the product stopped using releases ago. Identifiers,
  CSS classes and code comments keep their spellings, which is where
  0057 §3.2 drew the line.

- **BREAKING** — MCP goes from eight tools to six: `delete_concept` and
  `get_concept_usage` are gone (design doc
  [0076](docs/design/0076-two-tools-leave-mcp.md)). A tool schema is paid
  for out of the agent's context window on every session, so the tool
  count is a budget, and these two were doors nobody arrived through —
  neither the shipped hooks nor the shipped `CLAUDE.md` nor the
  BigQuery-catalog job ever called them. There is no deprecation window;
  MCP is explicitly unstable at 0.x
  ([docs/compatibility.md](docs/compatibility.md)), so a call to either
  name now fails with "no such tool". **The capability is not gone — it
  left this face only**:

  ```
  MCP
    delete_concept       →  DELETE /api/v1/bundle/{id}.md
                            ochakai delete <id>
                            web UI: ⋯ → Delete…
    get_concept_usage    →  GET /api/v1/usage/{id}
                            ochakai usage <id>
                            web UI: the concept's Usage tab
  ```

  An **agent** that called `delete_concept` should not call anything: if a
  concept is wrong, `report_outcome` failed puts it in the re-verification
  feed, and a better version is a `put_concept` draft at a different id.
  Deleting knowledge is a ruling, and this surface deliberately withholds
  even the reversible rulings — `POST /api/v1/review/{id}` (verify,
  reject) is not a tool either. An agent that called `get_concept_usage`
  needs nothing new: the trust tier and `verified_at` it acts on already
  ride on every search hit and every `get_concept`. **An operator or
  script** that read totals over MCP moves to `GET /api/v1/usage/{id}` or
  `ochakai usage`. Read-only deployments now withhold two write tools
  (`put_concept`, `report_outcome`) instead of three, and no MCP tool
  carries the `destructive` annotation any more.

- **BREAKING** — one variable says how a deployment embeds (design doc
  [0078](docs/design/0078-one-variable-says-how-it-embeds.md)):

  ```
  (unset)                                   →  (unset — embed, project discovered)
  OCHAKAI_EMBEDDINGS=on|off                 →  unchanged
  OCHAKAI_VERTEX_PROJECT=p                  ┐
  OCHAKAI_VERTEX_LOCATION=l                 ├→  OCHAKAI_EMBEDDINGS=projects/p/locations/l/publishers/google/models/m
  OCHAKAI_VERTEX_MODEL=m                    ┘
  OCHAKAI_EMBEDDING_DIM=…                   →  (gone: the width belongs to the model)
  ```

  **Set `OCHAKAI_EMBEDDINGS` before upgrading.** The four retired
  variables are ignored, so a deployment that named a model, a location, a
  project or a dimension silently falls back to the default
  (`gemini-embedding-001`, `us-central1`, the project it runs in, 768) —
  and **it will not look broken**. A changed width rebuilds the vector
  tables empty on the next start; an unchanged width with a changed model
  leaves the old rows in place but invisible, because search filters by
  model. Either way semantic search goes quiet, rankings quietly become
  lexical-only, and a base that used `gemini-embedding-2` loses image and
  PDF search with them. Nothing curated is lost — a vector is derived, and
  no `DROP TABLE` is ever needed — but the only cure is `ochakai reembed`,
  which pays Vertex AI to rewrite every vector in the base. Migration is
  one command:

  ```sh
  gcloud run services update ochakai \
    --update-env-vars=OCHAKAI_EMBEDDINGS=projects/$PROJECT_ID/locations/global/publishers/google/models/gemini-embedding-2 \
    --remove-env-vars=OCHAKAI_VERTEX_PROJECT,OCHAKAI_VERTEX_LOCATION,OCHAKAI_VERTEX_MODEL,OCHAKAI_EMBEDDING_DIM
  ```

  A deployment that set none of the four, or only `OCHAKAI_EMBEDDINGS`,
  has nothing to do. A spelling that is none of the three forms is a
  startup error naming them, as `OCHAKAI_MODE` has been since 0.17.0, and
  a model ochakai carries no vector width for is refused rather than
  embedded at a guessed width. The dimension is no longer settable at all:
  a vector is the product's derived value (design doc
  [0073](docs/design/0073-search-and-when-embeddings-apply.md) §2), 768 for
  both models, and unchanged for every deployment that never set it.
  Terraform's `vertex_model`, `vertex_location` and `embedding_dim` become
  one `embedding_model` taking the resource name.

- **BREAKING** — REST is frozen at `/api/v1` (design doc
  [0064](docs/design/0064-rest-stops-at-api-v1.md), issue
  [#379](https://github.com/na0fu3y/ochakai/issues/379)):
  [docs/compatibility.md](docs/compatibility.md) no longer lists REST
  among the unstable interfaces. This entry is the last batch of breaking
  changes that made the freeze possible:
  - An unrecognized query parameter is now a 400 naming it, on every REST
    operation. It used to be a silent no-op, which is what let a 0.16.1
    server ignore `dry_run` and write anyway; this closes the same shape
    of hole for anything added to the wire from here on. `fm.{key}` is
    the one family checked against the concept's own frontmatter rather
    than a fixed name list, and **only the two operations that filter by
    it take it** — `GET /api/v1/search` and `GET /api/v1/context`.
    Anywhere else, `?fm.anything=x` is an unknown parameter like any
    other and answers 400; it used to be ignored.
  - An unrecognized JSON body key on `POST /api/v1/move`,
    `POST /api/v1/review/{id}` and `POST /api/v1/usage/{id}` is now a 400
    naming it too, closing the same hole for the body: `{"rulling":
    "verified"}` used to be a 200 that did nothing recognizable, and
    `"notes"` for `"note"` used to drop the note silently (issue
    [#470](https://github.com/na0fu3y/ochakai/issues/470)).
  - **`If-Match: *` is now a 400.** RFC 9110 gives it a meaning ochakai
    does not implement — "only if a representation exists", the
    update-only mirror of the create-only `If-None-Match: *` — and it was
    being read as no precondition at all, so a caller sending it
    precisely to avoid creating anything **silently created**. If you
    send it, drop it; if you were relying on it to guard a write, you
    were not guarded. A 400 is also the only one of the three possible
    answers that can still change after the freeze, which is why it is
    not the RFC behaviour: nothing can depend on a request that always
    failed (§20.1).
  - **Three more preconditions that were accepted and dropped are now
    400s.** `If-None-Match` with a version on a PUT (RFC's meaning is
    "write only if it is *not* this version", which a knowledge store has
    no use for), `If-None-Match: *` on a GET (the opposite of the cache
    validator the header is otherwise used as), and **either header on a
    write or delete whose path holds a file** — the contract declares
    both on this operation because one address writes concepts and files,
    but the version they name is a concept's, and a file write had never
    read either one. In every case the request went through
    unconditioned, which is exactly what a caller sending a precondition
    is trying to prevent. `ochakai put` has refused the file case
    client-side all along; the server says so now too. Conditional file
    writes can still be implemented later: a 400 nobody can depend on is
    not a promise (§20.4).
  - `PUT /api/v1/bundle/{path}` **declares its 404**: `If-Match` naming a
    version where no concept lives has always answered 404, and the
    contract never said so. It stays 404 rather than the 412 RFC 9110
    prescribes, deliberately — 412 would conflate "it changed" with "it
    is gone", and those lead a caller to different next steps (§20.5).
  - **A repeated query key is now a 400**, unless the parameter is
    declared as an array — `type`, `status`, `trust`, `tag` and `prefix`
    are the five that legitimately repeat, and `?type=Metric&type=Policy`
    still means either. Everything else takes one value.
    `?limit=10&limit=50` used to be answered silently and in two
    directions at once: the first value won for most keys and the last
    one won for `fm.{key}` (§20.2).
  - **`change` is no longer a closed enum** in `Change` and `Revision`.
    The nine words are unchanged, but a client that validated against the
    list should now pass a value it does not know through as an event it
    has nothing special to render. The vocabulary has moved twice in
    three months (`unreject` → `withdraw`, `attach`/`detach` →
    `add_file`/`remove_file`), and a closed response enum would have made
    the tenth word impossible after the freeze rather than merely
    breaking (§20.3).
  - **`Accept` is read as a set of media type names, not as a ranking**,
    and the rule is now written down. Comparison is on the media type
    alone and case-insensitive, so `text/markdown; charset=utf-8` selects
    the document (it did before too) while `text/markdown-x` no longer
    does — substring matching used to answer a longer type that merely
    contained a token, and used to miss `TEXT/MARKDOWN`, which RFC 9110
    says is the same type. **`q` is not read, `q=0` included**:
    `application/gzip;q=0` still gets the archive. An address has one
    default and at most two types that select another, so there is
    nothing to rank, and a caller who wants the default asks for it by
    not naming the token (§22).
  - **Eight `operationId`s are renamed.** Five drop the retired word
    `knowledge` — `searchKnowledge`, `moveKnowledge`, `reviewKnowledge`,
    `getKnowledgeUsage` and `reembedKnowledge` become `searchConcepts`,
    `moveConcept`, `reviewConcept`, `getConceptUsage` and
    `reembedConcepts`. The three bundle operations name their address
    instead of a payload they mostly do not carry: `getBundleFile`,
    `putBundleFile` and `deleteBundleFile` become `getBundlePath`,
    `putBundlePath` and `deleteBundlePath`, since `File` is this wire's
    schema for a bundle file that is *not* a concept. No HTTP byte moves;
    a generated client's method names do (§21).
  - `GET /api/v1/bundle/{path}`'s `history`, `limit` and `files` query
    parameters are now a 400 naming the mode when sent outside the mode
    that reads them — `limit` outside `?history` and a directory's
    `log.md`, `files` outside `Accept: application/gzip`, and `?history`
    together with `Accept: application/gzip` (a different object, not a
    representation choice, so no precedence rule decides it). All three
    used to be silently ignored outside their mode (issue #470).
  - `X-Ochakai-On-Behalf-Of` and `X-Ochakai-Producer` lose their `X-`
    prefix — `Ochakai-On-Behalf-Of` and `Ochakai-Producer`, matching the
    five response headers, which never carried one. No dual-accept
    window; send the new spelling. **The failure mode is silence, not a
    400**: a caller that keeps sending the retired name gets no error —
    the header is simply not read, and every write it makes is attributed
    to the caller's own identity instead of the end user it meant to
    name. An embedding host with per-user provenance should check its
    writes are still landing as the right user after this upgrade, not
    only that requests still succeed.
  - `Accept: application/json` on a file's own bundle path now answers
    its metadata (name, media_type, size, sha256, path, created_by,
    created_at) instead of being ignored in favor of the bytes — the only
    way to read a file's `sha256` without downloading it.
  - `attachments`/`Attachment` are renamed to `files`/`File` everywhere
    they named the wire concept: the `Attachment` OpenAPI schema and the
    JSON key on `View`, `SearchHit` and `ReembedResult`; the archive
    address's `?attachments=` query parameter (now `?files=`); the MCP
    tool `get_attachment` (now `get_file`) and its result key
    (`attachment` → `file`). The CLI followed past the wire too:
    `ochakai export --no-attachments` is now `--no-files`.
  - `ochakai import`'s summary line, and its `--dry-run` twin, traded
    `%d attachments, %d files` for `%d attributed, %d loose`. Not a
    rename of the line above — `attributed` counts files a concept's body
    claims and `loose` counts files that belong to no concept, the same
    two counts as before under words this PR had not taught anywhere
    else. A script parsing the line by position still works; one
    matching the old words breaks silently.
  - The JSON key `entries` is renamed to `concepts` on `GET /api/v1/stats`,
    the bundle listing, and `GET /api/v1/context` (the MCP tool
    `get_context` carries the same field). Design doc
    [0057](docs/design/0057-concept-is-the-word-a-reader-meets.md) §3.2
    had exempted `entries` as naming an array rather than the unit, but
    that reasoning never reached `stats.entries` (a struct of counts, not
    an array) and left the bundle listing's `entries` unrenamed beside
    the `files` key this same PR renamed for the identical reason — issue
    [#411](https://github.com/na0fu3y/ochakai/issues/411) found the
    inconsistency; design doc 0064 §7 revokes the exemption.
  - `attach`/`detach`, the last retired-word holdouts in the `change`
    vocabulary (`Change.change`, `Revision.change`), are renamed to
    `add_file`/`remove_file` — design doc 0064 §6 renamed every other wire
    spelling of "attachment" and missed the change-verbs; issue
    [#470](https://github.com/na0fu3y/ochakai/issues/470) found the gap.
    Existing `knowledge_revision` rows are rewritten by a migration. The
    CLI commands of the same name were split out to their own issue and
    are folded in the entry below.
  - `GET /api/v1/context`'s `truncated` count is dropped — it always
    equaled `len(outline)`, so the array's own length carries the same
    signal (design doc 0064 §9, issue #470). The bundle listing's
    `truncated` flag (a different, non-redundant signal: whether the
    concept or file list hit the 1000-per-level cap) is unchanged.
  - The bundle listing's file rows say `created_at` instead of
    `updated_at` — a file is content-addressed and immutable, and the
    listing's query now reads the column that actually stays fixed across
    an overwrite, matching what `File.created_at` already says elsewhere
    on the wire (design doc 0064 §9, issue #470).
  - `POST /api/v1/reembed`'s `embedded` count is renamed to `concepts`,
    matching its sibling `files` — it counts concepts, not the act of
    embedding (design doc 0064 §9, issue #470).
  - A failed `If-None-Match: *` (the id was already taken) is now 412,
    not 409 — RFC 9110's status for a failed precondition. 409 stays the
    answer for a conflict with no precondition behind it, such as `move`
    onto an occupied id.
  - `DELETE` on a concept now honors `If-Match`, the same optimistic-
    concurrency precondition `PUT` already took (design doc
    [0030](docs/design/0030-optimistic-locking.md)) — a stale delete now
    fails with a 412 instead of silently removing a version the caller
    never read. Not extended to `?purge=true`, which stays unconditional.
  - A file `PUT` answers 201 when it creates the object and 200 when it
    replaces one, matching what a concept `PUT` already did — it used to
    always answer 200.
  - `ruling: withdrawn` against a live concept with nothing to withdraw
    is now 409, the same shape purge-on-a-live-concept already answers,
    instead of 404 — a missing concept is still a genuine 404.
  - Out-of-range `limit` (search, context, bundle log/history, usage
    revisions, reembed) and `days` (stats) are now a 400 naming the valid
    range, everywhere. Most `limit`s used to clamp silently to a default
    instead; `days` was already the strict one, and the rest now match
    it.
  - **BREAKING** — `title` is optional on the wire, everywhere. OKF SPEC
    §4.1 gives it no default — it is RECOMMENDED, and "If omitted,
    consumers MAY derive a title from the filename" — so `Summary.title`
    being `required` and resolved asserted a value the document never
    declared, and a client could not tell a title from a filename. The
    key is now absent when the document declared none, on `Summary` (and
    so on every search hit and every `View`, which inherit it),
    `ContextRank` and `ContextOutline`; the bundle listing and
    `Change.title` already behaved this way and only the prose changed.
    **What a client does:** treat `title` as absent and derive a display
    name from the id's last segment — `id.split("/").pop()` — the way
    the web UI and the CLI already did. `status` stays resolved, because
    SPEC §5.4 does give *it* a normative default (design doc 0064 §15,
    [0074](docs/design/0074-the-document-and-the-vocabulary-that-asks-it.md)
    §1).
  - **BREAKING** — `observed.generated.at` reports the instant the
    *content* last changed, not the instant the row was last written.
    OKF SPEC §5.2 defines `generated.at` as the content's "last
    meaningful change"; the exported document has always rendered that
    (`ContentChangedAt`) while the JSON rendered `updated_at`, so a write
    that only reformatted a document moved one and not the other and the
    two renderings of the same concept disagreed inside one response.
    **What a client does:** if it was reading `observed.generated.at` as
    "when was this row last written", read `summary.updated_at` instead;
    if it was reading it as OKF's `generated.at`, nothing changes except
    that it is now correct (design doc 0064 §17).
  - **BREAKING** — a `type` may contain `/`. OKF SPEC §4.1 says type
    values are not registered centrally and a consumer "MUST tolerate
    unknown types gracefully", and §11 forbids rejecting a bundle over an
    unknown one; ochakai answered 400 to `type: acme/Table` and, on
    import, silently demoted such a document to a loose bundle file — it
    kept its bytes but stopped being a concept, with no note and nothing
    in `skipped`. The ban was ochakai's own rule with no SPEC behind it.
    The `Type` schema's pattern widens to `^[^\r\n]{1,128}$` and
    `domain.ValidType` with it; the 128-byte cap stays, as ordinary
    defensive practice rather than anything SPEC derives. **What a client
    does:** a client that validated types against the old pattern must
    widen it — a response can now carry a type with a `/` in it (design
    doc 0064 §18).
  - `PUT /api/v1/bundle/{path}` declares a `requestBody` at last
    (`required: true`; `text/markdown` for a concept,
    `application/octet-stream` for a file). It never had one, so no
    generator could produce a working write client from the contract.
    It survived because the contract test skipped *every* PUT under the
    bundle address, concepts included — written for opaque file bytes,
    it took the whole write face out from under the validator. The skip
    is now decided the way the handler decides, by the body, so a
    concept PUT is checked like any other request; a client that sent no
    `Content-Type` on a concept write is off-contract, though the server
    stays lenient (design doc 0064 §16).
  - The contract now declares what the server already sent: an
    `Ochakai-Read-Only` header on the two bundle 412s and the review
    409; a 400 on `GET /api/v1/usage/{id}` (the one operation whose
    response list was never updated when unknown query keys became a
    400) and a 413 on `POST /api/v1/move`, `POST /api/v1/review/{id}`
    and `POST /api/v1/usage/{id}`; and the `Cache-Control`,
    `X-Content-Type-Options: nosniff` and `Content-Security-Policy:
    sandbox` response headers, plus `ETag` on the 304. All additive —
    nothing a client holds breaks. The three headers move
    [docs/surface.md](docs/surface.md)'s `HEADER` count 10 → 13: the
    section derives it from the spec rather than from the code, so a
    header the server set and the contract omitted was surface nobody
    counted (design doc 0064 §19).
  - `api/openapi.yaml`'s `Trust` schema now says what design doc
    [0009](docs/design/0009-provenance-portability.md) decided: the tier
    is derived from *this instance's* verification ledger, and a
    document's own `verified` key is kept under `received:` and never
    scored. A reader coming from OKF SPEC §5.3 would have expected the
    document's own key to be read. Prose only.
  - No version or capability-discovery signal ships, and CORS stays
    deliberately absent — both are decided in 0064 rather than left
    open.
  - **BREAKING** — `status` and `trust` values outside their vocabularies
    are a 400 naming it, not a 200 with no hits. `?status=Draft` (case
    matters) and `?trust=verified` (a status word, not a tier) used to
    read as "the base holds nothing" — the same silent no-op unknown
    query keys used to be. Every surface goes through the one check, so
    MCP and the CLI close with REST. **What a client does:** send
    `draft|stable|deprecated` and
    `unverified|machine-confirmed|human-reviewed`, exactly (design doc
    0064 §23.3).
  - **BREAKING** — `fm.id` and `fm.status_note` are no longer askable
    (400). Both are ochakai's own envelope keys — OKF SPEC v0.2 defines
    no top-level `id` and no `status_note` — and the `fm.` vocabulary is
    OKF's; ochakai's own extension keys are refused the same way anybody
    else's are. Storage and round trip are untouched. **What a client
    does:** the id question is answered by the address and `prefix=`;
    a `status_note` exact-match had no answerable question behind it
    (design doc 0064 §23.5).
  - **BREAKING** — `POST /api/v1/reembed` refuses a request body (400):
    it never read one, so `{"limit": 10}` in the body silently ran a
    default-sized pass. `limit` and `cursor` are query parameters. And a
    negative `budget` on `GET /api/v1/context` is a 400 instead of
    silently meaning "no cap" — 0 or omission is that spelling (design
    doc 0064 §23.4).
  - A dry run's response now carries the values the write would stamp:
    `observed.generated.by` (an empty actor — a value the contract's own
    enum forbids — used to reach the wire on every update plan),
    `content_hash` (deterministic, so the plan's hash equals the later
    write's ETag), `summary.updated_at`/`created_at`, and a planned
    file's `created_by`/`created_at`. And a file overwrite's 200 now
    reports the row's original creator and creation time instead of the
    current caller and now, so the PUT response agrees with the next GET
    (design doc 0064 §§23.1-23.2).
  - Always-present response fields are now declared `required` — the
    error envelope's `error`, search's `hits`, context's
    `hits`/`concepts`, `Usage`'s four counters, `File`'s seven fields,
    all of `Stats`, `Actor`'s `kind`/`name`, and the rest that match the
    structs. Additive for readers; a generated client stops holding
    all-optional types. This was the last day it could happen: adding
    `required` moves the frozen fingerprint (design doc 0064 §23.6).
  - `Ochakai-Read-Only` now stands on auth-layer 400/403 responses too,
    which "on every /api/v1 response" always promised. Contract prose
    that said untrue things is corrected — the phantom rule that a
    `verified` key decides the `stable` mapping (no such rule exists),
    `summary.updated_at` described as the same instant as
    `observed.generated.at` (it is the row's time; the content's is
    `generated.at`), and the `change` vocabulary's `add_file` /
    `remove_file` marked as historical spellings: since a file became an
    object at its own path, a new file event is that path's own
    `create` or `delete` (design doc 0064 §§23.7, 8).

- **Most of the manual is now Japanese** — the largest user-visible
  change in this release for a reader who does not read Japanese. C8
  (issue [#398](https://github.com/na0fu3y/ochakai/issues/398)) says
  ochakai should be one of the best choices available to a
  Japanese-speaking user, and a manual they have to read in English is
  the first place that fails. Ten pages were translated this cycle:
  `docs/README.md`, `docs/loop.md`, `docs/configuration.md`,
  `docs/architecture.md`, `docs/guides/operating.md` (#446),
  `docs/guides/troubleshooting.md` (#440),
  `docs/guides/mcp-clients.md`, `docs/guides/golden-query-canary.md`,
  `deploy/cloudrun/README.md` (#464) and `deploy/terraform/README.md`.
  **Every heading anchor is kept**, so a bookmark or an inbound link
  still lands where it did — `operating.md`'s eight
  (`#backup-and-restore`, `#capacity`, `#supply-chain`, `#hardening`,
  `#guardrails-against-misconfiguration`, `#the-team-web-ui`,
  `#public-demo`, `#upgrades`) included — and a link test now checks
  that manual links and their fragments resolve. Every remaining
  English page that links to a translated one — `README.md`,
  `ROADMAP.md`, `docs/compatibility.md`, `docs/faq.md` — is marked
  `(Japanese)`. The English pages that remain are the ones an outside
  reader meets first: the repository README, `CONTRIBUTING.md`,
  `docs/compatibility.md`, `docs/faq.md`, `ROADMAP.md` and the wire
  contract's own prose. One capacity claim ("20 files per concept") was
  already stale before translation — the cap was removed in `[0.16.0]`
  below — and is left in English pending issue
  [#466](https://github.com/na0fu3y/ochakai/issues/466) rather than
  translated as written (CONTRIBUTING.md's "check the English before
  translating it").

- **The manual lost two pages and gained one.** `docs/knowledge.md`
  folded into `docs/architecture.md`, whose own evaluation half then
  moved into `docs/README.md`; `docs/images/README.md` went with the
  file-address fold. `docs/guides/rest-integration.md` is new (see
  **Added**). [docs/surface.md](docs/surface.md)'s `DOC` count moves
  25 → 23 and its `DOC-LINES` ceiling 5,100 → 5,300 — translation adds
  lines, and the ceiling moves in hundreds so that only a page that
  actually grew the manual rewrites that line. **A link to either
  removed page 404s**; both were reference pages with no address a
  client sends.

## [0.17.0] - 2026-07-31

### Added

- **BREAKING** — a listing is not a search, so it is not the same command
  (design doc [0062](docs/design/0062-a-listing-is-not-a-search.md)):

  ```
  ochakai search --sort usage --status draft  →  ochakai list usage --status draft
  ochakai search --sort verified_at           →  ochakai list verified_at
  ochakai search --sort failed                →  ochakai list failed
  ochakai search --sort stale_after           →  ochakai list stale_after
  ochakai search --source <uri>               →  ochakai list --source <uri>
  ochakai search --links-to <id>              →  ochakai list --links-to <id>
  ```

  `ochakai search` now requires a query and takes no `--cursor`; a ranking
  has no page two. `ochakai list` pages with `--cursor` and defaults
  `--limit` to 100 (max 1000), where a search defaults to 10 (max 50) —
  the two defaults that used to hide behind one flag. The nine filters
  (`--type`, `--status`, `--tag`, `--trust`, `--prefix`, `--source`,
  `--links-to`, `--fm`, `--rejected`) work the same on both.

  `ochakai stats` prints the new spelling on its queue lines, so a script
  that greps the third field gets `ochakai list …`.

  **The wire is unchanged.** `GET /api/v1/search?sort=`, MCP's
  `search_concepts` and the web UI are untouched, and so is the
  vocabulary: `usage`, `verified_at`, `failed` and `stale_after` are the
  same four words, moved from a flag's values to a command's argument.

- **BREAKING** — one variable says what a deployment is (design doc
  [0060](docs/design/0060-one-word-for-the-posture.md)):

  ```
  OCHAKAI_READ_ONLY=true         →  OCHAKAI_MODE=read-only
  OCHAKAI_PUBLIC_READ_ONLY=true  →  OCHAKAI_MODE=public
  OCHAKAI_INSECURE_DEV=true      →  OCHAKAI_MODE=dev
  (unset)                        →  (unset — the ordinary posture)
  ```

  **Set `OCHAKAI_MODE` before upgrading.** The old variables are ignored,
  so a deployment that set only `OCHAKAI_READ_ONLY=true` becomes writable,
  and one that set `OCHAKAI_PUBLIC_READ_ONLY=true` starts demanding an
  identity as well. Migration is one command:

  ```sh
  gcloud run services update ochakai \
    --update-env-vars=OCHAKAI_MODE=read-only \
    --remove-env-vars=OCHAKAI_READ_ONLY,OCHAKAI_PUBLIC_READ_ONLY,OCHAKAI_INSECURE_DEV
  ```

  Nothing design docs 0040 and 0042 decided has changed — only how it is
  written down. Three booleans could spell eight combinations, of which
  four mean anything, and the gap was covered by three rules: two that
  silently corrected what an operator wrote (`public` forcing read-only
  on, and forcing miss recording off) and one that refused to start
  (`public` together with insecure dev). What actually exists is a
  two-by-two — is the caller identified, and may anyone write —
  and **the rules are not removed so much as left without a subject**,
  because the combinations can no longer be written down. A spelling that
  is none of the four is a startup error rather than a guess.

  Environment variables go 16 → 14, and [docs/surface.md](docs/surface.md)'s
  ceiling with them. The Terraform module's `read_only` and
  `public_read_only` variables are unchanged; they set the new spelling.

- **BREAKING** — a review queue is now named after the listing that shows
  it (design doc
  [0059](docs/design/0059-a-queue-is-named-by-its-listing.md)):

  ```
  GET /api/v1/stats  queues.reported_wrong  →  queues.failed
                     queues.past_expiry     →  queues.stale_after
                     queues.drafts          →  unchanged
  ```

  `ochakai stats` was printing its own evidence — each queue line says how
  many are waiting and what to type to see them, and on two of the three
  the two halves disagreed (`reported_wrong 1  ochakai search --sort
  failed`). The sort names win because they **reuse words a reader has
  already met**: `failed` is the outcome a caller reports through
  `report_outcome`, and `stale_after` is OKF SPEC §5's own frontmatter
  key, which is where the counted date comes from. The queue spellings
  existed for nothing else. `drafts` keeps a name of its own because it is
  not a single sort (`sort=usage` plus `status=draft`).

  A CI job reading `jq .queues.reported_wrong` needs one edit; one reading
  `jq '.queues | add'` needs none, and `ochakai stats --exit-code` reads
  no key name at all. The counted sets, the counting and the number of
  queues are unchanged.

  Vocabulary goes 38 → 36, and [docs/surface.md](docs/surface.md)'s
  ceiling with it. The counting learned the reverse of its own rule: one
  spelling meaning two things in two families is two words to learn, and
  **one thing wearing two names is one** — so a queue keyed by the sort
  that lists it is no longer counted twice, and a queue key that stops
  being a sort name shows up as a word again.

- **BREAKING** — two filters nobody arrived through come off (design doc
  [0058](docs/design/0058-filters-nobody-arrived-through.md)):

  ```
  GET /api/v1/context?min_score=   →  gone; use budget=
  ochakai context --min-score      →  gone; use --budget
  MCP search_concepts / get_context "fm"  →  gone (REST ?fm.<key>= and
                                             ochakai --fm are unchanged)
  ```

  `min_score` had no working consumer. A score floor is only meaningful
  once calibrated against a corpus in a known search mode, and ochakai's
  scores are neither comparable nor calibrated — matched-fragment weight
  plus boosts under lexical, RRF (~0.02 scale) under hybrid — which is
  why design doc 0051 §3.1 refused a score threshold when it defined a
  search miss. MCP never offered it, the web UI never used it, and the
  one shipped caller, the `ochakai-recall` hook, passed a default of `0`
  (off) beside a README paragraph saying there is no universal default.
  **Bounding a response is `budget`'s job**, and it asks nobody to
  calibrate anything: what does not fit comes back named rather than
  dropped. `OCHAKAI_RECALL_MIN_SCORE` is gone from the hook.

  `fm` comes off **MCP only** — REST and the CLI keep it, so 0047's
  vocabulary and its derivation from the OKF envelope keys are
  unchanged. What comes off is its 944-character schema description,
  which sat on two tools and made up **14% of the schema text (1,888 of
  13,099 characters) every connecting agent pays for**, for a filter
  nothing shipped went through. The tool count is still 8: what dropped
  is a cost the tool count never showed.

  Query parameters go 19 → 18, and [docs/surface.md](docs/surface.md)'s
  ceiling with them.

- **BREAKING** — two CLI commands fold into the ones that already
  answered the same question, and taking back a rejection gets one
  spelling everywhere (design doc
  [0056](docs/design/0056-one-question-one-command.md)):

  ```
  ochakai backlinks <id>     →  ochakai search --links-to <id>
  ochakai queues             →  ochakai stats  (--exit-code moves with it)
  ochakai reject --lift      →  ochakai reject --withdraw
  revision change "unreject" →  "withdraw"
  ```

  Both commands were second addresses for a face the wire had already
  folded away — `/api/v1/backlinks/{id}` into `links_to=` (design doc
  0046 §3.5) and `/api/v1/queues` into the `queues` key of `stats`
  (0049 §3.1) — and each was quietly costing something. `ochakai
  backlinks --limit` documented the defaults of the endpoint that no
  longer existed (20/100, where the search listing it actually calls
  gives 100/1000), and `queues` printed the same three numbers under
  different keys than `stats` did. **No capability is lost.** The queue
  lines of `ochakai stats` now carry the command that lists each queue,
  under the scope you asked with, and `ochakai stats --exit-code` still
  exits 2 while any of the three is holding something and 1 on an error.
  `--links-to` with no query lists in address order, pages with
  `--cursor`, and combines with every other filter.

  `withdraw` is the spelling the wire already chose (`ruling:
  "withdrawn"`, 0055 §3.2). The CLI flag, the web UI's button and the
  revision log's `change` value said `lift`, `Lift rejection` and
  `unreject` — four words for one act. Migration `0034` rewrites the
  stored `change` values; who withdrew what and when is untouched. A
  client that branches on a revision's `change` rewrites one word.

  `ochakai verify` and `ochakai reject` stay two commands: what folds is
  two commands with one capability, not two different acts. CLI commands
  go 27 → 25, and [docs/surface.md](docs/surface.md)'s ceiling with them.

- **BREAKING** — the word for the unit of knowledge, `concept`, now runs
  through **everything a reader meets** rather than the MCP tool names
  alone (design doc
  [0057](docs/design/0057-concept-is-the-word-a-reader-meets.md), which
  completes [0054](docs/design/0054-concept-is-the-okf-word.md)): the
  README and docs, the examples and deploy guides, the CLI's help and
  output, the web UI's labels, the tool and schema descriptions an agent
  is handed, the OpenAPI descriptions, and the server's error text.

  Renaming the tools alone had left `get_concept` describing itself as
  "Fetch one entry", the English documentation saying `entry` 287 times
  against `concept` 6, and the web UI labelling its own button "New
  entry". The translation table 0054 set out to remove had moved rather
  than gone.

  The line is words a reader meets versus identifiers. The `entries`
  JSON key stays — naming the array in a response is a different
  question from naming the unit — and so do Go type names, database
  columns, and "entry" in its other English senses (a catalog entry, a
  `sources` entry, a changelog entry). **Nothing on the wire moves**;
  only a script matching on help or error text sees this.

- **`ochakai import --dry-run` now asks the server what the import would
  do** instead of listing what it parsed (design doc
  [0061](docs/design/0061-a-dry-run-is-the-write-withheld.md)). Every
  object is sent to `PUT /api/v1/bundle/{path}?dry_run=true`, a new
  parameter that runs the whole write path and stores nothing, and
  answers with the new `Ochakai-Plan: created | updated | unchanged`
  header beside the same `Ochakai-Note` headers a real write sets.

  This closes a false green. `--dry-run --strict` was documented as a CI
  gate, but half of what `--strict` fails on is not visible from the
  bundle: whether a document's trust family stays a claim under
  `received` is decided against the *stored* concept, whether the server
  refuses the document is the write path's validation, and whether the
  write changes anything is a comparison with stored bytes. A bundle
  carrying `verified:` passed the gate with 0 notes and then imported
  with one. It now reaches the import's own verdict, with nothing
  written.

  The output gains the breakdown the real import always had — `would
  create` / `would update` / `would leave unchanged`, and a summary line
  in the same shape — so a re-sync says how many of its 66 concepts would
  actually move.

  Two consequences worth knowing before upgrading: a dry run against an
  ochakai 0.16.1 or older server now **fails** rather than passing (that
  server ignored `dry_run` and wrote, so a missing plan header means the
  opposite of a clean bill), and a dry run of a large bundle now makes
  one request per object, as the import does. `?dry_run` is refused with
  a 400 on DELETE: a delete has no plan beside it, and ignoring the
  parameter would remove an object for a caller who believed they were
  asking. REST query parameters go 18 → 19 and headers 9 → 10, with
  [docs/surface.md](docs/surface.md)'s ceilings.

### Changed

- **BREAKING** — the recommended type vocabulary drops from eleven types
  to nine (design doc [0063](docs/design/0063-two-unused-recommended-types-leave.md)),
  four days after 0038 grew it to eleven. `Playbook` and `API Endpoint`
  are retired: neither spelling was ever written by ochakai's own
  `examples/` or by [OKF's own reference
  bundles](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles) —
  only named in the spec's prose — and a recommended type is taught on
  every MCP call (design doc 0015 §3.1), so an unused one is a cost with
  no offsetting use. The other nine additions and retirements 0038 made
  are unchanged.
  - **No migration runs and no stored entry changes.** `Playbook` and
    `API Endpoint` stay valid to write, matched, searched, exported and
    updated exactly as before — they are now free types (design doc
    0005).
  - `domain.TypePlaybooks` and `domain.TypeEndpoints` are gone from the
    Go API.
  - The MCP tool descriptions, schema, `--type` help, shell completion
    and [docs/knowledge.md](docs/knowledge.md)'s table all drop the two
    spellings; `domain.TypesHint()` stays the one source they render
    from.

### Fixed

- **`ochakai search --links-to <id>` works on its own.** The reverse
  lookup has listed without a query on the wire since design doc 0046
  §3.5 — a set is the answer and there is no text to rank it by — but the
  CLI refused it, and the refusal named `--sort` and `--source` as the
  ways to list without a query, which is how a reader learns that
  `--links-to` is not one of them. Of the three the message could have
  named, it named the two the caller had not asked for.

- **The export stops writing `status: ""`.** A concept whose document
  named no lifecycle value is a document that said nothing, and ochakai
  does not write a key its writer left out (design doc 0046 §3.9) — but
  the rendering `ochakai put` and `ochakai import` send emitted the key
  empty, so the empty string reached the store and came back out of the
  export. ochakai reads it as `stable` either way, and its own import
  round-trips it with 0 notes, so nothing here was broken for ochakai;
  what it cost was a third party's OKF reader, which met a concept with
  an empty status. The first import after upgrading rewrites the
  documents that carried it — same content, a new revision, and
  `updated` rather than `unchanged` for those concepts once.

- `ochakai reject` is named in `ochakai help` and in the CLI reference's
  command list. It has shipped, run and carried its own `-h` text since
  the ruling existed, but the one place a reader looks for the commands
  never listed it, so half of review was reachable only by already
  knowing it was there. The guard that should have caught it compared
  the dispatch table against a list written out in the test — a copy of
  the help rather than a reading of it — and now reads the help itself,
  in both directions.

## [0.16.1] - 2026-07-30

### Fixed

- **Upgrading to 0.16.0 fails on any instance that holds a file**, and
  fails in the worst place: migration `0029_files_become_objects.sql`
  moves the attachment rows into `object`, where a file has no concept
  id, but it did so *before* replacing the haystack trigger that 0016
  put on the table. The old trigger computed `ochakai_search_text(NULL,
  …)`, which is NULL, and the column is `NOT NULL` — so the first file
  to move aborted the migration with

  ```
  migration 0029_files_become_objects.sql: ERROR: null value in column
  "search_text" of relation "object" violates not-null constraint
  ```

  and the server exited. By then `0022`–`0028` had applied, so the
  running 0.15.0 binary was looking for a `knowledge` table that had been
  renamed out from under it and answered 500 to everything: **a failed
  upgrade took the deployment down rather than leaving it where it
  started.** An instance with no files migrated cleanly, which is why
  nothing caught it — the statement order is only reachable when there is
  a row to move.

  The functions, the triggers and the two indexes are now established
  before the rows move, which is the order the migration's own comments
  describe. Nothing else about it changes: the same objects end up in the
  same shape, so an instance that already applied `0029` successfully is
  unaffected and re-runs nothing.

  **If you hit this**: deploy 0.16.1 and start it. Migration `0029`
  re-runs from the beginning — it never committed — and `0030`–`0033`
  follow. There is nothing to undo by hand and no need to restore a
  backup.

  `TestMigrationFilesBecomeObjects` applies the migration to a base
  seeded with two files, one at its entry's canonical `<id>/<name>` and
  one an import placed elsewhere in the bundle, and checks the
  attribution and the filename haystack that follow.

## [0.16.0] - 2026-07-30

This release carries [0046](docs/design/0046-bundle-address-space.md)'s
fold from end to end, and the fold moved some things more than once
before it settled. **Entries below are in landing order, newest first,
so where an early one names a spelling a later one changed, the later
one is what shipped.** The whole of what moved between `0.15.0` and here:

```
REST
  GET|PUT|DELETE /api/v1/knowledge/{id}          →  the same three on /api/v1/bundle/{id}.md
  GET|PUT|DELETE /api/v1/attachments/{id}/{name} →  the same three on /api/v1/bundle/{path}
  GET    /api/v1/knowledge?q=…                   →  GET  /api/v1/search?q=…
  GET    /api/v1/backlinks/{id}                  →  GET  /api/v1/search?links_to={id}
  GET    /api/v1/export                          →  GET  /api/v1/bundle/ + Accept: application/gzip
  GET    /api/v1/bundle/{id}/log.md as JSON      →  GET  /api/v1/bundle/{id}.md?history
  POST   /api/v1/verify/{id}                     →  POST /api/v1/review/{id} {"ruling":"verified"}
  POST   /api/v1/reject/{id}                     →  POST /api/v1/review/{id} {"ruling":"rejected"}
  DELETE /api/v1/reject/{id}                     →  POST /api/v1/review/{id} {"ruling":"withdrawn"}

MCP
  search_knowledge     →  search_concepts        create_knowledge  ┐
  get_knowledge        →  get_concept            update_knowledge  ┴→  put_concept
  delete_knowledge     →  delete_concept
  get_knowledge_usage  →  get_concept_usage

CLI
  ochakai create  ┐
  ochakai update  ┴→  ochakai put [--only-if-new]
```

REST goes from 19 operations to 11, MCP stays at 8 tools, the CLI goes
from 28 commands to 27 ([docs/surface.md](docs/surface.md)). Nothing
that could be asked before cannot be asked now.

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

- **Something to tell you the review queue is not empty** (design doc
  [0049](docs/design/0049-queue-counts.md)). **`ochakai queues`** counts
  the three feeds a curator empties — drafts waiting to be published or
  turned down, entries whose failure reports are unanswered, entries past
  the expiry their author declared — instead of listing them. The three
  numbers are the `queues` key of `GET /api/v1/stats`; they get no
  address of their own, because that address would return what the stats
  response already carries, under the same key and from the same query
  (§3.1, revised before release).

  The queues were emptiable already; nothing said they were not empty,
  and a review queue going quiet looked exactly like one with nothing in
  it. Each printed line carries the command that lists that queue, and
  `ochakai queues --exit-code` exits **2** while any of them is
  non-empty, so a scheduled CI job goes red on work nobody picked up —
  1 stays "the command failed", so an unreachable server cannot be read
  as "nothing to do". The web UI shows the same counts on its Review
  tab. [docs/guides/operating.md](docs/guides/operating.md) has the cron
  recipe.

  Scoping the counts to a subtree is what the endpoint of its own would
  have been for, so **`prefix` landed on `GET /api/v1/stats`** instead —
  and got wider there. Every number keyed by an entry honors it
  (`entries`, `queues`, `review`, `outcomes`), where a team on a shared
  deployment would otherwise have been able to count its own review queue
  but not its own knowledge. `misses` is never scoped and cannot be: a
  search that found nothing found it nowhere, so there is no id to scope
  by (design doc
  [0051 §3.7](docs/design/0051-instance-metrics-and-search-misses.md)).
  `ochakai stats` takes `--prefix`.

  ochakai delivers nothing itself: no mail, no chat, no webhooks, no
  address book, and no scheduler inside the server. The verification-age
  feed is deliberately uncounted — it ranks every verified entry, so its
  size is the size of the knowledge base and nobody can drive it to
  zero. MCP gets no tool for it: an agent's ends of the loop are
  drafting and reporting outcomes, both of which lengthen a queue rather
  than read it.

- **BREAKING** — the five MCP tools carrying `knowledge` are renamed to
  OKF's word for the unit of knowledge, `concept` (design doc
  [0054](docs/design/0054-concept-is-the-okf-word.md)):

  ```
  search_knowledge      →  search_concepts
  get_knowledge         →  get_concept
  put_knowledge         →  put_concept
  delete_knowledge      →  delete_concept
  get_knowledge_usage   →  get_concept_usage
  ```

  OKF SPEC §2 defines a **concept** as "a single unit of knowledge within
  a bundle", and a **concept ID** as its path without `.md`. ochakai's own
  design records and code comments have said "concept" since 0046; once
  §3.5's fold finished, these tool names were the only place on the wire
  still saying `knowledge` — REST and the CLI carry none. It is the third
  time this project has chosen OKF's spelling over its own (0023, 0038),
  and the last double vocabulary.

  Search alone is plural: `concept` is countable, a search returns many
  and the rest act on one. `get_context`, `report_outcome` and
  `get_attachment` keep their names — none of them names the unit. **The
  tool count stays at 8**, and no argument, response or capability
  changes.

  Breaking for a client that names these tools in its configuration — an
  allow-list, a hook, a prompt. A client that reads the advertised tool
  list needs nothing. Shipping both names was refused: tool schemas are
  paid for out of the agent's context, and 8 tools would have become 13
  (0054 §4).

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

  **This completes §3.5's fold.** The address space is the bundle, and
  what is not an object of it — a ranking, a judgment, a measurement —
  is addressed outside it.

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
  for you. A direct REST caller edits the URL.

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
  `update_knowledge` are one `put_concept` (spelled `put_knowledge` until
  0054 renamed the five, above): it creates when the id is free and
  replaces when it is taken (design doc
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
  listing mode of `GET /api/v1/search` — the four `sort` feeds and the
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
  seen twice. Counting the three review queues is the `queues` key of
  `GET /api/v1/stats`, above: count once, or walk — they are different
  questions.

  On the surfaces: MCP's `search_concepts` takes and returns `cursor`;
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
  questions that came back empty. It carries the three queue depths
  beside them, under `queues` — the place 0049 §3.1 kept for exactly
  this, and the only address they have. It is computed on demand — no
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

  `index.md` and `log.md` are 409 to every face that reads their address
  as something other than the file itself — a write, an archive
  (`Accept: application/gzip`, which reads a path as a subtree), or an
  object's `?history`. Reading the generated file is a 200: that is what
  generating it is for. A file's history lands in the same ledger the
  concepts use — created and deleted, by whom.

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

- **`?history` answers one history, whichever representation asks.** A
  markdown document that carries no `type` is a *file* (design doc
  [0046](docs/design/0046-bundle-address-space.md) §3.2) and lives at a
  `.md` path like a concept does, but `?history` routed on that suffix
  alone: with `Accept: application/json` it went looking for a concept
  ledger under an id nothing held and answered 404, at an address whose
  markdown listed the history fine. The concept ledger is asked first and
  falls through to the object's, which is keyed by path — the one key
  both kinds have.

- **A generated `log.md` links relative to the directory it sits in**,
  the way the `index.md` beside it does and the way SPEC §6 writes a link
  between two objects of one bundle. The links were root-absolute
  (`/metrics/revenue.md`), which resolves to the filesystem root once the
  archive is extracted: two generated files in one directory disagreed
  about what a bundle path means. A directory's history also covers the
  concept it is named after, one level up, so `../metrics.md` is a form
  these now take.

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

- **The published contract closes two holes it had been carrying.** The
  `501` on `PUT /api/v1/bundle/{path}` says what it is — writing a file
  needs GCS (design doc 0013), and a concept is unaffected — rather than
  standing unexplained where a "not built yet" placeholder used to be.
  And the recipe for writing your own importer no longer sends the
  reader to a `POST` that never existed: it is one PUT per bundle
  member, at the path it arrived at.

  While [0046](docs/design/0046-bundle-address-space.md) §3.5's fold ran,
  the spec marked the second spellings `deprecated` so a reader knew
  which address survives. The fold finished inside this release, so the
  marks went with the addresses: what `api/openapi.yaml` declares is what
  answers, and there is no deprecation window here — a removal arrives in
  a minor release with a changelog entry, as
  [the compatibility policy](docs/compatibility.md) says.

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

  The wire caught up with it later in this release — a file is read and
  written at its own bundle path, and `path` replaced `okf_path`
  (above). Two behaviours follow from the derivation rather than from a
  column, and both are what the design asks for: moving an entry carries
  the files under its namespace with it, and attaching a file at a path
  the entry's body says nothing about stores it at `<id>/<name>` instead
  of orphaning it.

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

[Unreleased]: https://github.com/na0fu3y/ochakai/compare/v0.27.3...HEAD
[0.27.3]: https://github.com/na0fu3y/ochakai/compare/v0.27.2...v0.27.3
[0.27.2]: https://github.com/na0fu3y/ochakai/compare/v0.27.1...v0.27.2
[0.27.1]: https://github.com/na0fu3y/ochakai/compare/v0.27.0...v0.27.1
[0.27.0]: https://github.com/na0fu3y/ochakai/compare/v0.26.3...v0.27.0
[0.26.3]: https://github.com/na0fu3y/ochakai/compare/v0.26.2...v0.26.3
[0.26.2]: https://github.com/na0fu3y/ochakai/compare/v0.26.1...v0.26.2
[0.26.1]: https://github.com/na0fu3y/ochakai/compare/v0.26.0...v0.26.1
[0.26.0]: https://github.com/na0fu3y/ochakai/compare/v0.25.0...v0.26.0
[0.25.0]: https://github.com/na0fu3y/ochakai/compare/v0.24.1...v0.25.0
[0.24.1]: https://github.com/na0fu3y/ochakai/compare/v0.24.0...v0.24.1
[0.24.0]: https://github.com/na0fu3y/ochakai/compare/v0.23.0...v0.24.0
[0.23.0]: https://github.com/na0fu3y/ochakai/compare/v0.22.1...v0.23.0
[0.22.1]: https://github.com/na0fu3y/ochakai/compare/v0.22.0...v0.22.1
[0.22.0]: https://github.com/na0fu3y/ochakai/compare/v0.21.1...v0.22.0
[0.21.1]: https://github.com/na0fu3y/ochakai/compare/v0.21.0...v0.21.1
[0.21.0]: https://github.com/na0fu3y/ochakai/compare/v0.20.0...v0.21.0
[0.20.0]: https://github.com/na0fu3y/ochakai/compare/v0.19.1...v0.20.0
[0.19.1]: https://github.com/na0fu3y/ochakai/compare/v0.19.0...v0.19.1
[0.19.0]: https://github.com/na0fu3y/ochakai/compare/v0.18.0...v0.19.0
[0.18.0]: https://github.com/na0fu3y/ochakai/compare/v0.17.0...v0.18.0
[0.17.0]: https://github.com/na0fu3y/ochakai/compare/v0.16.1...v0.17.0
[0.16.1]: https://github.com/na0fu3y/ochakai/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/na0fu3y/ochakai/compare/v0.15.0...v0.16.0
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
