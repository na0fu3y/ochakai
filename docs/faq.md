# FAQ

Answers that the README implies but never states in one place. When a
question is really about a symptom, it is in
[troubleshooting](guides/troubleshooting.md) instead.

### Can I run this without Google Cloud?

Not in production. Cloud Run's IAM check is the access model and Cloud SQL
IAM is the database credential, which together are why ochakai holds no
secret of its own (design docs 0002, 0003) — a deployment elsewhere would
have to invent both, and that is the thing the design refuses. Locally,
`deploy/compose.yaml` runs the same binary against plain PostgreSQL with
`OCHAKAI_INSECURE_DEV=true`, which disables authentication and records
everyone as `human:anonymous`. That is a development harness, not the
small end of production.

What is portable is the knowledge: `ochakai export` writes an OKF bundle
of markdown and YAML that any OKF consumer can read, so leaving does not
depend on the runtime you are leaving.

### Does my data leave my project?

No. ochakai reports nothing anywhere — there is no telemetry, and so
nothing to opt out of. The only hosts it ever contacts are Google Cloud
APIs you turn on yourself: Cloud SQL always, plus GCS if you set
`OCHAKAI_GCS_BUCKET` and Vertex AI if you set `OCHAKAI_VERTEX_PROJECT`.
With neither set, an instance talks to its database and nothing else.

Usage counts are rows in your own database. They exist so a reviewer can
see which knowledge is earning its place, and they never leave it.

### How is this different from RAG over our documents?

RAG retrieves passages from documents somebody wrote for other reasons;
what comes back is as right as whatever was in the wiki. ochakai returns
entries a human marked `verified`, with provenance saying who wrote it,
who checked it and when, and a feed that resurfaces knowledge which has
gone too long unchecked or came back wrong. The unit is a reviewed claim,
not a chunk.

The other difference is direction. A RAG index is built from documents; a
knowledge base here is written *by the agents using it* and promoted by a
human — the write-back loop is the product, and a rejected proposal keeps
its reason so agents stop re-proposing it.

### How is it different from a memory layer (mem0, Zep, Letta)?

Memory layers extract per-user memories with an LLM and inject them back
unaudited: nobody reviews what got remembered, and a wrong memory persists
quietly. ochakai is team-shared knowledge that passes through human
review. They compose — preferences in your memory layer, verified data
knowledge here.

### Do I need embeddings?

No. Search is lexical by default and calls no external API. Turn
embeddings on (`OCHAKAI_VERTEX_PROJECT`) when your knowledge base is in
Japanese, when questions arrive as sentences rather than keywords, or
when you attach images and PDFs you want searchable by content.

The reason Japanese is called out: PostgreSQL's full-text search does not
tokenize it, so a Japanese query is matched by two-character windows
against a stored haystack. That finds the right entry for a term, but it
is a bag of words, and it is answered by scanning rather than from an
index — about 16 ms across 5000 entries.

### Can an agent overwrite or delete knowledge a human verified?

Not over MCP. `put_knowledge` and `delete_knowledge` both refuse an entry
a human has ruled on — verified, rejected or deprecated — and the refusal
says what to do instead:

> cannot update metrics/revenue from this surface: it is verified, and
> this surface has no If-Match precondition to replace curated knowledge
> safely. If it is wrong, say so with report_outcome failed — that puts it
> in the re-verification feed. If you have something better,
> put_knowledge a new draft. A human changes curated entries from the
> web UI or CLI.

This is not authorization — a human on the same deployment can edit
anything from REST, the CLI or the web UI. It is a surface rule: MCP has
no way to carry the `If-Match` precondition that makes a safe replacement
expressible (design docs 0015 §3.1, 0030). Reviving a curated entry's
tombstone with `create` is refused on MCP for the same reason: it would
put a fresh draft where a rejection's recorded reason used to be. On REST
and the CLI that revival is allowed — those are the surfaces a human
curates from.

### What happens when two people edit the same entry?

Last write wins, unless the client asks for better. Every `GET` and `PUT`
returns an `ETag` — the hash of the entry's canonical OKF document,
quoted, and also in the body as `summary.content_hash` — and a `PUT` carrying
`If-Match` with a stale value gets `412` and writes nothing (design docs
0030, 0043 §3.4). It is a hash of the content alone, so verifying or
rejecting the entry, or attaching a file to it, leaves your precondition
valid: only an edit invalidates it. MCP
exposes no version field but uses the same mechanism internally to protect
curated entries.

### Who can read and write?

Anyone who can reach the deployment. ochakai identifies the caller from
what Cloud Run forwards, records it as provenance, and does no
authorization at all — no roles, no per-entry permissions, no read-only
users. Deciding who may reach it is Cloud Run IAM's job, and running the
service publicly invokable is a misconfiguration rather than a deployment
mode (design doc 0002).

If you need a deployment that serves knowledge without changing it, that
is `OCHAKAI_READ_ONLY` (design doc 0040) — a property of the deployment,
not of the caller. It refuses the operator too.

### What happens to my knowledge if I stop using ochakai?

`ochakai export ./knowledge` writes the whole base as an OKF v0.2 bundle:
one markdown file per entry with YAML frontmatter, attachments as plain
files beside them, trust and lifecycle in the spec's own keys. It is a
git-friendly directory that another OKF consumer reads without knowing
ochakai exists. `ochakai import` is the inverse, and it accepts any
producer's bundle, not just ours.

Provenance is the one thing that does not travel: it is what an instance
observed, not a claim a document can carry, so an import records the
importer rather than replaying history (design doc 0009).

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
built for the scale a *curated* base reaches — thousands of entries, not
millions, because every one of them passed a human. The only published
figure is the search cost quoted above (about 16 ms per Japanese search
across 5000 entries, against 0.2 ms for a latin word). If you are
planning a deployment materially larger than that, say so in
[Discussions](https://github.com/na0fu3y/ochakai/discussions) — it is the
kind of thing that should be measured before it is promised.

### Can I try it without installing a Go toolchain?

Yes, two ways. Take a
[release archive](https://github.com/na0fu3y/ochakai/releases) — linux,
macOS and Windows on amd64 and arm64, with checksums and build provenance
— or skip the client entirely and talk to the REST API, which is all the
CLI does. The README's [Requirements](../README.md#requirements) section
has a `curl` that creates an entry.

### What is an "entry", exactly?

One markdown document with YAML frontmatter, addressed by a path-like id
(`queries/sales/monthly-revenue`). The id is the address and the type is
an attribute, not a directory (design doc 0017). Relationships come from
ordinary markdown links in the body — there is no links field to fill in
(design doc 0024) — and a `title` is optional, because the last segment of
the id already names the entry (design doc 0022).
