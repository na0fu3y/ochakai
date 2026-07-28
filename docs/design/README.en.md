# Design records, in English

The decision records in this directory are Japanese, immutable, and
authoritative. This file is neither immutable nor authoritative: it is a
reading aid for people who cannot read them, so that the sixty-odd
citations to "design doc NNNN" scattered through the README, the
architecture doc and `api/openapi.yaml` lead somewhere. Where a summary
here and its record disagree, **the record is right** — say so in an issue
and this file gets fixed.

Each entry gives the status, what was decided, and — where there is one —
what it means for somebody using ochakai rather than building it. The
grouping follows [the index](README.md), which is the file that says which
records still describe the current state of an area. Numbers have gaps:
proposals that were never accepted stay in their pull requests.

For the shape of the system rather than the history of it, read
[docs/architecture.md](../architecture.md) first.

## Architecture and foundations

- **[0001 Overall architecture](0001-architecture.md)** — *Accepted; §6's
  distribution and auth are superseded by 0002 and 0003, its
  restriction on promoting entries to verified was withdrawn by 0002, and
  §3's envelope representation and §9.1's rejected status are revised by
  0043.*
  ochakai is a context provider for data agents: one Go binary plus
  PostgreSQL, holding knowledge entries under a common envelope with
  status, provenance, links and revisions. It runs no LLM and executes no
  SQL — it returns human-verified knowledge verbatim. Search is `pg_trgm`
  lexical, with opt-in pgvector hybrid search through Vertex AI. Also the
  source of the usage telemetry and outcome reporting that later records
  build on.
  *For a user:* semantic search is off until you configure embeddings, and
  interpretation is your agent's job by design.

- **[0003 Google Cloud only](0003-gcp-only.md)** — *Accepted.* Supports
  Cloud Run + Cloud SQL (optionally Vertex AI) and nothing else,
  overturning 0001's portability stance. Removed in 0.3.0: `OCHAKAI_AUTH`,
  the static-token client mode, CORS configuration, and the embedding
  provider switch. Starting with a removed variable whose behavior is lost
  is refused rather than ignored.
  *For a user:* portability is promised for your data (OKF export), not
  for the runtime; there is no supported deployment elsewhere.

## Quality gates

- **[0035 Machine-checked invariants](0035-verifiability.md)** —
  *Accepted.* Invariants Go's type system cannot carry are checked from
  outside, in three layers: golangci-lint (with `exhaustive` as the
  centerpiece), a contract test that runs every REST integration request
  and response past `api/openapi.yaml`, and fuzzing plus round-trip
  property tests on the OKF parser. A linter is admitted only if a clean
  tree reports nothing.
  *For a user:* none — internal, but it is why an endpoint that drifts
  from the published spec fails CI.

## Authentication, authorization and provenance

- **[0002 Authentication and authorization](0002-authn-authz.md)** —
  *Accepted; §4's IAP JWT verification was later implemented by 0032.*
  Establishes the rule that whoever can reach a deployment can read and
  write. ochakai implements no authorization at all — no roles, no scopes,
  no token issuance — and reads headers only to decide the actor recorded
  as provenance. Trust is judged by whoever reads the entry, from that
  provenance.
  *For a user:* there are no tokens to manage, and the service must be
  deployed private (IAM-enforced) or the identity headers are merely
  self-asserted.

- **[0040 Deployment-wide read-only mode](0040-read-only-mode.md)** —
  *Accepted.* `OCHAKAI_READ_ONLY=true` makes a deployment serve knowledge
  without changing any. It is deliberately not authorization: it never
  looks at the caller and refuses the operator too. Enforcement sits at one
  point in the service layer, so no surface can leak past it. Usage
  recording continues, being the server's own observation.
  *For a user:* REST writes return 403, MCP does not offer the write tools
  at all, the web UI stops drawing buttons that would fail, and
  `ochakai whoami` reports the mode.

- **[0042 The public read-only posture](0042-public-read-only.md)** —
  *Accepted.* `OCHAKAI_PUBLIC_READ_ONLY=true` is the posture for a
  deployment anyone may reach — a demo, or a reference-only copy handed
  out. It reads no identity at all: `Authorization` and the delegation
  header are ignored, every caller is `human:anonymous`, and nobody gets a
  401. Reading a token nothing verified would be worse than ignoring it,
  since without Cloud Run IAM in front a forged one could name any person
  while the anonymous visitor a demo is made of is turned away. It implies
  0040's read-only mode and cannot be separated from it: not recording who
  asked is defensible only because nothing is written. Not a revision of
  0002 — reading less provenance is the opposite of adding authorization —
  and deliberately not spelled `OCHAKAI_INSECURE_DEV`, which also lets
  anyone delegate and must stay a name that means "not this".
  *For a user:* it is the only way to expose ochakai without IAM in front,
  and a publicly readable *and* writable deployment is not a configuration
  the program accepts.

- **[0027 Delegated end-user provenance](0027-delegated-provenance.md)** —
  *Accepted; the web-UI half is 0032.* A caller listed in
  `OCHAKAI_DELEGATING_CALLERS` may name an end user with
  `X-Ochakai-On-Behalf-Of`. Both identities are recorded — `human:x via
  agent:sa` — never just the forwarded one, and the header from an
  unlisted caller is a 403 rather than a silent downgrade.
  *For a user:* an application embedding ochakai can attribute writes to
  its real users instead of collapsing them into one service account.

- **[0009 OKF/Git round-trips and who owns
  provenance](0009-provenance-portability.md)** — *Proposed; its
  world-view was settled by 0036 §2.2 and carried into the stored shape
  by 0043.* Provenance is what an instance
  observed, not a portable attribute, so bundles carry knowledge only and
  exported `created_by` / `verified_by` are historical reference that
  import never reads back. A `--preserve-provenance` flag is refused:
  with no authorization mechanism, it would let anyone self-assert
  provenance and destroy the anchor 0002 depends on.
  *For a user:* migrating to a new instance records the importer, so run
  migrations under a dedicated identity.

## The knowledge model — structure, ids, types, names, links

- **[0047 `fm.` carries the keys ochakai does not name](0047-fm-carries-unnamed-keys.md)**
  — *Accepted; amends 0046 §3.11, whose "the named filters become sugar
  over the frontmatter expression" is not what shipped, and adds one
  deliberate omission to 0015 §3.* The typed columns stay — they are the
  better index — so `fm.type`, `fm.status`, `fm.tags`, `fm.sources` and
  `fm.stale_after` are refused with 400 instead of answering a different
  question than the filter of the same name. The difference was real and
  invisible: `status=stable` matches a document that says nothing, because
  OKF's default is stable, and `fm.status=stable` never did — which after
  0046 §3.9 is every entry an agent wrote without saying `draft`. The
  boundary is "the key already has a way to ask", not "ochakai knows the
  key", so a key with a projection but no filter is still queryable.
  *For a user:* the refusal names the filter to use, so the next request
  is the right one; `fm.` stays on REST, MCP and the CLI, and stays off
  the web UI, whose filters show the values you can pick.

- **[0046 The bundle is the address space](0046-bundle-address-space.md)**
  — *Accepted; the current record for OKF compatibility, superseding 0043,
  0008, 0011, 0013 and 0014. Implementation follows in later pull requests,
  released together with 0043's. §3.11's "the named filters become sugar"
  is amended by 0047.* Turns the remaining mapping inside out:
  what ochakai holds is one bundle — a map from path to object — and a
  knowledge entry is one kind of object in it. A `.md` without a `type` and
  a file nothing links to are stored and round-tripped instead of being
  dropped, so what goes into a bundle comes out of it; attachments stop
  being a concept and become objects, with "which entry owns this file"
  derived from the body the way links already are. Storage becomes **the
  bytes as received** — canonical form is a derived value used to decide
  whether the content meaningfully changed — so frontmatter comments and
  key order survive, and the server stops rewriting the writer's `type`
  casing or inventing a `status` the writer did not put there. `index.md`
  and `log.md` become derived surfaces in the shapes SPEC §8 and §9 define,
  absorbing browse and revisions and making history portable. Body links
  keep only SPEC §6's forms, so `ochakai://` leaves the bundle. Trust is
  SPEC §5.3's three tiers rather than a boolean, and the index is the
  frontmatter as `jsonb`, so a key the spec adds is queryable with no
  migration.
  *For a user:* REST collapses to `/api/v1/bundle/{path}` plus search,
  context, the ledgers and move; MCP is five tools; an entry written
  without a `status` reads as OKF's default (`stable`) and is unverified
  until the ledger says otherwise.

- **[0043 The document is the truth](0043-document-first.md)** —
  *Superseded by 0046, which keeps its world-view, status vocabulary,
  ledgers and actor spelling and replaces the stored shape, the wire, the
  hash's subject, canonicalization, revisions and the surface list; §3.11's
  web UI form sugar had already been withdrawn by 0044. Implemented across
  later pull requests and not yet released.* Both storage and the wire become the
  canonical OKF document itself (frontmatter + markdown); the database is
  reduced to derived index columns and instance-local ledgers. Status
  becomes OKF's three values verbatim; "a human confirmed this" is a row
  in an append-only verification ledger (so verifications are plural and
  re-verification is history, not an overwrite); rejection moves to its
  own ledger and stops being a status; the ETag becomes a hash of the
  canonical document; actors are spelled `human:` / `process:` per SPEC
  §7. The mapping tables, the envelope-versus-`attrs` double floor, the
  lossy `rejected` round trip, the demotion of foreign `stable` entries,
  and `updated_at`'s triple duty all lose their subject.
  *For a user:* a breaking release — knowledge is written as a document
  (PUT), read as document + read-only projection + observed ledgers, the
  status vocabulary changes, and one migration rewrites the stored shape
  one way. Bundles gain multi-entry `verified` lists and `process:`
  actors; nothing needs re-embedding.

- **[0041 Narrowing a search by address](0041-path-scoped-search.md)** —
  *Accepted.* Carries 0017's "the path is the address" through to search
  and `get_context`, which could filter by type, status, tag and source but
  never by id. A repeatable `prefix` filter narrows to a subtree — the
  prefix's own entry and everything under it — matched at segment
  boundaries, so `prefix=metrics` does not reach `metrics-legacy/churn`.
  It is a filter rather than a mode, so it combines with a query and with
  any `sort`, and repeating it ORs the scopes together, because the
  ordinary question is "my team's *and* the company's". No index and no
  migration. The record is unusually candid that the demand for this was
  weaker than for 0037, and that the feature only earns its place where
  directories mean something a type cannot say — a team, a domain, a
  tenant. It is emphatically **not an access boundary**: anyone may pass
  any prefix, passing none returns everything, and reading stays bounded
  by who can reach the service (0002). The record spends a paragraph on
  that, because a scope filter and a read ACL look alike from outside and
  running a deployment as though they were the same would hollow out 0002.
  *For a user:* `--prefix` on the CLI, `prefix` on REST and MCP; scoping
  by path stops meaning a second, parallel tagging scheme that drifts from
  the tree.

- **[0037 Making declared expiry and cited sources
  queryable](0037-stale-and-source-lookup.md)** — *Accepted, shipped in
  0.14.0.* `stale_after` and `sources` were writable but not askable.
  Adds `sort=stale_after`, a feed of entries already past the expiry their
  author declared, most overdue first, with "today" pinned to UTC; and a
  `source` filter matching `sources[].resource` exactly, so a GIN
  containment index applies.
  *For a user:* `?sort=stale_after` and `?source=…` on REST, `--sort` and
  `--source` on the CLI, both also on MCP. Verifying does not clear the
  stale feed — only editing the entry to re-declare a date does.

- **[0036 OKF's schema is ochakai's schema](0036-okf-schema-first.md)** —
  *Superseded by 0043, which the code keeps shipping until its
  implementation lands. Shipped in 0.14.0; supersedes 0005 and 0016. Two
  items in §5 were withdrawn and implemented by 0037, and §3.6's type list
  was reworked by 0038.* One rule replaces
  the whole area: keys the OKF v0.2 spec defines are first-class envelope
  fields, and `attrs` keeps only producer extension keys. Seven fields move
  up (`sources`, `usage_window`, `runtime`, `parameters`, `computation`,
  `executor`, `attester`); trust becomes `generated` / `verified`.
  Compatibility with past ochakai loses to compatibility with OKF.
  *For a user:* those keys left `attrs` (migration 0020 moved them),
  writing an `attrs` key that shadows an envelope key is a 400, and an
  Attested Computation without `runtime` is rejected.

- **0034 OKF v0.2 conformance** — *Withdrawn; the number is vacant.*
  Written as the v0.2 work and merged to `main`, but its criterion for
  what belongs on the envelope turned out to be wrong, so it was folded
  into 0036 and the file deleted before any release carried it. The text
  survives in the history of PR #176; the background is 0036 §1.1. Only
  the document was withdrawn — the implementation shipped.

- **[0005 OKF compatibility and knowledge
  structure](0005-okf-compatibility.md)** — *Superseded by 0036.* The
  starting point for bidirectional OKF compatibility: slash-separated
  hierarchical ids, free-string types, unknown frontmatter preserved in
  `attrs`, `index.md` / `log.md` reserved, and bundle import as a
  client-side loop over endpoints that already exist rather than a new
  server surface.

- **[0016 Alignment with the knowledge-catalog reference
  bundles](0016-knowledge-catalog-alignment.md)** — *Superseded by 0036;
  its type vocabulary was already replaced by 0023.* Adopted the
  conventions of Google's reference OKF bundles, including promoting
  `resource` from `attrs` to an envelope field.

- **[0017 The path is the address; the type is an
  attribute](0017-path-addressing.md)** — *Accepted; the id character set
  was relaxed by 0019 §2, the type vocabulary replaced by 0023 and 0038,
  and filtering by address added by 0041.* Removes the rule that a path's
  first segment names the type. The full path is the id and the sole
  address; the primary key becomes the id alone. A file with no frontmatter
  `type` is skipped and reported rather than having a type inferred from
  where it sits.
  *For a user:* MCP address-taking tools take one argument instead of two,
  and an exported bundle's directory layout comes from ids alone — so
  organize files however you like.

- **[0019 Pre-0.10.0 consistency
  adjustments](0019-release-review-adjustments.md)** — *Accepted; §4 lost
  its subject when 0028 removed compile.* Id segments are constrained only
  by path safety, which made non-ASCII ids and a leading underscore legal.
  A bundle import treats a server rejection as a skip of that one document
  and carries on.

- **[0022 The filename is the name](0022-filename-as-name.md)** —
  *Accepted.* `title` becomes optional: the display name falls back to the
  last segment of the id, and export omits an empty title. The id joins
  both the lexical haystack and the embedding text, so an entry named only
  by its path is still findable. Ids, attachment names, link targets and
  queries are NFC-normalized at every entry point.
  *For a user:* clients must tolerate a missing `title` in responses.

- **[0023 One type vocabulary, OKF's](0023-okf-type-vocabulary.md)** —
  *Superseded by 0038.* Ends the double vocabulary of internal slugs and
  OKF display names: the stored type is the OKF spelling itself, and a
  type is any single line. Aliases were refused — no compatibility shims
  while the major version is 0.
  *For a user:* type values need quoting or URL-encoding, and are matched
  case-insensitively.

- **[0038 Realigning the recommended
  vocabulary](0038-type-vocabulary-realignment.md)** — *Accepted; the
  current record for the type vocabulary.* Nine recommended types become
  eleven. `Semantic Model` and `Golden Query` are retired — the first
  explains nothing without an external spec, the second is what an
  `Attested Computation` with a `runtime` already is — and `Skill`,
  `Playbook`, `Policy` and `API Endpoint` are added, two of them naming
  the far end of edges ochakai already stores.
  *For a user:* nothing migrates and no stored entry changes; the retired
  spellings keep working as free types.

- **[0024 Links are derived from the body](0024-links-from-body.md)** —
  *Accepted; 0046 §3.6 retires the `ochakai://` form from bodies in favour
  of SPEC §6's bundle-absolute and relative paths.* The structured `links` input field is removed. Links come
  from the markdown in the body — bundle-absolute, relative, `ochakai://`,
  autolinked — with fenced and inline code excluded, because a link in an
  example is not an edge.
  *For a user:* sending `links` on write is ignored; write a markdown link
  in the body instead.

## Attachments

- **[0008 Image attachments](0008-image-attachments.md)** — *Superseded by
  0046, which dissolves attachments into bundle objects; the "evidence, not
  originals" framing and the `<id>/<name>` layout survive as the rule that
  derives which entry a file belongs to.* An image is knowledge only together
  with the text around it, so search and retrieval are per-entry and an
  image is never a standalone result. Metadata first, bytes on demand. SVG
  is refused because it can carry script into the web UI. 5 MiB per file,
  20 files per entry.

- **[0011 Moving attachment bytes to GCS](0011-gcs-attachment-storage.md)**
  — *Superseded by 0046; only "the bytes live in GCS" survives.* Only the bytes move; attachment rows,
  revisions and metadata stay in PostgreSQL. Signed URLs were refused —
  the API keeps proxying bytes, so clients see no change.

- **[0013 Any file type, GCS only](0013-attachment-files-gcs-only.md)** —
  *Superseded by 0046; sniffing the media type and running markdown-only
  without a bucket carry forward, while refusing SVG and HTML at write time
  becomes a serving-side defence.* Widens attachments from
  images to the intersection of what Claude reads and what Gemini embeds:
  png, jpeg, webp, pdf, plain text. The media type is decided by sniffing
  the bytes, never by what the client claims. HTML and SVG stay refused.
  *For a user:* attachments need `OCHAKAI_GCS_BUCKET`; without it the
  instance runs as a markdown-only knowledge base and attach returns 501.

- **[0020 Attachment search](0020-attachment-search.md)** — *Accepted; 0046
  rekeys it by bundle path.*
  Filenames join the lexical haystack, and attachments are embedded at
  attach time as a third list in the rank fusion. ochakai still refuses to
  interpret them: no OCR, no PDF text extraction.
  *For a user:* image and PDF content search needs a file-capable model
  (`gemini-embedding-2`), and existing attachments are not backfilled.

## Browse and the web UI

- **[0006 Two serving paths for the web UI](0006-web-ui-serving.md)** —
  *Accepted; §6's note that an IAP-fronted deployment still records the
  service account was overturned by 0032.* One embedded page, two
  commands, so the dangerous combinations cannot be expressed: `ochakai
  ui` binds loopback and acts as you, `ochakai serve-ui` is the deployed
  team service, and `ochakai serve` never serves the UI.
  *For a user:* `ochakai ui` also proxies `/mcp`, so an MCP client can use
  `http://127.0.0.1:<port>/mcp` under your own identity.

- **[0014 Folder browse](0014-folder-browse.md)** — *Superseded by 0046,
  which makes browse the derived `index.md`; its parameters had been
  revised by 0017 §4.7.* `GET /api/v1/browse` returns one
  level of the id hierarchy at a time. A level is cut off at 1000 entries
  and flagged rather than paginated. Browsing records no usage, since
  walking a tree is not a demand signal. REST and web UI only — deliberately
  not an MCP tool.

- **[0021 Move, and web UI refinements](0021-move-and-webui-refinements.md)**
  — *Accepted.* `POST /api/v1/move` rewrites an entry's id in one
  transaction, carrying revisions, attachments, usage, embeddings and the
  inbound references that point at it. An occupied destination is a 409
  even when soft-deleted. Not on MCP.

- **[0044 The web UI edits documents](0044-web-ui-edits-documents.md)** —
  *Accepted; revises 0043 §3.11.* Withdraws the form sugar: editing in the
  web UI is editing the canonical OKF document as text, while the detail
  view still renders from the projection. Filling a form needs the browser
  to parse real YAML, and the two ways out — a hand-written YAML subset
  parser in JS, or a thicker read shape carrying the envelope beside the
  document — were both refused: the first plants a parser whose failure
  mode is silently dropping a key, days after 0043 §3.6 decided that a key
  dropped on the way in is unrecoverable; the second leaves the duplicate
  representation that document-first exists to remove. The stated reason
  to prefer the document is that the one surface humans touch should teach
  the format the CLI, MCP, export and git bundles all use. The record is
  candid that losing the row editors for `sources` and `parameters` is a
  cost, not a saving, and names what comes back instead: a parse check
  before save, and type templates for new entries. If the demand returns,
  the answer is a server-side "parse this document" surface, not a second
  default read shape.
  *For a user:* the web UI's edit screen is a document; the detail screen
  is unchanged.

- **[0032 Recording web UI writes under the IAP
  identity](0032-webui-iap-identity.md)** — *Accepted.* Both proxies always
  strip a browser-supplied delegation header; with `OCHAKAI_IAP_AUDIENCE`
  set, `serve-ui` rebuilds it from IAP's verified assertion, and a request
  IAP did not sign is refused rather than silently recorded as the service
  account.

## Surfaces — REST, MCP, CLI, web UI

- **[0004 The remote CLI](0004-cli.md)** — *Accepted; the compile row and
  exit code 2 lost their subject with 0028.* The client commands are a pure
  REST client in the same binary, adding no server surface. Resolution
  order is `--url` > `$OCHAKAI_URL` > the `ochakai use` selection. There is
  no `ochakai login`: the CLI resolves Google ID tokens itself.

- **[0007 Retiring the direct-database commands](0007-api-only-cli.md)** —
  *Accepted.* Data in and out goes through the API; `serve` is the only
  command that still needs to sit next to the database. Two paths to the
  same artifact drift, and API calls record the verified caller rather than
  an OS username.
  *For a user:* seeding or restoring without a deployment means running a
  local `serve` and pointing client commands at it.

- **[0015 Surface consistency](0015-surface-consistency.md)** —
  *Accepted; §4's verify-as-sugar judgment was overturned by 0025 §6, its
  surface table restated by 0046 §3.14, and one omission — `fm.` is not on
  the web UI — added to §3 by 0047 §4.*
  Fixes what each surface is for and, more usefully, what each deliberately
  omits. REST is the contract; MCP's tool count is a context budget, so
  browse, revisions, backlinks, attachment writes, bulk export/import,
  purge and reembed stay off it. MCP also refuses to overwrite or delete an
  entry a human has judged, because it has no way to carry the `If-Match`
  precondition that would make a safe replacement expressible.
  *For a user:* an agent that finds verified knowledge wrong reports the
  outcome or drafts a replacement; humans edit from REST, CLI or the web UI.

- **[0033 Context hits are a ranking](0033-context-hits-are-a-ranking.md)**
  — *Accepted; 0046 adds the trust tier to each row.* Records the byte-budget decision for `/context` and strips
  entry bodies out of `hits`, which now carry `id`, `type`, `title`,
  `status` and `score` on every surface — otherwise the budget could be
  bypassed by the duplicate copy.
  *For a user:* a breaking REST change in 0.13.0; read bodies from
  `entries`, not from `hits`.

## The verification loop and usage measurement

- **[0025 Closing the write-back loop](0025-closing-the-loop.md)** —
  *Accepted; 0037 later added a third feed, and 0043 turns verify's record
  into an append to a verification ledger, so re-verification accumulates
  as history.* Adds the `sort=failed`
  re-verification feed of entries agents reported wrong, worst first, and
  `POST /api/v1/verify/{id}` so that re-checking can be recorded — an
  unchanged PUT writes nothing and therefore cannot express "I looked
  again". Automatic demotion of verified entries was refused.
  *For a user:* `ochakai verify` is what empties the review feeds;
  verification is not exposed on MCP.

- **[0029 Usage recording off the read
  path](0029-usage-recording-off-the-read-path.md)** — *Accepted.* Usage
  events are buffered in memory and flushed periodically instead of being
  written during a read. Usage statistics are explicitly best-effort: the
  buffer has a ceiling and drops the overflow, and a failed flush loses
  that batch.
  *For a user:* usage counts lag reads by seconds and can be lost in a
  crash; the knowledge itself never is.

## Concurrency and deletion

- **[0030 Optimistic locking with If-Match](0030-optimistic-locking.md)** —
  *Accepted; 0043 moves the version from `updated_at` to a content hash and
  0046 makes that hash cover the bytes as stored, keeping the mechanism.* Conditional updates are opt-in,
  exposed as an ETag and accepted as `If-Match`.
  A mismatch is a 412 with nothing written, and the check lives inside the
  conditional UPDATE, so there is no window between reading and writing.
  *For a user:* send `If-Match` and handle 412; omit it and the behavior is
  the last write winning, as before.

- **[0031 Purge](0031-purge.md)** — *Accepted.* A second deletion stage
  that discards an already soft-deleted entry and frees its id, refusing a
  live one. GCS bytes are deliberately not reclaimed, since blobs are
  content-addressed and may be shared. An audit row records who purged
  what, without the content.
  *For a user:* REST and CLI only, irreversible, and the revision history
  is gone with it — an export is the only way back.

## Semantic models and compile

- **[0018 Retiring import-ossie](0018-semantic-model-as-knowledge.md)** —
  *Accepted; its compile-facing parts were removed by 0028 and the
  recommended type by 0038.* A semantic model gets no dedicated mechanism:
  it is an ordinary knowledge entry, which is how it gains revisions,
  provenance and the OKF round trip.

- **[0028 Retiring compile_sql](0028-retire-compile-sql.md)** —
  *Accepted; §3's decision to keep `Semantic Model` in the vocabulary was
  reversed by 0038.* Removes deterministic SQL compilation entirely — the
  endpoint, the MCP tool, the CLI command, the web UI tab and the
  write-time validation of model entries. The reason was cost, not
  technique: no users, and its tool schema was the largest consumer of an
  agent's context.
  *For a user:* breaking in 0.13.0; what an agent needs is the verified
  query and its caveat, and both arrive from `get_context`.

## The MCP OAuth connector, and what replaced it

- **[0010 An MCP OAuth connector service](0010-mcp-oauth-connector.md)** —
  *Superseded by 0012; kept as the starting point if it ever returns.* A
  second, public Cloud Run service exposing only `/mcp` and the OAuth
  endpoints, so hosted assistants could reach an otherwise private
  deployment.

- **[0012 Retiring the connector](0012-retire-mcp-oauth-connector.md)** —
  *Accepted.* Removed on cost, not on technique: no user materialized,
  while the connector alone carried the deployment's only secret, its only
  publicly invokable service, and about 1,700 unused lines under security
  review. Returns to 0002 §4's "not until it is needed".

- **[0039 A stdio bridge for MCP clients](0039-mcp-stdio-bridge.md)** —
  *Accepted.* Fills the hole 0012 left — clients that speak only stdio had
  no route — with `ochakai mcp-stdio` in the CLI rather than a public
  service. It forwards JSON-RPC message by message, so it never holds a
  second copy of the tool definitions, and it attaches the Google ID token
  the client cannot mint.
  *For a user:* a desktop client reaches a Cloud Run deployment with no
  credentials configured on the client side.

---

Two numbers are vacant. **0026** was a proposal to replace Cloud SQL with
Cloud Storage as the only persistence layer; it never landed, so it lives
in its pull request. **0034** is described above.
