# Design records, in English

The decision records in this directory are Japanese, immutable, and
authoritative. This file is neither immutable nor authoritative: it is a
reading aid for people who cannot read them, so that the citations to
"design doc NNNN" scattered through the README, the architecture doc and
`api/openapi.yaml` lead somewhere. Where a row here and its record
disagree, **the record is right** — say so in an issue and this file
gets fixed.

One row per record: the record it links to, its status, and the decision
in a sentence or two. The grouping follows [the index](README.md) — start
with the table at the top of it for which record still describes an area
today. Numbers have gaps: proposals that never landed stay in their pull
requests.

A record that has been superseded gets a one-line pointer to whatever
replaced it instead of a summary — that is enough to follow the trail and
is all the maintenance it earns (design record
[0048](0048-decision-records-for-wire-contracts.md) §2.5).
`TestEnglishDesignIndexCoversEveryRecord` holds every record to at least a
row, and `TestEnglishIndexSummarizesOnlyWhatIsCurrent` holds a superseded
one to the pointer and nothing more.

For the shape of the system rather than the history of it, read
[docs/architecture.md](../architecture.md) first.

## Architecture and foundations

| Record | Status | Decision |
|---|---|---|
| [0001 Overall architecture](0001-architecture.md) | Accepted; §6 superseded by 0002/0003, §3/§9.1 revised by 0043 | Context provider for data agents: one Go binary plus PostgreSQL, an envelope with status/provenance/links/revisions, no LLM and no SQL execution — human-verified knowledge returned verbatim; `pg_trgm` lexical search with opt-in pgvector hybrid via Vertex AI; source of the usage telemetry and outcome reporting later records build on. Interpretation stays the agent's job by design. |
| [0003 Google Cloud only](0003-gcp-only.md) | Accepted; Vertex AI became a default-path dependency in 0053 | Cloud Run + Cloud SQL (optionally Vertex AI) and nothing else. Portability is promised for your data (OKF export), not for the runtime — there is no supported deployment elsewhere. |
| [0053 Embeddings are the default, and a vector space is disposable](0053-embeddings-by-default.md) | Accepted; revises 0001 §4 | On Google Cloud, semantic search turns on by default via IAM with no variable to set; a deployment lacking `aiplatform.user` or vector support falls back to lexical instead of refusing to start, while a named `OCHAKAI_VERTEX_PROJECT` still fails loudly. A changed embedding dimension rebuilds vector tables itself rather than handing you a `DROP TABLE`. **A default flips** — `OCHAKAI_EMBEDDINGS=off` is the one way to opt out. |

## Quality gates

| Record | Status | Decision |
|---|---|---|
| [0035 Machine-checked invariants](0035-verifiability.md) | Accepted | Invariants Go's type system can't carry are checked from outside: golangci-lint (with `exhaustive`), an OpenAPI contract test on every REST integration request/response, and fuzzing plus round-trip tests on the OKF parser — a linter is admitted only if a clean tree reports nothing. No user-facing effect; it is why an endpoint drifting from the published spec fails CI. |

## How decisions get written down

| Record | Status | Decision |
|---|---|---|
| [0048 Decision records are for wire-contract decisions](0048-decision-records-for-wire-contracts.md) | Accepted | Narrows what earns a numbered record to decisions a user can observe (the wire, a stored form, identity/provenance, a new Google Cloud dependency, a refusal); internal changes belong in a PR instead. An unreleased record is revised by replacing it, never by taking a new number. The 47 existing records stay untouched; the index gains a table of which record to read per area, and full summaries here are owed only to records that are still current. |

## Authentication, authorization and provenance

| Record | Status | Decision |
|---|---|---|
| [0002 Authentication and authorization](0002-authn-authz.md) | Accepted; §4's IAP JWT verification implemented by 0032 | No authorization at all — whoever can reach a deployment can read and write; headers are read only to record the actor as provenance, and trust is judged by the reader from that provenance. No tokens to manage; the service must be deployed private (Cloud Run IAM) or identity headers are merely self-asserted. |
| [0040 Deployment-wide read-only mode](0040-read-only-mode.md) | Accepted; respelled `OCHAKAI_MODE=read-only` by 0060 | Serves knowledge without changing any, enforced at one point in the service layer regardless of caller. REST writes return 403, MCP offers no write tools, the web UI hides buttons that would fail, and `ochakai whoami` reports the mode. |
| [0042 The public read-only posture](0042-public-read-only.md) | Accepted; respelled `OCHAKAI_MODE=public` by 0060 | For a deployment anyone may reach: reads no identity at all (every caller is `human:anonymous`, nobody gets a 401), implies read-only, and is not a revision of 0002 since it reads *less* provenance rather than adding authorization. The only way to expose ochakai with no IAM in front; public *and* writable is not an accepted configuration. |
| [0060 One word for the posture](0060-one-word-for-the-posture.md) | Accepted; **breaking** | Collapses `OCHAKAI_READ_ONLY` / `OCHAKAI_PUBLIC_READ_ONLY` / `OCHAKAI_INSECURE_DEV` into one `OCHAKAI_MODE` (`read-only` / `public` / `dev`), changing nothing 0040 or 0042 decided — only its spelling; an unrecognized value is a startup error, not a guess. Set `OCHAKAI_MODE` before upgrading — the old variables are ignored, so a deployment that only set `OCHAKAI_READ_ONLY=true` becomes writable unless updated. ENV drops 16 → 14. |
| [0027 Delegated end-user provenance](0027-delegated-provenance.md) | Accepted; composition reused by 0052 for producer; header respelled without `X-` by 0064 | A caller listed in `OCHAKAI_DELEGATING_CALLERS` may name an end user via `Ochakai-On-Behalf-Of`; both identities are recorded (`human:x via process:sa`), and an unlisted caller's header is a 403 rather than a silent downgrade. Lets an application embedding ochakai attribute writes to its real users instead of one shared service account. |
| [0052 The self-declaration goes beside the actor](0052-producer-beside-the-actor.md) | Accepted; header respelled without `X-` by 0064 | Records SPEC §7's software-identity form (`Ochakai-Producer`, MCP `clientInfo`, or `OCHAKAI_PRODUCER`) in a fourth actor field rather than in the actor's place; actor kinds and trust tier are untouched. Puts "which agent, at which build, drafted this" on every provenance line, in `log.md`, and on export's `producer` key — import reads none of it back. |
| [0009 OKF/Git round-trips and who owns provenance](0009-provenance-portability.md) | **Proposed** | Provenance is what an instance observed, not a portable attribute: bundles carry knowledge only, and exported `created_by`/`verified_by` are historical reference that import never reads into a ledger; a `--preserve-provenance` flag is refused since nothing authorizes it. Migrating to a new instance records the importer — run migrations under a dedicated identity. |

## The knowledge model — structure, ids, types, names, links

| Record | Status | Decision |
|---|---|---|
| [0047 The filter vocabulary is the keys OKF defines](0047-fm-carries-okf-keys.md) | Accepted | Keeps typed filter columns (`fm.type` etc. refused with 400 rather than silently answering a different question) and closes `fm.` to only the keys OKF defines — a producer's own key is still stored and returned but not queryable, so a caller can always tell an empty result from a misspelled key. `fm.resource`, `fm.runtime` and the rest of OKF's own keys work on REST/MCP/CLI; `fm.` stays off the web UI. |
| [0061 A dry run is the write withheld](0061-a-dry-run-is-the-write-withheld.md) | Accepted | Adds `dry_run` and `Ochakai-Plan` to the write path itself, so `ochakai import --dry-run --strict` reaches the same verdict (claim status, refusals, whether anything would change) the real import reaches, sharing one decision step (`settleUpdate`) instead of a second reimplementation. Against an ochakai 0.16.1-or-older server the dry run fails rather than silently writing. |
| [0046 The bundle is the address space](0046-bundle-address-space.md) | Accepted; the current record for OKF compatibility (supersedes 0043, 0008, 0011, 0013, 0014) | ochakai holds one bundle — a path→object map — so any file, not just knowledge entries, is stored and round-tripped as received; a document's own trust family stays a claim under `received`, never entering the ledger or trust tier. REST collapses to `/api/v1/bundle/{path}` plus search/context/ledgers/move; MCP is five tools; an entry written without `status` reads as OKF's default (`stable`) until the ledger says otherwise. |
| [0043 The document is the truth](0043-document-first.md) | **Superseded by 0046** | |
| [0041 Narrowing a search by address](0041-path-scoped-search.md) | Accepted | Adds a repeatable `prefix` filter (segment-matched, ORed when repeated) to search and `get_context` for scoping by subtree; explicitly not an access boundary — reading stays bounded by who can reach the service (0002), and passing no prefix returns everything. |
| [0037 Making declared expiry and cited sources queryable](0037-stale-and-source-lookup.md) | Accepted, shipped in 0.14.0 | Adds `sort=stale_after` (entries past their declared expiry, most overdue first) and a `source` filter matching `sources[].resource` exactly; verifying does not clear the stale feed, only re-declaring a date does. |
| [0036 OKF's schema is ochakai's schema](0036-okf-schema-first.md) | **Superseded by 0043, then 0046** | |
| 0034 OKF v0.2 conformance | **Withdrawn — number vacant** | Folded into 0036 and the file deleted before any release carried it; only the document was withdrawn — the implementation shipped. |
| [0005 OKF compatibility and knowledge structure](0005-okf-compatibility.md) | **Superseded by 0036** | |
| [0016 Alignment with the knowledge-catalog reference bundles](0016-knowledge-catalog-alignment.md) | **Superseded by 0036** | |
| [0017 The path is the address; the type is an attribute](0017-path-addressing.md) | Accepted | The full path becomes the id and sole address (no type inferred from a path segment); a file with no frontmatter `type` is kept as a file, not a broken concept. MCP address-taking tools take one argument instead of two; an exported bundle's directory layout comes from ids alone. |
| [0019 Pre-0.10.0 consistency adjustments](0019-release-review-adjustments.md) | Accepted | Id segments are constrained only by path safety (non-ASCII ids and a leading underscore become legal); a bundle import treats a server rejection as a skip of that one document rather than aborting. |
| [0022 The filename is the name](0022-filename-as-name.md) | Accepted | `title` becomes optional — display falls back to the id's last segment, and the id joins both the lexical haystack and the embedding text; ids, attachment names, link targets and queries are NFC-normalized everywhere. Clients must tolerate a missing `title` in responses. |
| [0023 One type vocabulary, OKF's](0023-okf-type-vocabulary.md) | **Superseded by 0038** | |
| [0038 Realigning the recommended vocabulary](0038-type-vocabulary-realignment.md) | Accepted; §3.2 and §4's table amended by 0063 | Nine recommended types become eleven: `Semantic Model` and `Golden Query` are retired, `Skill`, `Playbook`, `Policy` and `API Endpoint` are added. Nothing migrates and no stored entry changes — retired spellings keep working as free types. |
| [0063 Two unused recommended types leave](0063-two-unused-recommended-types-leave.md) | Accepted; current record for the type vocabulary | Drops `Playbook` and `API Endpoint`, four days after 0038 added them: neither spelling was ever written by ochakai's own examples or by OKF's own reference bundles, only named in the spec's prose, and a recommended type is taught on every MCP call (0015 §3.1). Nine recommended types remain. Nothing migrates and no stored entry changes — both keep working as free types. |
| [0057 Concept is the word a reader meets too](0057-concept-is-the-word-a-reader-meets.md) | Accepted; **breaking (wording only)**; the current record for the unit of knowledge's name (carries 0054); §3.2's exclusion of the `entries` JSON key is revoked by [0064](0064-rest-stops-at-api-v1.md) §7 | Renames the five MCP tools carrying `knowledge` to OKF's word (`search_concepts`, `get_concept`, `put_concept`, `delete_concept`, `get_concept_usage`; tool count stays 8) and completes that by renaming every word a reader meets — README, docs, examples, CLI help/output, web UI labels, MCP/OpenAPI descriptions, error text — from `entry` to `concept`; identifiers (Go type names, DB columns, the Japanese records) are untouched. The `entries` JSON key was originally excluded too (issue #411 found the exclusion did not hold in two of the three places it applied; 0064 §7 renames it to `concepts`). |
| [0054 The unit of knowledge is called a concept](0054-concept-is-the-okf-word.md) | **Superseded by 0057** | |
| [0024 Links are derived from the body](0024-links-from-body.md) | Accepted | Removes the structured `links` input field; links come from markdown in the body (bundle-absolute, relative, `ochakai://`, autolinked), excluding fenced and inline code. Sending `links` on write is ignored — write a markdown link in the body instead. |

## Attachments

| Record | Status | Decision |
|---|---|---|
| [0008 Image attachments](0008-image-attachments.md) | **Superseded by 0046** | |
| [0011 Moving attachment bytes to GCS](0011-gcs-attachment-storage.md) | **Superseded by 0046** | |
| [0013 Any file type, GCS only](0013-attachment-files-gcs-only.md) | **Superseded by 0046** | |
| [0020 Attachment search](0020-attachment-search.md) | Accepted; rekeyed by bundle path in 0046 | Filenames join the lexical haystack, and attachments are embedded at attach time as a third list in the rank fusion; ochakai still refuses to interpret them (no OCR, no PDF text extraction). Content search needs a file-capable embedding model, and existing attachments aren't backfilled. |

The wire spelling `attachments`/`Attachment` outlived the concept 0046 §2.1
already retired. [0064](0064-rest-stops-at-api-v1.md) §6 is the record
that retires the name itself (`files`/`File` on the wire, `get_file` on
MCP) — it revises none of the records above; the storage model stays
0046's.

## Browse and the web UI

| Record | Status | Decision |
|---|---|---|
| [0006 Two serving paths for the web UI](0006-web-ui-serving.md) | Accepted | One embedded page, two commands: `ochakai ui` binds loopback and acts as you (also proxying `/mcp` under your own identity), `ochakai serve-ui` is the deployed team service, and `ochakai serve` never serves the UI. |
| [0014 Folder browse](0014-folder-browse.md) | **Superseded by 0046** | |
| [0021 Move, and web UI refinements](0021-move-and-webui-refinements.md) | Accepted | `POST /api/v1/move` rewrites an entry's id in one transaction, carrying revisions, attachments, usage, embeddings and the inbound references that point at it; an occupied destination is a 409 even when soft-deleted. Not on MCP. |
| [0044 The web UI edits documents](0044-web-ui-edits-documents.md) | Accepted | Editing in the web UI means editing the canonical OKF document as text (a parse check before save, type templates for new entries) rather than a form — the detail view still renders from the projection. Losing row editors for `sources`/`parameters` is a stated cost, not a saving. |
| [0032 Recording web UI writes under the IAP identity](0032-webui-iap-identity.md) | Accepted | Both proxies always strip a browser-supplied delegation header; with `OCHAKAI_IAP_AUDIENCE` set, `serve-ui` rebuilds it from IAP's verified assertion, and a request IAP did not sign is refused rather than silently recorded as the service account. |

## Surfaces — REST, MCP, CLI, web UI

| Record | Status | Decision |
|---|---|---|
| [0004 The remote CLI](0004-cli.md) | Accepted | Client commands are a pure REST client in the same binary, adding no server surface; resolution order is `--url` > `$OCHAKAI_URL` > the `ochakai use` selection. There is no `ochakai login` — the CLI resolves Google ID tokens itself. |
| [0007 Retiring the direct-database commands](0007-api-only-cli.md) | Accepted | Data in and out goes through the API; `serve` is the only command still touching the database directly, so API calls record the verified caller rather than an OS username. Seeding or restoring without a deployment means running a local `serve` and pointing client commands at it. |
| [0015 Surface consistency](0015-surface-consistency.md) | Accepted | Fixes what each surface is for and what it deliberately omits: REST is the contract; MCP's tool count is a context budget (browse, revisions, backlinks, bulk export/import, purge and reembed stay off it), and MCP refuses to overwrite or delete an entry a human has judged since it can't carry an `If-Match` precondition. An agent that finds verified knowledge wrong reports the outcome or drafts a replacement; humans edit from REST, CLI or the web UI. |
| [0058 Two filters nobody arrived through](0058-filters-nobody-arrived-through.md) | Accepted; **breaking** | `min_score` is removed entirely from REST and the CLI (scores are mode-dependent and uncalibrated, so a floor is nonsense under RRF rank fusion); `fm.` comes off MCP only, staying on REST and the CLI. If you passed `min_score`, use `budget` instead; an MCP client filtering with `fm` now uses the CLI or REST. Tool count stays 8. |
| [0033 Context hits are a ranking](0033-context-hits-are-a-ranking.md) | Accepted | `/context`'s `hits` strip out entry bodies, carrying only `id`/`type`/`title`/`status`/`score` on every surface, so the byte budget can't be bypassed by a duplicate copy. Breaking REST change in 0.13.0 — read bodies from `entries`, not `hits`. |
| [0064 REST stops at /api/v1](0064-rest-stops-at-api-v1.md) | Accepted; amends [0046](0046-bundle-address-space.md) §3.5 and [0057](0057-concept-is-the-word-a-reader-meets.md) §3.2 | The last breaking REST batch before the wire is declared frozen (issue #379); `docs/compatibility.md` is rewritten in the same PR to say so. **Breaking**: unknown query parameters now 400 on every operation (`fm.` excepted); `Ochakai-On-Behalf-Of`/`Ochakai-Producer` lose their `X-` prefix; a file's own address answers `Accept: application/json` with metadata instead of ignoring the header; `attachments`/`Attachment` are renamed to `files`/`File` on the wire and the MCP tool `get_attachment` becomes `get_file`; the `entries` JSON key on `stats`, the bundle listing and `context`/`get_context` is renamed to `concepts`, revoking 0057 §3.2's exclusion of it (issue #411 — the exclusion's reasoning did not hold for two of the three places it applied); a failed `If-None-Match: *` is 412 not 409; DELETE on a concept honors `If-Match`; a file PUT answers 201 on creation; `ruling: withdrawn` with nothing to withdraw is 409 not 404; out-of-range `limit`/`days` is 400 everywhere instead of silently clamping. `.md` is mandatory and part of a concept's bundle path, not a representation selector; `Change.path` is the full bundle path, not bare. Amends 0046 §3.5's representation table: a concept's default (no `Accept`) is the JSON View, not the export form. Only a security defect justifies breaking the freeze after this point; a repeated DELETE on an already-gone id stays 404, deliberately, and is safe to retry. No version or capability signal ships, and CORS stays deliberately absent.

## The verification loop and usage measurement

| Record | Status | Decision |
|---|---|---|
| [0050 Listings page, rankings do not](0050-listings-page-rankings-do-not.md) | Accepted | Every `sort` feed and the `source` lookup take an opaque keyset `cursor` and return one while more entries follow, so a queue is walked to its end instead of stopping at the limit; search refuses a cursor with a 400 since relevance has no page two. Pass the cursor back with the same sort and filters — REST `?cursor=`, MCP's `cursor`, `ochakai search --cursor`, the web UI's "load more". |
| [0062 A listing is not a search](0062-a-listing-is-not-a-search.md) | Accepted; **breaking** | Splits `ochakai search` in two: `--sort` and its four values become `ochakai list [feed]`, and `ochakai search` now requires a query — one capability, one command; two capabilities, two commands. The wire and vocabulary don't move (`GET /api/v1/search?sort=` and `search_concepts` are unchanged). `ochakai search --sort usage` → `ochakai list usage`; no deprecation period, the old flag fails as unknown. |
| [0056 One question, one command; one ruling, one word](0056-one-question-one-command.md) | Accepted; **breaking**; the current record for the ruling's face and spelling (carries 0055) | Replaces `POST /api/v1/verify/{id}` and `POST`/`DELETE /api/v1/reject/{id}` with one `POST /api/v1/review/{id}` taking `ruling: verified \| rejected \| withdrawn` in the body — the three shared every structural property and differed only in the value written — and folds `ochakai backlinks` into `ochakai search --links-to` and `ochakai queues` into `ochakai stats`, losing no capability; also unifies the spelling for un-rejecting to `withdraw` everywhere (CLI flag, web UI button, revision log). REST clients rewrite three one-line calls; `ochakai backlinks <id>` → `ochakai search --links-to <id>`; `ochakai queues` → `ochakai stats`; `ochakai reject --lift` → `ochakai reject --withdraw`; MCP is unchanged. |
| [0055 One ruling, one face](0055-one-ruling-one-face.md) | **Superseded by 0056** | |
| [0049 Counting the review queues](0049-queue-counts.md) | Accepted | Adds three counts a curator empties (unreviewed drafts, unanswered failure reports, entries past their declared expiry) under `GET /api/v1/stats`'s `queues` key, scoped by `prefix`; `ochakai stats` says whether anybody owes a review, and `--exit-code` exits 2 while any queue is non-empty. No delivery mechanism — the operator's own cron or CI pushes. |
| [0059 A queue is named by the listing that shows it](0059-a-queue-is-named-by-its-listing.md) | Accepted; **breaking** | Renames `reported_wrong`→`failed` and `past_expiry`→`stale_after` so a queue's counted name matches the sort that lists it. `GET /api/v1/stats` returns `queues.failed` and `queues.stale_after`; a CI job reading `jq .queues.reported_wrong` needs one edit. |
| [0025 Closing the write-back loop](0025-closing-the-loop.md) | Accepted | Adds the `sort=failed` re-verification feed and `POST /api/v1/verify/{id}` so re-checking can be recorded; automatic demotion of verified entries was refused. `ochakai verify` is what empties the review feeds; verification isn't exposed on MCP. |
| [0029 Usage recording off the read path](0029-usage-recording-off-the-read-path.md) | Accepted | Usage events are buffered in memory and flushed periodically instead of being written during a read; usage statistics are explicitly best-effort (overflow is dropped, a failed flush loses that batch). Usage counts can lag reads or be lost in a crash; the knowledge itself never is. |
| [0051 Recording the questions nothing answered, and measuring the loop at the instance](0051-instance-metrics-and-search-misses.md) | Accepted | A search that returns nothing becomes a row of its own (buffered and pruned like a usage event, since a miss has no knowledge id to hang off); `GET /api/v1/stats` also answers entries by lifecycle/trust, review activity in a window, and the most-asked unanswered questions. A miss is defined as zero hits, never a low score. Storing query text has an off switch (`OCHAKAI_RECORD_MISSES=false`) and is off by default on a public deployment. |

## Concurrency and deletion

| Record | Status | Decision |
|---|---|---|
| [0030 Optimistic locking with If-Match](0030-optimistic-locking.md) | Accepted | Conditional updates are opt-in, exposed as an ETag and accepted as `If-Match`; a mismatch is a 412 with nothing written. Send `If-Match` and handle 412; omitting it keeps last-write-wins behavior. |
| [0031 Purge](0031-purge.md) | Accepted | A second deletion stage that discards an already soft-deleted entry and frees its id, refusing a live one; GCS bytes aren't reclaimed since blobs are content-addressed and may be shared. Irreversible, REST/CLI only — an export is the only way back. |

## Semantic models and compile

| Record | Status | Decision |
|---|---|---|
| [0018 Retiring import-ossie](0018-semantic-model-as-knowledge.md) | Accepted | A semantic model gets no dedicated mechanism — it's an ordinary knowledge entry, gaining revisions, provenance and the OKF round trip like any other. |
| [0028 Retiring compile_sql](0028-retire-compile-sql.md) | Accepted | Removes deterministic SQL compilation entirely (endpoint, MCP tool, CLI command, web UI tab, write-time validation) — cut for cost, not technique: no users, and its tool schema was the largest consumer of an agent's context. Breaking in 0.13.0; what an agent needs is the verified query and its caveat, both from `get_context`. |

## The MCP OAuth connector, and what replaced it

| Record | Status | Decision |
|---|---|---|
| [0010 An MCP OAuth connector service](0010-mcp-oauth-connector.md) | **Superseded by 0012** | |
| [0012 Retiring the connector](0012-retire-mcp-oauth-connector.md) | Accepted | Removed on cost, not technique — no user materialized, while the connector alone carried the deployment's only secret and about 1,700 unused lines under security review. Returns to 0002's "not until it is needed". |
| [0039 A stdio bridge for MCP clients](0039-mcp-stdio-bridge.md) | Accepted | Fills the hole 0012 left with `ochakai mcp-stdio` in the CLI rather than a public service, forwarding JSON-RPC message by message so it never holds a second copy of the tool definitions. A desktop client reaches a Cloud Run deployment with no credentials configured client-side. |

---

Two numbers are vacant. **0026** was a proposal to replace Cloud SQL with
Cloud Storage as the only persistence layer; it never landed, so it lives
in its pull request. **0034** is described above.
