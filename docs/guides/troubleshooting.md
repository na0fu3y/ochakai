# Troubleshooting

Symptoms on the local and client side. Google Cloud symptoms — the GFE
intercepting `/healthz`, `NOT_AUTHORIZED`, IAM propagation delays — are
[§7 of the deploy guide](../../deploy/cloudrun/README.md). MCP clients
that will not connect at all are in
[Connecting an MCP client](mcp-clients.md).

Start with `ochakai whoami`. It prints the whole client-side situation in
four lines — which server, from where that choice came, as whom, and
whether the server answers:

```
server:    http://localhost:8080 ($OCHAKAI_URL)
identity:  human:anonymous (plain http, no credentials)
health:    ok
mode:      read-write
```

## The CLI

**`ochakai: unknown command "--url"`.** Flags go after the subcommand, not
before it: `ochakai whoami --url http://localhost:8080`, never
`ochakai --url … whoami`. The first word after `ochakai` is always a
command.

**`health: error: … connect: connection refused`.** The server named on
the `server:` line is not listening. That line also says where the choice
came from — `$OCHAKAI_URL`, `--url`, or the `ochakai use` selection — which
is usually the actual bug: a shell that still exports `OCHAKAI_URL` from an
earlier session overrides `ochakai use` every time.

**Writes fail with 403 and `mode:` says `read-only`.** The deployment sets
`OCHAKAI_READ_ONLY`. Every write is refused, including yours as the
operator; it is a property of the deployment, not of the caller.

## Search

**`search needs a query`, HTTP 400.** A search wants either a query or a
`sort` that lists without one (`verified_at`, `usage`, `failed`,
`stale_after`), or a `--source` to list what cites a resource. Listing
everything is deliberately not a mode.

**Search returns nothing on a base that has entries.**

- *A Japanese query of only hiragana returns no hits.* The lexical half
  cuts Japanese into two-character windows and keeps only the ones
  carrying a kanji or katakana, because an all-hiragana window is grammar
  and would match nearly everything. Search for the noun, not the
  particle — or turn embeddings on.
- *An English question returns the wrong entries.* Lexical search is a bag
  of words: an entry sharing three function words can outrank the one that
  names the subject. This is the case embeddings fix
  (`OCHAKAI_VERTEX_PROJECT`).
- *Nothing matches a term you know is stored.* Rejected and soft-deleted
  entries are excluded from default search, and `--status` narrows
  further. Drop the filters before concluding the entry is missing.
- *Embeddings are on but ranking did not change.* Vectors are written when
  an entry is written, so entries that predate the setting have none. Run
  `ochakai reembed`.

**Scores look meaningless.** They are an ordering, not a measure, and they
are not comparable between the lexical and hybrid modes. To bound a
response, use `budget` rather than a score floor.

## Writing entries

**409 on create.** A live entry already has that id — including a
`rejected` one. Use `update`, or pick another id. Creating over a
*soft-deleted* entry revives it, with its prior history intact; over MCP,
reviving one that was verified, rejected or deprecated before deletion is
refused instead, because it would put a fresh draft where a recorded
ruling used to be. REST and the CLI allow that revival: they are the
surfaces a human curates from.

**404 on update.** `update` replaces an entry that exists; it does not
create one.

**412 on update.** The `If-Match` you sent is not the entry's current
`ETag` — somebody wrote in between. Re-read the entry, reapply your change
and retry. Nothing was written.

**`invalid type "…"`.** A type is one line, no `/`, up to 128 bytes. Any
value that fits is legal — the recommended vocabulary is a recommendation,
not a closed set — but a multi-line string is not.

**An agent gets `cannot update … it is verified`.** Working as intended:
MCP refuses to overwrite or delete curated entries. See the
[FAQ](../faq.md#can-an-agent-overwrite-or-delete-knowledge-a-human-verified).

## Import and export

**`import` reports files skipped.** Every skip is printed to stderr with a
`skip:` prefix and counted in the summary
(`imported N entries (… , M skipped)`). The reasons:

- the file has no `type` in its frontmatter — the type is never inferred
  from the path (design doc 0017);
- it is a reserved `index.md` or `log.md`, or a hidden path;
- the server rejected it, in which case the message carries the server's
  own complaint, and the rest of the bundle still imports;
- it is an attachment whose entry was not imported.

**Everything imported one directory deeper than expected.** The packed
shape is the structure: an archive wrapped in a single directory imports
under that directory, so a bundle keeps its own namespace. That is
deliberate — unwrap the archive first if you did not want it.

**Import says `unchanged`.** Entries identical to what is stored are left
alone rather than rewritten, so a re-import does not fill the revision
history with copies.

## Attachments

**501 on attach.** The instance has no `OCHAKAI_GCS_BUCKET`, so it stores
markdown entries only. This is a whole-deployment setting, not a
per-request one.

**A file is refused.** The media type is decided by sniffing the bytes,
never by what the client claims: png, jpeg, webp, pdf and plain text are
accepted, and 5 MiB per file / 20 files per entry are the limits. HTML and
SVG are refused on purpose — both can carry script into the web UI.

**Attachment contents do not turn up in search.** Filenames match in every
search, but contents join only when embeddings are on, and images and PDFs
need a file-capable model (`gemini-embedding-2`, with
`OCHAKAI_VERTEX_LOCATION` set to `global`, `us` or `eu`). Attachments that
predate the setting are not backfilled — `ochakai reembed`.

## The web UI

**The UI is empty though the API has entries.** `ochakai ui` serves the UI
against the server *it* selected, which is not necessarily the one your
last curl went to. Its startup line and `ochakai whoami` both say which.

**Edits are recorded as the wrong identity.** `ochakai ui` acts as you on
loopback. A deployed `ochakai serve-ui` records the service account
instead, unless `OCHAKAI_IAP_AUDIENCE` is set and the webui's service
account is listed in the server's `OCHAKAI_DELEGATING_CALLERS`, which is
what turns browser edits into `human:you via agent:webui-sa` (design doc
0032).

## Startup

**The server refuses to start after an embedding change.** Changing
`OCHAKAI_EMBEDDING_DIM` on a database that already holds vectors is
refused rather than silently corrupting them. Put it back, or drop the
vector tables and re-embed.

**`pgvector extension is required for semantic search`.** The database
user may not `CREATE EXTENSION`. Create `vector` once as an admin — the
deploy guide's §3 bootstrap SQL does exactly this — or unset
`OCHAKAI_VERTEX_PROJECT` and run lexical-only.

**Logs say `attachments disabled` or `semantic search disabled`.** Those
are the startup lines confirming which optional subsystems are off. They
are informational, and they are the fastest way to check what a running
instance actually has enabled.
