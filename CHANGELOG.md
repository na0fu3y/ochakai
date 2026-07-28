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

- **`ochakai log [path]`** prints the update history as OKF's `log.md`
  (SPEC §9): date-grouped, newest first, for a path or the whole bundle
  (design doc [0046](docs/design/0046-bundle-address-space.md) §3.8).
  It says what `ochakai revisions` says, in the format a bundle carries
  — which is what makes a history portable.

- **Any frontmatter key is a filter**: `?fm.owner=finance` on REST,
  `fm: {owner: finance}` on MCP, `--fm owner=finance` on the CLI (design
  doc [0046](docs/design/0046-bundle-address-space.md) §3.11). It matches
  a scalar exactly or a member of a list, and repeating it with different
  keys ANDs them.

  The whole frontmatter is now indexed as `jsonb`, so a key OKF adds in a
  later version — or one a producer invented — is askable the day
  somebody writes it, with no column, no migration and no release.
  Storage became additive when the document became the stored form;
  querying did not follow until now.

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

  The typed columns stay where they are — they are the same values with
  better indexes (trigram over the text, a date for `stale_after`, an
  array for tags) — so `type`, `status`, `tag`, `source` and
  `stale_after` keep answering from them. Because they do, `fm.type`,
  `fm.status`, `fm.tags`, `fm.sources` and `fm.stale_after` are refused
  with a 400 naming the filter to use instead (design doc
  [0047](docs/design/0047-fm-carries-unnamed-keys.md)). A column and a
  jsonb path do not answer the same question — `status=stable` matches a
  document that says nothing, since OKF's default is stable, while
  `fm.status=stable` never would — and which of the two you got would
  have depended on how you spelled the key. The prefix carries the keys
  ochakai does not name, and only those.

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
