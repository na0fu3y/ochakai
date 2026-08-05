# Support

ochakai is maintained by one person. Everything here is public and
asynchronous, there is no support contract, and no response time is promised —
what follows is where to put a thing so it gets seen.

## Where to ask what

| You have | Where it goes |
|---|---|
| A question — how to deploy it, whether something is the intended way, what a design doc decided | [Discussions](https://github.com/na0fu3y/ochakai/discussions) |
| A bug you can reproduce | [Issues](https://github.com/na0fu3y/ochakai/issues/new/choose), bug report form |
| A concrete proposal | [Issues](https://github.com/na0fu3y/ochakai/issues/new/choose), feature request form |
| A vulnerability | [Security advisories](https://github.com/na0fu3y/ochakai/security/advisories/new), privately — see [SECURITY.md](SECURITY.md) |
| A change you already wrote | A pull request — see [CONTRIBUTING.md](CONTRIBUTING.md) |

A question posted as an issue will be moved rather than ignored, so a wrong
guess costs nothing. The one thing to get right is the vulnerability row: a
public issue is the wrong place, and the private path exists for exactly that.

## Before you ask

Most answers are already written down, and the docs are short enough to check:

- [docs/faq.md](docs/faq.md) (Japanese) — the questions this project's shape
  invites:
  whether it runs outside Google Cloud, whether anything leaves your project,
  what an agent may do to verified knowledge, what happens if you leave.
- [docs/guides/troubleshooting.md](docs/guides/troubleshooting.md) (Japanese)
  — symptoms and their causes on the local and client side, from an empty
  search to a skipped import to a 412.
- [README](README.md) — what ochakai is, and the things it deliberately does
  not do. [docs/configuration.md](docs/configuration.md) (Japanese) has the
  requirements and every environment variable.
- [deploy/cloudrun/README.md](deploy/cloudrun/README.md) (Japanese) — the full
  Cloud Run + Cloud SQL walkthrough, including the hardening checklist.
- [docs/design/README.md](docs/design/README.md) — the index of numbered design
  docs (mostly Japanese), with [English summaries](docs/design/README.en.md)
  beside it. It says which document describes the current state of an area,
  which is usually the fastest answer to "why does it work like that".
- [ROADMAP.md](ROADMAP.md) — what is being worked on, and what has been ruled
  out on purpose.
- [docs/compatibility.md](docs/compatibility.md) — what may break between
  releases, and which release is supported (one: the latest).

When you do ask, the useful details are the same ones the bug form asks for:
version (`ochakai version`), deployment (docker compose or Cloud Run),
PostgreSQL version, what the startup log says about semantic search, and
which surface you were on.

## What to expect

Reports get read. Reproducible bugs in the documented deployment posture get
priority; proposals are answered honestly, including "no, and here is the
design doc that says why". Silence means the maintainer has not got to it, not
that the thread was rejected — a ping after a week or two is fine.

Commercial support does not exist. The project is MIT-licensed and
self-hostable, which is the intended answer to needing it on your own terms.
