# FAQ

Answers that the README implies but never states in one place.

Several questions here are really about an area another page owns, and
those get a short answer and a link rather than a second copy — **"why
not just use X?" is [positioning](positioning.md), every symptom is
[troubleshooting](guides/troubleshooting.md) (Japanese), every setting is
[requirements and configuration](configuration.md) (Japanese).** Two texts
saying the same thing means one of them is out of date and nobody can
tell which.

### Can I run this without Google Cloud?

Not in production: Cloud Run's IAM check is the access model and Cloud
SQL IAM is the database credential, which together are why ochakai holds
no secret of its own. `deploy/compose.yaml` runs the same binary locally
against plain PostgreSQL with authentication disabled — a development
harness, not the small end of production. What is portable is the
knowledge, not the runtime. In full:
[requirements](configuration.md#requirements) (Japanese).

### Does my data leave my project?

No. ochakai reports nothing anywhere — there is no telemetry, and so
nothing to opt out of. The only hosts it ever contacts are Google Cloud
APIs in your own project: Cloud SQL always, GCS if you set
`OCHAKAI_GCS_BUCKET`, and Vertex AI where semantic search is enabled —
which, running on Google Cloud, is the default (design doc
[0073](design/0073-search-and-when-embeddings-apply.md)).
`OCHAKAI_EMBEDDINGS=off` — the one variable that says how a deployment
embeds (design doc [0078](design/0078-one-variable-says-how-it-embeds.md))
— declines it, and so does simply not granting `roles/aiplatform.user`;
either way an instance then talks to its database and nothing else.

Usage counts are rows in your own database. They exist so a reviewer can
see which knowledge is earning its place, and they never leave it.

### How is it different from RAG, a memory layer, or a markdown vault?

One page answers all six neighbours at once, says where ochakai loses,
and says who should pick something else:
[positioning](positioning.md) — semantic layers, catalogs, memory
layers, RAG, the verified-query store inside an AI-analyst product, and
a vault of markdown notes with an MCP server.

The one-line version: *memory layers remember what happened; ochakai
curates what's true* — and against a vault, the difference is
everything except the file format, because an `ochakai export` bundle
**opens as a vault** ([positioning](positioning.md#a-markdown-vault-with-an-mcp-server)).

### Do I need embeddings?

Usually yes, which is why you get them without asking: running on Google
Cloud, ochakai finds its own project and turns hybrid search on, provided
its service identity may call Vertex AI. They earn their keep when your
knowledge base is in Japanese, when questions arrive as sentences rather
than keywords, or when you attach images and PDFs you want searchable by
content. Search still works without them — lexical-only, calling no
external API. What that costs, and why Japanese is the case that needs
them, is in
[architecture's search section](architecture.md#search) (Japanese);
the three ways to decline are in
[configuration](configuration.md#environment-variables) (Japanese).

### Can an agent overwrite or delete knowledge a human verified?

Not over MCP. Deleting is not a tool at all — design doc
[0076](design/0076-two-tools-leave-mcp.md) took `delete_concept` off that
surface, because deleting knowledge is a ruling and MCP does not carry
even the reversible rulings. `put_concept` refuses a concept a human has
ruled on — verified, rejected or deprecated — and the refusal says what to
do instead:

> cannot update metrics/revenue from this surface: it is verified, and
> this surface has no If-Match precondition to replace curated knowledge
> safely. If it is wrong, say so with report_outcome failed — that puts it
> in the re-verification feed. If you have something better,
> put_concept a new draft. A human changes curated concepts from the
> web UI or CLI.

This is not authorization — a human on the same deployment can edit
anything from REST, the CLI or the web UI. It is a surface rule: MCP has
no way to carry the `If-Match` precondition that makes a safe replacement
expressible (design docs 0067 §6, 0030). Reviving a curated concept's
tombstone with `put_concept` is refused on MCP for the same reason: it would
put a fresh draft where a rejection's recorded reason used to be. On REST
and the CLI that revival is allowed — those are the surfaces a human
curates from.

### What happens when two people edit the same concept?

Last write wins, unless the client asks for better. Every `GET` and `PUT`
returns an `ETag` — the hash of the concept's canonical OKF document,
quoted, and also in the body as `summary.content_hash` — and a `PUT` carrying
`If-Match` with a stale value gets `412` and writes nothing (design docs
0030, 0075 §3.2). It is a hash of the content alone, so verifying or
rejecting the concept, or attaching a file to it, leaves your precondition
valid: only an edit invalidates it. MCP
exposes no version field but uses the same mechanism internally to protect
curated concepts.

### Who can read and write?

Anyone who can reach the deployment — reachability is the whole access
model, and there is no authorization on top of it. What identity ochakai
records, and the postures (`read-only`, `public`) that narrow what a
reachable caller can do, are in
[requirements and configuration](configuration.md#authentication-has-no-configuration)
(Japanese).

### What happens to my knowledge if I stop using ochakai?

`ochakai export ./knowledge` writes the whole base as a git-friendly OKF
v0.2 bundle that another OKF consumer reads without knowing ochakai
exists; `ochakai import` is the inverse, for any producer's bundle, not
just ours. What does and does not survive the round trip — provenance
does not — is in
[operating a deployment](guides/operating.md#backup-and-restore)
(Japanese).

### Is there a hosted version?

No. ochakai is self-hosted per tenant; there is no service to sign up for
today.

### Does it connect to my warehouse, or run SQL?

Neither. ochakai holds no warehouse credentials and executes nothing. It
stores the sanctioned computation — the SQL in a `# Computation` fence,
the contract in `runtime` / `parameters` / `executor` / `attester` — and
hands it back verbatim. Your agent runs it. Even for an Attested
Computation, which names how a run can be checked, ochakai records the
contract and never fetches or executes any part of it.

### How large a knowledge base does this hold?

Nobody has measured a ceiling, and the honest answer is that ochakai is
built for the scale a *curated* base reaches — thousands of concepts, not
millions, because every one of them passed a human. What has been
measured, and what has not, is in
[operating a deployment](guides/operating.md#capacity) (Japanese). If you
are
planning a deployment materially larger than that, say so in
[Discussions](https://github.com/na0fu3y/ochakai/discussions) — it is the
kind of thing that should be measured before it is promised.

### Can I try it without installing a Go toolchain?

Yes, two ways. Take a
[release archive](https://github.com/na0fu3y/ochakai/releases) — linux,
macOS and Windows on amd64 and arm64, with checksums and build provenance
— or skip the client entirely and talk to the REST API, which is all the
CLI does.
[The data model](architecture.md#the-data-model) (Japanese) opens with
a `curl` that creates a concept.

### What is a "concept", exactly?

One markdown document with YAML frontmatter, addressed by a path-like id
(`queries/sales/monthly-revenue`). The id is the address and the type is
an attribute, not a directory (design doc 0075 §2). Relationships come from
ordinary markdown links in the body — there is no links field to fill in
(design doc 0074 §2) — and a `title` is optional, because the last segment of
the id already names the concept (design doc 0074 §1).
