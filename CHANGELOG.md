# Changelog

All notable changes to ochakai are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) — while
the major version is 0, a minor bump may break compatibility, and those
breaks are marked **BREAKING** below.

Dates are the tag dates (JST). Each released version also has a GitHub
release with longer prose; this file keeps what an operator upgrading
needs to know. Database migrations run automatically at server start
unless a note says otherwise.

Releases before 0.9.0 are retracted in `go.mod` and unsupported — see the
last entry.

## [Unreleased]

### Added

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

[Unreleased]: https://github.com/na0fu3y/ochakai/compare/v0.14.0...HEAD
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
