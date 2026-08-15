# Compatibility and support

The short version, so nobody has to infer it:

> **REST is frozen at `/api/v1`.** Everything else is still unstable while the
> major version is 0: MCP, the CLI and the stored shape may all change in a
> minor release. **A name gets one release** — an MCP tool or a CLI command a
> release renames keeps answering under its old spelling until the next
> release, and nothing else gets a window
> ([0088](design/0088-a-retired-name-answers-for-one-release.md)). Only the
> latest release is supported. Two things can still break REST — a security
> defect, and output that does not conform to OKF
> ([0100](design/0100-md-is-how-a-concept-is-spelled.md)) — though a *new
> field in a response* was never a break, and is allowed
> ([0082](design/0082-what-the-freeze-holds-still.md)).

That is the actual policy, not a disclaimer. If you are deciding whether
to build on ochakai, decide against *this*, not against what "SemVer"
usually implies at 0.x.

## What "unstable" covers

Everything a client can touch, except the one line below that no longer
moves:

- **REST is frozen.** [0064](design/0064-rest-stops-at-api-v1.md) closed
  the last batch of breaking changes — an unrecognized query parameter is
  now a 400 naming it rather than a silent no-op, which is what makes it
  safe to add one later without breaking a client that predates it. On
  that same footing, [0082](design/0082-what-the-freeze-holds-still.md)
  says what the freeze was never holding still: a property *added* to a
  schema that only ever travels in a response, which a client that
  predates it ignores. What stays frozen is every address, everything a
  client sends, and every removal, rename, retype or requiredness change
  in what it receives. That
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
  everything that file lets a client observe — every operation and its
  `operationId`, parameter, header, status code, schema field and the
  constraints on it (`pattern`, `format`, `default`, `maximum`, and the
  rest) — and CI fails when the two disagree
  (`cmd/ochakai/frozenwire_test.go`), so a rename that keeps every count
  still cannot land quietly. Prose is deliberately outside it: the
  documentation in the contract stays free to improve, and so is an
  **optional query parameter**: 0064 §2 made an unrecognized key a 400
  expressly so one could be added later, and
  [0101](design/0101-a-level-can-be-walked.md) §5 finished writing that
  down on the request side — the first one added is `cursor`, which walks
  a directory listing past the 1000-row page instead of ending at
  `truncated: true`. A caller that does not send it reads the same answer
  it read before. **What the
  fingerprint cannot hold is which input gets which answer**, and
  [0100](design/0100-md-is-how-a-concept-is-spelled.md) is the first change
  to land in that gap: a `.md` path now holds a concept and nothing else,
  so a markdown document with no `type` is a 400 where it used to be
  stored as a file at that address — a request that used to succeed and
  now does not, with **no line of the fingerprint moving**, because
  OpenAPI cannot spell a `requestBody` that differs per address. It is
  also the first use of the second reason the freeze may be broken:
  ochakai was writing bundles that fail OKF SPEC §11's conformance
  conditions, and the export is the guarantee this project actually
  makes.
- **An error's sentence is unstable; its `code` is not.** Every error
  response carries both ([0083](design/0083-an-error-carries-a-code.md)):
  `error` is a sentence for a person and may be reworded in any release,
  and `code` is a closed vocabulary a client may branch on. Branch on the
  code, never on the prose — the prose was the only thing separating
  three conditions that share 409 (`already_exists`, `not_deleted`,
  `no_rejection`) and two that share 412 (a stale `If-Match`, and an
  occupied path under `If-None-Match: *`), which is what made a
  string match the only way to tell them apart and this policy's
  permission to reword them a trap. New codes may be added, so treat an
  unrecognized one as the status alone would be treated.
- **MCP** — tool names, arguments, and which tools exist at all.
  `compile_sql` existed until `0.13.0` and does not now, and the five
  tools that said `knowledge` say `concept`. **A tool a release renames
  goes on answering under its old name for that release**
  ([0088](design/0088-a-retired-name-answers-for-one-release.md)): the
  call is forwarded, and the answer opens with a line naming the tool to
  call instead. The old name is never in `tools/list`, so an agent that
  has not already learned it never does — a tool description is paid for
  out of an agent's context window, and one that is about to stop
  existing is not worth the room. Arguments have no window; only names
  do.
- **The CLI** — commands, flags, and the shape of what they print. A
  renamed *command* gets the same one release, and says so on stderr. It
  is not in `ochakai help`, `docs/cli.md` or the completion scripts,
  for the reason the MCP one is not listed either. Flags and printed
  shapes have no window.
- **The stored data** — migrations run automatically at server start, and
  they are one-way. There is no downgrade path.

The OKF export format is the one part with an external anchor: it follows
[OKF v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf),
so it changes when the spec does or when ochakai's reading of it does —
`0.14.0` changed the key order in exported frontmatter, which is a diff in
anything that stores bundles in git.

## What you get instead of stability

Not a promise that nothing breaks. A promise that you are told what did:

- **A rename reaches you at the call, not only in the changelog**, which
  is the one place a script or an agent configuration was never going to
  read. And the window closes on time because closing it is checked:
  each retired name carries the release that retired it, and CI fails as
  soon as a later release is cut with the entry still in place. A
  deprecation window's failure mode is not being broken, it is never
  ending.
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
