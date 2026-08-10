---
name: release
description: Cut an ochakai release — the version-bump PR, the annotated tag, the release body, and the post-tag verification. Use when asked to release, cut a version, tag, or prepare a release PR.
---

# Cutting an ochakai release

A release is a tag. Everything else is automation — but three files carry
a version number the tag does not update, the release body is only as
good as the tag message it is copied from, and a pushed tag is permanent
because the Go module proxy caches it forever. So the work is: a reviewed
PR, then a tag, then verification.

[CONTRIBUTING.md](../../../CONTRIBUTING.md) is the reference; this skill
is the running order and the traps.

## 0. Find out where you actually are

Never infer the current version from `CHANGELOG.md` alone — it is what a
PR *proposes*, not what is published.

```bash
gh release list --limit 5 && git fetch --tags && git log --oneline -5 origin/main
```

Then decide the number. While the major version is 0:

- any **BREAKING** entry in the unreleased section → bump the **minor**
- otherwise → bump the patch

Check CI is green on `main` before starting. Do not prepare a release on
top of a red main.

## 1. The preparation PR

Branch off `origin/main`. Four files that drift silently — nothing
fails when they are stale — and one window that has to close:

1. **`CHANGELOG.md`**
   - `## [Unreleased]` → `## [x.y.z] - YYYY-MM-DD` (tag date, JST)
   - add a fresh empty `## [Unreleased]` above it
   - at the bottom: add `[x.y.z]: …/compare/v<prev>...vx.y.z` and
     repoint `[Unreleased]` at the new tag
2. **`api/openapi.yaml`** — `info.version`
3. **`.github/ISSUE_TEMPLATE/bug_report.yml`** — the version placeholder
4. **`deploy/cloudrun/README.md`** — the hand-set `export VERSION=`
   example, for the reader with no `gh` CLI
5. **`mcpserver.RetiredToolNames` and `retiredCommands`** — delete every
   entry whose `Release` is older than the version you are cutting. A renamed
   MCP tool or CLI command answers under its old name for one release
   (design doc 0088), and this release ends the previous one's window.
   `TestARetiredNameLastsOneRelease` fails until they are gone, so the
   prep PR is where you find out.

`grep -rn '<prev>' --exclude-dir=.git .` catches a fifth if one appears;
`CHANGELOG.md` and `docs/design` name old versions legitimately, so read
the hits rather than replacing them.

Read the unreleased entries as you go and confirm they match what
actually landed (`git log v<prev>..origin/main --oneline`). A missing
entry is much cheaper to fix now than after the tag.

Run `scripts/check` and open the PR. Merge it before tagging.

## 2. Tag the merged commit

Annotated, and **the message is the release notes** — that is the repo's
habit. Commits and code comments are English, but Japanese is fine in a
tag message and release body.

```bash
git fetch origin && git checkout -B rel origin/main
git tag -a vX.Y.Z -F -
git push origin vX.Y.Z
```

Pushing runs [release.yaml](../../../.github/workflows/release.yaml): one
job publishes the multi-arch image to GHCR with SBOM and provenance, the
other runs goreleaser for the archives, `checksums.txt`, and provenance.

## 3. The release body

**goreleaser's own changelog generation is disabled** (`.goreleaser.yaml`
— notes are written by hand), so the release it creates would come out
with an **empty** body. The workflow fills it from the annotated tag's
message, so the tag you wrote in step 2 is already on the Releases page —
which is the real reason to write that message properly rather than
leaving it for later.

**A published release here is immutable, so the workflow assembles a
draft and publishes it last.** goreleaser creates the release as a draft
(`release.draft`), the bundle, the notes and the attestations go on, and
a final step flips `--draft=false`. Anything that tries to add an asset
to an already-published release gets HTTP 422 — which is how v0.21.0
shipped with no bundle, no notes and no archive attestations: the upload
failed first and `set -e` took the steps behind it down. If you ever see
that 422 again, the fix is the *order*, not a retry. A release body can
still be edited after publication (`gh release edit --notes-file`);
assets cannot.

This is why the trap is upstream: a thin tag message is now a thin
release page, immediately and publicly. Write for an operator upgrading —
which migrations run, whether `updated_at` moves (held ETags,
`generated.at`), whether re-embedding is needed, and what a client
reading the old shape must change.

To add upgrade and install sections on top of it, edit the release; the
body is replaced wholesale, so start from what is already there:

```bash
git tag -l --format='%(contents)' vX.Y.Z | tail -n +3 > notes.md
# add the upgrade + install sections to notes.md
gh release edit vX.Y.Z --notes-file notes.md
```

A release drafted with its body *before* the tag was pushed keeps that
body — the workflow only writes into an empty one.

## 4. Verify what shipped

Do this rather than trusting the green check.

```bash
gh release view vX.Y.Z --json assets -q '.assets[].name'
gh release view vX.Y.Z --json body -q '.body' | head -5
```

Expect **6 archives, `ochakai_X.Y.Z.mcpb`, and `checksums.txt`**, and a
body that is the tag message — an empty one means the copy step warned
and skipped (a lightweight tag, or a message with nothing under its title
line). The bundle is not in `checksums.txt`: goreleaser writes that file
before `scripts/mcpb` runs, and the bundle's guarantee is the build
provenance attestation instead
(`gh attestation verify ochakai_X.Y.Z.mcpb -R na0fu3y/ochakai`). Then,
in a scratch directory:

```bash
shasum -a 256 -c checksums.txt --ignore-missing
./ochakai version
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:X.Y.Z -R na0fu3y/ochakai
gh attestation verify ochakai_X.Y.Z_linux_amd64.tar.gz -R na0fu3y/ochakai
gh attestation verify ochakai_X.Y.Z.mcpb -R na0fu3y/ochakai
curl -s https://proxy.golang.org/github.com/na0fu3y/ochakai/@latest
```

`./ochakai version` must print the tag — a mismatch means the ldflags
wiring broke, and it is the one failure the automation cannot see.

## Why the ceremony

A tag is effectively permanent: the module proxy caches it forever and a
bad one can only be retracted in `go.mod`, never withdrawn. That is why
the prep is a reviewed PR rather than a push to `main`, and why the
verification comes before announcing anything.
