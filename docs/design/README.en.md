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
[docs/architecture.md](../architecture.md) (Japanese) first.

## Architecture and foundations

| Record | Status | Decision |
|---|---|---|
| [0081 What ochakai is, and what it refuses to hold](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) | Accepted; the current record for the overall architecture (carries 0001) | A context provider for data agents that runs no LLM and executes no SQL — interpretation stays the calling agent's job, and human-verified knowledge is returned as it was written. One Go binary serving MCP and REST on one port, with **PostgreSQL as the only runtime dependency**: no Redis, no separate vector database, no search cluster, because pgvector works on plain Postgres and Cloud SQL alike. Distribution is a multi-arch GHCR image built `CGO_ENABLED=0 -trimpath` onto distroless static, with SBOM, SLSA provenance, keyless signing and SHA-pinned Actions. A rejection is kept as a ledger rather than a status — the memory of no that stops an agent re-proposing what a human turned down, opt-in to retrieve and excluded from search by default. §5 maps each of 0001's sections to whichever record owns it now. |
| [0001 Overall architecture](0001-architecture.md) | **Superseded by 0081** | |
| [0003 Google Cloud only](0003-gcp-only.md) | Accepted; Vertex AI became a default-path dependency in 0053 | Cloud Run + Cloud SQL (optionally Vertex AI) and nothing else. Portability is promised for your data (OKF export), not for the runtime — there is no supported deployment elsewhere. |
| [0053 Embeddings are the default](0053-embeddings-by-default.md) | **Superseded by 0080** (via 0073) | |

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
| [0065 No authorization; only who a write is recorded as](0065-identity-and-provenance.md) | Accepted; the current record for identity and provenance (carries 0002, 0027, 0032, 0052) | No authorization at all — whoever can reach a deployment can read and write, and headers are read only to record provenance, so trust is judged by the reader from it. The actor comes from the ID token Cloud Run's IAM already verified (`human:` or `process:`). A caller listed in `OCHAKAI_DELEGATING_CALLERS` may name an end user with `Ochakai-On-Behalf-Of`, recorded by composition (`human:x via process:sa`) and never by replacement, since a delegation you cannot tell apart from a direct write is indistinguishable from impersonation; an unlisted caller's header is a 403 rather than a silent downgrade. A caller's self-declared build (`Ochakai-Producer`, MCP `clientInfo`, `OCHAKAI_PRODUCER`) goes in a fourth actor field *beside* the authenticated name, which is the condition on admitting a self-declaration at all. Both web UI proxies always strip a browser-supplied delegation header, and `serve-ui` rebuilds one only from an IAP assertion it verified. Consolidates four records; no decision changes. |
| [0066 Four postures, one word](0066-four-postures-one-word.md) | Accepted; the current record for the deployment posture (carries 0040, 0042, 0060) | Two axes — is the caller identified, can anyone write — give four mutually exclusive postures, spelled with one `OCHAKAI_MODE` (unset / `read-only` / `public` / `dev`) rather than three booleans that could write eight. `read-only` refuses every write at a single point in the service layer regardless of caller, with usage telemetry deliberately outside the freeze; `public` reads no identity at all (every caller is `human:anonymous`, nobody gets a 401) and implies read-only, which is what makes a deployment anyone can reach exposable without IAM in front — an unsigned token forging any identity is the reason reading none is safer than reading unverified ones. An unrecognized value is a startup error, not a guess. Consolidates three records; no decision changes. |
| [0002 Authentication and authorization](0002-authn-authz.md) | **Superseded by 0065** | |
| [0040 Deployment-wide read-only mode](0040-read-only-mode.md) | **Superseded by 0066** | |
| [0042 The public read-only posture](0042-public-read-only.md) | **Superseded by 0066** | |
| [0060 One word for the posture](0060-one-word-for-the-posture.md) | **Superseded by 0066** | |
| [0027 Delegated end-user provenance](0027-delegated-provenance.md) | **Superseded by 0065** | |
| [0052 The self-declaration goes beside the actor](0052-producer-beside-the-actor.md) | **Superseded by 0065** | |
| [0009 OKF/Git round-trips and who owns provenance](0009-provenance-portability.md) | **Accepted** | Provenance is what an instance observed, not a portable attribute: bundles carry knowledge only, and a document's own trust family never reaches a ledger — 0075 §3.1 holds the current statement of that. What stays here is the two refusals it implies (`--preserve-provenance`, and signed provenance between instances — nothing authorizes either without an authorization model), and the ruling that Git is a review route: a round trip into the same instance moves no provenance at all, whoever imports is recorded as the actor, and **a merge is not a verification**. The procedure is [the Git-review guide](../guides/git-review.md) (Japanese). |

## The knowledge model — structure, ids, types, names, links

| Record | Status | Decision |
|---|---|---|
| [0079 Taking the document](0079-taking-the-document.md) | Accepted; the current record for what an import refuses and what the CLI sends; amends [0075](0075-the-bundle-is-the-address-space.md) §§3, 4.2 and [0074](0074-the-document-and-the-vocabulary-that-asks-it.md) §1. **Changes no REST behaviour** | A consumer's job under SPEC §11 is to take the document, and every place ochakai refused one was a place a user's bytes could vanish. **Two of the refusals were ochakai's own rules mistaken for the spec's**: `executor.receipt` was demanded and the untyped §4.1 keys (`title`, `description`, `status`, …) were forced to strings, both citing SPEC §10.2 for requirements §10.2 does not state. A non-string scalar is now read as the text it was written as, with a note; `tags` members are stringified; only unparseable frontmatter and a missing `type` — SPEC §11's own conditions — still refuse a document. A hand-written `index.md`/`log.md` is still regenerated rather than stored, but says so instead of vanishing (which makes `export \| import --strict` of ochakai's own bundle fail — deliberately). `ochakai put` and `ochakai import` send the document's own bytes instead of re-rendering the canonical form from the parsed fields, so a CLI write and a REST write of the same file store the same thing. One finding is **deliberately not fixed**: a document the write path refuses is reported rather than stored, because keeping its bytes would have needed a new rule on `PUT /api/v1/bundle/{path}`, and that wire freezes permanently under [0064](0064-rest-stops-at-api-v1.md) — the source bundle still holds the document and the skip says so. What is fixed is the refusal spreading: files the refused concept's body pointed at are written anyway, attribution being derived rather than stored. |
| [0075 The bundle is the address space](0075-the-bundle-is-the-address-space.md) | Accepted; the current record for the bundle, addressing and the stored form (carries 0017, 0021, 0041, 0046) | ochakai holds one bundle — a path→object map with two kinds of object, concepts (`.md` with a `type`) and files — and **whatever went into the bundle comes out**: a `.md` without a type is kept as a file rather than dropped. The path is the address and the type is an attribute, so layout is the user's (no type is inferred from a path, and a tarball's shape is the structure). What is stored is the bytes received; the canonical form is derived and used only for indexing and for deciding whether the meaning changed, so a reformat moves the ETag but not `generated.at`. Server-owned keys are stripped line-wise and another instance's trust family survives as a claim under `received:`, never entering a ledger. Attribution is derived from the body, `move` carries history, usage and the `<id>/` namespace in one transaction, and `prefix` narrows by address at segment boundaries — explicitly not an access boundary. Consolidates four records; no decision changes. |
| [0074 The document, and the vocabulary that asks it](0074-the-document-and-the-vocabulary-that-asks-it.md) | Accepted; the current record for a concept document's shape and the filter vocabulary (carries 0019, 0022, 0024, 0047, 0061) | `title` is optional (display falls back to the id's last segment) and the id joins the search haystack; only strings compared as keys are NFC-normalized, never content. Links come from markdown in the body — the structured `rel` was a machine-readable type no machine ever read — and `move` rewrites bodies too. Id segments are bounded by path safety alone. `fm.` answers only the keys OKF defines and refuses the five that have typed columns, so a caller can always tell an empty result from a misspelled key. A `dry_run` on the write path returns the real verdict without writing, because half of what `--strict` decides lives on the server. Consolidates five records; no decision changes. |
| [0071 The recommended type vocabulary](0071-the-recommended-type-vocabulary.md) | Accepted; the current record for the type vocabulary (carries 0038, 0063) | Nine recommended types, and the set is never closed — an unlisted type is first-class and nothing migrates. Three things decide the list: whether the spelling is self-explanatory on its own, whether OKF gave it a spelling, and whether anyone actually wrote it. The third is what removed `Playbook` and `API Endpoint`: neither appears in ochakai's own examples or in OKF's reference bundles. The vocabulary is stated in one place and checked from outside. Consolidates two records; no decision changes. |
| [0047 The filter vocabulary is the keys OKF defines](0047-fm-carries-okf-keys.md) | **Superseded by 0074** | |
| [0061 A dry run is the write withheld](0061-a-dry-run-is-the-write-withheld.md) | **Superseded by 0074** | |
| [0046 The bundle is the address space](0046-bundle-address-space.md) | **Superseded by 0075** | |
| [0043 The document is the truth](0043-document-first.md) | **Superseded by 0046, then 0075** | |
| [0041 Narrowing a search by address](0041-path-scoped-search.md) | **Superseded by 0075** | |
| [0037 Making declared expiry and cited sources queryable](0037-stale-and-source-lookup.md) | **Superseded by 0069** | |
| [0036 OKF's schema is ochakai's schema](0036-okf-schema-first.md) | **Superseded by 0043, then 0046, then 0075** | |
| 0034 OKF v0.2 conformance | **Withdrawn — number vacant** | Folded into 0036 and the file deleted before any release carried it; only the document was withdrawn — the implementation shipped. |
| [0005 OKF compatibility and knowledge structure](0005-okf-compatibility.md) | **Superseded by 0036, then 0075** | |
| [0016 Alignment with the knowledge-catalog reference bundles](0016-knowledge-catalog-alignment.md) | **Superseded by 0036, then 0075** | |
| [0017 The path is the address; the type is an attribute](0017-path-addressing.md) | **Superseded by 0075** | |
| [0019 Pre-0.10.0 consistency adjustments](0019-release-review-adjustments.md) | **Superseded by 0074** | |
| [0022 The filename is the name](0022-filename-as-name.md) | **Superseded by 0074** | |
| [0023 One type vocabulary, OKF's](0023-okf-type-vocabulary.md) | **Superseded by 0038, then 0071** | |
| [0038 Realigning the recommended vocabulary](0038-type-vocabulary-realignment.md) | **Superseded by 0071** | |
| [0063 Two unused recommended types leave](0063-two-unused-recommended-types-leave.md) | **Superseded by 0071** | |
| [0057 Concept is the word a reader meets too](0057-concept-is-the-word-a-reader-meets.md) | Accepted; **breaking (wording only)**; the current record for the unit of knowledge's name (carries 0054); §3.2's exclusion of the `entries` JSON key is revoked by [0064](0064-rest-stops-at-api-v1.md) §7, and §0's tool table is amended by [0076](0076-two-tools-leave-mcp.md) | Renames the five MCP tools carrying `knowledge` to OKF's word (`search_concepts`, `get_concept`, `put_concept`, `delete_concept`, `get_concept_usage`; tool count stayed 8 at the time — 0076 has since taken `delete_concept` and `get_concept_usage` off MCP, leaving 6, and 0064 §6 renamed `get_attachment` to `get_file`) and completes that by renaming every word a reader meets — README, docs, examples, CLI help/output, web UI labels, MCP/OpenAPI descriptions, error text — from `entry` to `concept`; identifiers (Go type names, DB columns, the Japanese records) are untouched. The `entries` JSON key was originally excluded too (issue #411 found the exclusion did not hold in two of the three places it applied; 0064 §7 renames it to `concepts`). |
| [0054 The unit of knowledge is called a concept](0054-concept-is-the-okf-word.md) | **Superseded by 0057** | |
| [0024 Links are derived from the body](0024-links-from-body.md) | **Superseded by 0074** | |

## Attachments

| Record | Status | Decision |
|---|---|---|
| [0080 What search fuses, and how a deployment embeds](0080-search-and-how-a-deployment-embeds.md) | Accepted; the current record for search and embeddings (carries 0020, 0053, 0073, 0078) | On Google Cloud the project **and the region** are discovered from the metadata server and semantic search is on by default; IAM decides whether it works, not configuration, and a discovered default degrades to lexical while a named one refuses to start. **Text is embedded in the region the deployment runs in** — a deployment in asia-northeast1 does not send concept bodies and search queries to another continent to be embedded, and where the region cannot be read ochakai embeds nowhere rather than picking a region nobody chose. No fixed default is right: us-central1 exports a Japanese deployment's text, and asia-northeast1 is the same mistake mirrored. One variable says how a deployment embeds: `OCHAKAI_EMBEDDINGS` takes unset/`on`, `off`, or a Vertex AI model resource name — the one string that already carries project, location and model together — and anything else is a startup error naming the three forms. The vector width is a per-model constant (768), not a setting, and a model ochakai knows no width for is refused rather than guessed at; a width change rebuilds the vector tables itself, because **vectors are derived, not records**, and refilling is `reembed`, paid for when asked. Search fuses three lists: lexical (filenames included), concept vectors and file vectors, with a file's hit mapped onto the concept that owns it. Files never come back on their own, and ochakai never interprets them. Consolidates four records; no decision changes, except that 0073 §6's claim of a write-side media-type allowlist is corrected — there is none, and the five embeddable types decide only what is worth an embedder call. |
| [0073 What search fuses, and when embeddings apply](0073-search-and-when-embeddings-apply.md) | **Superseded by 0080** | |
| [0078 One variable says how a deployment embeds](0078-one-variable-says-how-it-embeds.md) | **Superseded by 0080** | |
| [0008 Image attachments](0008-image-attachments.md) | **Superseded by 0046, then 0075** | |
| [0011 Moving attachment bytes to GCS](0011-gcs-attachment-storage.md) | **Superseded by 0046, then 0075** | |
| [0013 Any file type, GCS only](0013-attachment-files-gcs-only.md) | **Superseded by 0046, then 0075** | |
| [0020 Attachment search](0020-attachment-search.md) | **Superseded by 0080** (via 0073) | |

The wire spelling `attachments`/`Attachment` outlived the concept 0046 §2.1
already retired. [0064](0064-rest-stops-at-api-v1.md) §6 is the record
that retires the name itself (`files`/`File` on the wire, `get_file` on
MCP) — it revises none of the records above; the storage model stays
0046's.

## Browse and the web UI

| Record | Status | Decision |
|---|---|---|
| [0072 The web UI: two serving paths, and editing documents](0072-the-web-ui-serves-and-edits-documents.md) | Accepted; the current record for the web UI (carries 0006, 0044) | One self-contained page that knows nothing about authentication, served two ways: `ochakai ui` binds loopback and acts as you, `ochakai serve-ui` is the deployed team service, and `ochakai serve` never serves the UI. Splitting them into two commands is what makes the dangerous combinations unrepresentable. Editing means editing the canonical OKF document as text — a form would need a YAML parser in the browser, and a thicker read shape would teach a form the format does not have. Losing the row editors is a stated cost. Consolidates two records; no decision changes. |
| [0006 Two serving paths for the web UI](0006-web-ui-serving.md) | **Superseded by 0072** | |
| [0014 Folder browse](0014-folder-browse.md) | **Superseded by 0046, then 0075** | |
| [0021 Move, and web UI refinements](0021-move-and-webui-refinements.md) | **Superseded by 0075** | |
| [0044 The web UI edits documents](0044-web-ui-edits-documents.md) | **Superseded by 0072** | |
| [0032 Recording web UI writes under the IAP identity](0032-webui-iap-identity.md) | **Superseded by 0065** | |

## Surfaces — REST, MCP, CLI, web UI

| Record | Status | Decision |
|---|---|---|
| [0067 Four faces, and what each declines](0067-four-faces-and-what-they-decline.md) | Accepted; the current record for how the surfaces divide the work (carries 0004, 0007, 0015, 0033, 0039) | REST is the only contract and the other three are its clients; MCP's tool count is a context budget so its default is no; the CLI is the completeness surface, where completeness means capabilities rather than one command per REST operation; the web UI is a curation surface, not a BI tool. The CLI is a pure REST client (only `serve` touches the database, resolution is `--url` > `OCHAKAI_URL` > `ochakai use`, and there is no `ochakai login`), and `ochakai mcp-stdio` is a transport for the existing MCP face rather than a new one — it forwards JSON-RPC message by message so it never holds a second copy of the tool definitions. `/context`'s `hits` carry only a ranking on every surface, outside the byte budget. Also carries the per-surface list of deliberate omissions — rewritten in the vocabulary that exists today, since the old list still taught `search_knowledge`, `get_attachment` and `POST /api/v1/knowledge`. Consolidates five records; no decision changes. |
| [0068 How a face is added, and how one is removed](0068-how-a-face-is-added-and-removed.md) | Accepted; the current record for the rules that add and retire a face (carries 0050, 0056, 0058, 0062) | Four rules. **One capability, one command** — a second spelling buys convenience, not capability, and only the second one rots (`ochakai backlinks` and `ochakai queues` were folded away). **Two capabilities, two commands** — its converse, which split `ochakai search --sort` into `ochakai list [feed]`. **A listing is not a search** — listings take an opaque keyset `cursor` and return no total count, while a ranking refuses a cursor with a 400 since relevance has no page two. **An entrance nobody arrives through comes down** — `min_score` was removed outright (scores are uncalibrated across modes) and `fm.` came off MCP alone, returning 14% of the tool schemas without changing the tool count. Also carries the ruling face: one `POST /api/v1/review/{id}` taking `ruling`, spelled `withdraw` on every surface. Consolidates four records; no decision changes. |
| [0076 Two tools leave MCP](0076-two-tools-leave-mcp.md) | Accepted; **breaking (MCP)**; amends [0067](0067-four-faces-and-what-they-decline.md) §5.1, §6 and §7 | `delete_concept` and `get_concept_usage` come off MCP, taking the tool count from eight to six — 0068 §3's "an entrance nobody arrives through comes down", applied to whole tools rather than to a parameter. Deleting knowledge is a **ruling**, and this surface does not carry even the reversible rulings (`POST /api/v1/review/{id}`'s verify and reject are not tools): an agent that cannot be trusted to reject a concept cannot be trusted to delete one, and the guard in 0067 §6 only ever protected concepts a human had already ruled on — an agent could still erase its own unread draft. Usage totals are the loop's **human** half (0069); what an agent needs in order to rely on a concept, its trust tier and `verified_at`, already rides on every search hit and every `get_concept`, and it keeps `report_outcome`, the edge of the loop it owns. **Capability is lost from this face only** — REST, the CLI (`ochakai delete`, `ochakai usage`) and the web UI keep both. No deprecation window: MCP is explicitly unstable at 0.x. |
| [0077 One address for a file](0077-one-address-for-a-file.md) | Accepted; the current record for how the CLI names a file | `ochakai attach` and `ochakai detach` are folded into `ochakai put` and `ochakai delete` (CLI 26 → 24, no new flag). They were a second address: they named a file by `<concept id> <name>` rather than by the bundle path it lives at, in a word the rest of the product had retired. A write's kind is decided by its bytes, exactly as the server decides it — frontmatter with a `type` is a concept at `<id>.md`, anything else is a file at the path given — so this is the CLI mirroring REST rather than one command doing two things. A delete has no bytes, so the argument's spelling picks which of the two addresses to ask first and the other is tried only when the first holds nothing. The relative markdown link `attach` printed is kept by `put`: attribution is derived from bodies, so a file nothing links is a file nobody finds. What folds away is convenience — several files per invocation (a shell loop) and `--name` (the path is the name). The web UI's buttons follow the CLI's word, which is what 0064 §8 asked for when it kept them symmetric. |
| [0004 The remote CLI](0004-cli.md) | **Superseded by 0067** | |
| [0007 Retiring the direct-database commands](0007-api-only-cli.md) | **Superseded by 0067** | |
| [0015 Surface consistency](0015-surface-consistency.md) | **Superseded by 0067** | |
| [0058 Two filters nobody arrived through](0058-filters-nobody-arrived-through.md) | **Superseded by 0068** | |
| [0033 Context hits are a ranking](0033-context-hits-are-a-ranking.md) | **Superseded by 0067** | |
| [0082 What the freeze holds still](0082-what-the-freeze-holds-still.md) | Accepted; amends [0064](0064-rest-stops-at-api-v1.md) §11 | The freeze covers **addresses, the shape of a request, and removals or changes in a response** — adding a property to a response-only schema was never what it was holding still. 0064 §2 closed unknown query keys with 400 expressly to decide "whether anything can be safely added after the freeze"; on that footing a client ignores a response key it does not know, and `required` in a response is a promise the server makes rather than a demand on the caller. The restriction to response-*only* schemas is the one subtlety: a schema reachable from a `requestBody` cannot grow a required property without producing the 400 that 0064 §2 introduced, and which schemas those are is derivable from the contract. Additions still regenerate `api/openapi.frozen.txt`, so they still appear in a diff a reviewer reads — what changes is which regenerations are legitimate and what the failure says. Prompted by C7: the loop's own instruments (dropped observations, outcome coverage, throughput) have nowhere to go but `stats`, and the CLI and web UI are REST clients (0067 §2), so the freeze was holding the product's central claim still rather than its contract. Requests, removals, renames and type changes are untouched. |
| [0083 An error carries a code](0083-an-error-carries-a-code.md) | Accepted | Every error response now carries `code` beside `error`: the sentence is for a person and stays free to be reworded in any release, while the code is a closed vocabulary of twelve words a client may branch on. Three conditions answer 409 — `already_exists`, `not_deleted`, `no_rejection` — and telling them apart meant matching prose that `docs/compatibility.md` explicitly allows to move, so an integrator embedding the REST API (C6) had only a method known to break. The code repeats the condition even where the status already determines it: a key present on only some errors forces a client to ask whether this failure has one, which is not something you can switch on. The mapping lives in one place and is read from the sentinels the service already returns, so the sixty-odd call sites are unchanged. RFC 9457 (`application/problem+json`) is declined for three reasons: it replaces the media type, which is the part of the contract 0064 froze hardest; its `type` URI would put a domain's survival inside the contract, which a product whose first condition is that the knowledge stays the user's cannot do; and it carries the same information under more keys. This is an addition to a response-only schema, which [0082](0082-what-the-freeze-holds-still.md) places outside the freeze — the golden fingerprint is regenerated in the same PR. VOCAB rises 34 → 46. |
| [0064 REST stops at /api/v1](0064-rest-stops-at-api-v1.md) | Accepted; amends [0046](0046-bundle-address-space.md) §3.5, [0057](0057-concept-is-the-word-a-reader-meets.md) §3.2, [0071](0071-the-recommended-type-vocabulary.md) §1 and [0074](0074-the-document-and-the-vocabulary-that-asks-it.md) §§3, 7 | The last breaking REST batch before the wire is declared frozen (issue #379); `docs/compatibility.md` is rewritten in the same PR to say so. **Breaking**: unknown query parameters now 400 on every operation (`fm.` excepted); `Ochakai-On-Behalf-Of`/`Ochakai-Producer` lose their `X-` prefix; a file's own address answers `Accept: application/json` with metadata instead of ignoring the header; `attachments`/`Attachment` are renamed to `files`/`File` on the wire and the MCP tool `get_attachment` becomes `get_file`; the `entries` JSON key on `stats`, the bundle listing and `context`/`get_context` is renamed to `concepts`, revoking 0057 §3.2's exclusion of it (issue #411 — the exclusion's reasoning did not hold for two of the three places it applied); the `change` vocabulary's last retired-word holdouts, `attach`/`detach`, are renamed to `add_file`/`remove_file` with a migration rewriting stored rows (§8, issue #470); `/context`'s redundant `truncated` count is dropped (it always equaled `len(outline)`), the bundle listing's file rows say `created_at` instead of `updated_at` (files are content-addressed and immutable), and `reembed`'s `embedded` is renamed to `concepts` to match its sibling `files` (§9, issue #470); a failed `If-None-Match: *` is 412 not 409; DELETE on a concept honors `If-Match`; a file PUT answers 201 on creation; `ruling: withdrawn` with nothing to withdraw is 409 not 404; out-of-range `limit`/`days` is 400 everywhere instead of silently clamping. `.md` is mandatory and part of a concept's bundle path, not a representation selector; `Change.path` is the full bundle path, not bare. Amends 0046 §3.5's representation table: a concept's default (no `Accept`) is the JSON View, not the export form. Also breaking, and the last chance for each: `title` is optional everywhere on the wire (OKF SPEC §4.1 gives it no default, so a resolved title asserted a value the document never declared; `status` stays resolved because SPEC §5.4 does give *it* one), `observed.generated.at` reports the content's last meaningful change rather than the row's write time (SPEC §5.2 — the exported document always rendered the right one and only the JSON differed), and a `type` may contain `/` (SPEC §4.1/§11 require tolerating unknown types; the ban was ochakai's own, amending 0071 §1 and 0074 §§3, 7 on that one point). Additive in the same batch: the PUT finally declares a `requestBody` — no generator could build a write client without one, and the contract test could not see the gap because it skipped every bundle PUT — plus the `Ochakai-Read-Only`, 400, 413, `Cache-Control`, `X-Content-Type-Options`, `Content-Security-Policy` and 304 `ETag` declarations the server already sent (HEADER 10 → 13). Only a security defect justifies breaking the freeze after this point; a repeated DELETE on an already-gone id stays 404, deliberately, and is safe to retry. No version or capability signal ships, and CORS stays deliberately absent.

## The verification loop and usage measurement

| Record | Status | Decision |
|---|---|---|
| [0069 The loop, and what measures it](0069-the-loop-and-what-measures-it.md) | Accepted; the current record for the verification loop and usage measurement (carries 0025, 0029, 0037, 0049, 0051, 0059) | Three queues a curator can empty — unreviewed drafts, unanswered failure reports, concepts past their declared expiry — beside `sort=verified_at`, which is a ranking nobody can zero and is deliberately not counted. Evidence-based staleness sits next to time-based: a `failed` report outranks age, and the feed drops an entry once it is verified again. `stale_after` is the writer's own declaration, so verifying does not clear it — only re-declaring the date does. Usage events are buffered off the read path and are explicitly best-effort; a search that returns nothing becomes a row of its own (zero hits, never a low score), capped at 500 bytes and pruned at 180 days, off on a public deployment. `GET /api/v1/stats` answers the instance in one call, separating state from flow, with `prefix` scoping everything except misses — a miss has no id to hang off. No rollups, no scheduler, no delivery: the exit code and the counts are what an operator's cron reads. Consolidates six records; no decision changes. |
| [0050 Listings page, rankings do not](0050-listings-page-rankings-do-not.md) | **Superseded by 0068** | |
| [0062 A listing is not a search](0062-a-listing-is-not-a-search.md) | **Superseded by 0068** | |
| [0056 One question, one command; one ruling, one word](0056-one-question-one-command.md) | **Superseded by 0068** | |
| [0055 One ruling, one face](0055-one-ruling-one-face.md) | **Superseded by 0056, then 0068** | |
| [0049 Counting the review queues](0049-queue-counts.md) | **Superseded by 0069** | |
| [0059 A queue is named by the listing that shows it](0059-a-queue-is-named-by-its-listing.md) | **Superseded by 0069** | |
| [0025 Closing the write-back loop](0025-closing-the-loop.md) | **Superseded by 0069** | |
| [0029 Usage recording off the read path](0029-usage-recording-off-the-read-path.md) | **Superseded by 0069** | |
| [0051 Recording the questions nothing answered](0051-instance-metrics-and-search-misses.md) | **Superseded by 0069** | |

## Concurrency and deletion

| Record | Status | Decision |
|---|---|---|
| [0030 Optimistic locking with If-Match](0030-optimistic-locking.md) | Accepted | Conditional updates are opt-in, exposed as an ETag and accepted as `If-Match`; a mismatch is a 412 with nothing written. Send `If-Match` and handle 412; omitting it keeps last-write-wins behavior. |
| [0031 Purge](0031-purge.md) | Accepted | A second deletion stage that discards an already soft-deleted entry and frees its id, refusing a live one; GCS bytes aren't reclaimed since blobs are content-addressed and may be shared. Irreversible, REST/CLI only — an export is the only way back. |

## Semantic models and compile

| Record | Status | Decision |
|---|---|---|
| [0070 What was retired, and the bar for retiring it](0070-what-was-retired-and-why.md) | Accepted; the current record for what ochakai decided not to do (carries 0012, 0018, 0028) | Three faces came down — the public MCP OAuth connector, deterministic SQL compilation, and any dedicated mechanism for semantic models. In none of them was the design wrong; what flipped was the cost side, and it flipped the same way each time: no users materialized, the documentation had already demoted the feature, and the maintenance surface kept accruing (the connector alone carried the deployment's only secret and its only public service). **Retiring is maintenance, not failure.** No user knowledge is deleted: retired types stay first-class as free types and past usage rows stay. Consolidates three records; no decision changes. |
| [0018 Retiring import-ossie](0018-semantic-model-as-knowledge.md) | **Superseded by 0070** | |
| [0028 Retiring compile_sql](0028-retire-compile-sql.md) | **Superseded by 0070** | |

## The MCP OAuth connector, and what replaced it

| Record | Status | Decision |
|---|---|---|
| [0010 An MCP OAuth connector service](0010-mcp-oauth-connector.md) | **Superseded by 0012, then 0070** | |
| [0012 Retiring the connector](0012-retire-mcp-oauth-connector.md) | **Superseded by 0070** | |
| [0039 A stdio bridge for MCP clients](0039-mcp-stdio-bridge.md) | **Superseded by 0067** | |

---

Two numbers are vacant. **0026** was a proposal to replace Cloud SQL with
Cloud Storage as the only persistence layer; it never landed, so it lives
in its pull request. **0034** is described above.
