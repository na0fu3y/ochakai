# Compatibility and support

The short version, so nobody has to infer it:

> **REST is frozen at `/api/v1`.** Everything else is still unstable while the
> major version is 0: MCP, the CLI and the stored shape may all change in a
> minor release. There is no deprecation window for any of them. Only the
> latest release is supported. Only a security defect can still break it.

That is the actual policy, not a disclaimer. If you are deciding whether
to build on ochakai, decide against *this*, not against what "SemVer"
usually implies at 0.x.

## What "unstable" covers

Everything a client can touch, except the one line below that no longer
moves:

- **REST is frozen.** [0064](design/0064-rest-stops-at-api-v1.md) closed
  the last batch of breaking changes — an unrecognized query parameter is
  now a 400 naming it rather than a silent no-op, which is what makes it
  safe to add one later without breaking a client that predates it. That
  batch is also where the last *value* changes had to go, because a
  response's shape is what a validating client holds: `title` became
  optional the way OKF SPEC §4.1 always had it,
  `observed.generated.at` became the content's instant rather than the
  row's, and a `type` may now contain `/`, which SPEC §4.1 and §11
  require a consumer to tolerate. The changelog entry says what a client
  does about each. Before
  the freeze, REST moved the same way everything else did: `0.13.0` moved
  the knowledge out of `/context`'s `hits`; `0.14.0` moved seven keys out
  of `attrs` into envelope fields; the largest move,
  [0075](design/0075-the-bundle-is-the-address-space.md) §4's fold, left an object
  with one address — `/api/v1/bundle/{path}` — and removed the second
  spellings it replaced (`/api/v1/knowledge/{id}`,
  `/api/v1/attachments/{id}/{name}`, `/api/v1/backlinks/{id}`,
  `/api/v1/export`) rather than deprecating them. That history does not
  repeat: [api/openapi.yaml](../api/openapi.yaml) is now the address list
  a client can hold onto. **The freeze is checked, not promised.**
  [api/openapi.frozen.txt](../api/openapi.frozen.txt) is a fingerprint of
  everything that file lets a client observe — every operation, parameter,
  header, status code and schema field — and CI fails when the two disagree
  (`cmd/ochakai/frozenwire_test.go`), so a rename that keeps every count
  still cannot land quietly. Prose is deliberately outside it: the
  documentation in the contract stays free to improve.
- **MCP** — tool names, arguments, and which tools exist at all.
  `compile_sql` existed until `0.13.0` and does not now, and the five
  tools that said `knowledge` say `concept`.
- **The CLI** — commands, flags, and the shape of what they print.
- **The stored data** — migrations run automatically at server start, and
  they are one-way. There is no downgrade path.

The OKF export format is the one part with an external anchor: it follows
[OKF v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf),
so it changes when the spec does or when ochakai's reading of it does —
`0.14.0` changed the key order in exported frontmatter, which is a diff in
anything that stores bundles in git.

## What you get instead of stability

Not a promise that nothing breaks. A promise that you are told what did:

- **Every breaking change to MCP, the CLI or the stored shape is marked
  `BREAKING` in [the changelog](../CHANGELOG.md)**, with what an operator has
  to do about it — which migration runs, whether `updated_at` moves, whether
  re-embedding is needed, what a client reading the old shape must change.
- **Changes arrive in batches.** A minor release usually carries several
  at once rather than trickling them out, so upgrading is one reading of
  one changelog entry rather than a standing tax. `0.15.0` landed five
  design records together, including the type-vocabulary realignment and
  two new deployment postures.
- **Every removal from MCP, the CLI or the stored shape has a design record**
  that says why, and what to use instead. `compile_sql` went with
  [0070](design/0070-what-was-retired-and-why.md) §3, the OAuth connector with
  the same record's §2. Features are removed when
  they stop earning their place, and that is treated as maintenance rather than
  as a failure.
- **Your knowledge outlives any of it.** `ochakai export` writes an OKF
  bundle that another tool can read, which is the guarantee the project
  actually makes.

## Support

**Only the latest release.** There is no support window, no backporting,
and no long-term branch: a fix ships in the next release, and the way to
get it is to upgrade. This is also what
[SECURITY.md](../SECURITY.md) means by "the latest release is the
supported version" — it applies to security fixes too.

Releases before `0.9.0` are
[retracted](https://go.dev/ref/mod#go-mod-file-retract) in `go.mod` and
unsupported.

## Living with it

If you are running ochakai anyway — and the project is used this way — the
practices that make an unstable dependency survivable:

- **Pin a version.** The deploy guide pins an image tag rather than
  `:latest`, so a redeploy is a decision and an upgrade is a separate one.
- **Read the changelog before upgrading**, and read the whole entry: the
  breaking notes are written for the person doing exactly that.
- **Keep an export.** `ochakai export` is both your backup and your exit
  ([operating guide](guides/operating.md) (Japanese)).
- **Run the [golden query canary](guides/golden-query-canary.md)
  (Japanese) from CI**, so a knowledge base that quietly stopped matching
  reality says so without waiting for someone to notice.
- **Upgrade the CLI with the server.** They are versioned together, and a
  client one minor behind may be reading a shape that moved.

## 1.0

Still not scheduled, but no longer "no criteria have been set" — REST
reaching a frozen `/api/v1` is the first criterion actually met, recorded
in [0064](design/0064-rest-stops-at-api-v1.md). MCP, the CLI and the
stored shape are not there yet, so the paragraph at the top of this page
still applies to them. When each of the rest stops moving, that will be
its own decision, recorded the same way.
