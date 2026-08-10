#!/usr/bin/env python3
"""Draft Golden Query concepts from a warehouse's own job history.

Reads INFORMATION_SCHEMA.JOBS rows as JSON on stdin and writes an OKF
bundle of `Attested Computation` **drafts** as a tar.gz on stdout, one
per query somebody keeps running. Pipe it into `ochakai import -`.

    bq query --format=json --nouse_legacy_sql \\
      'SELECT query, user_email, creation_time, referenced_tables
         FROM `region-us`.INFORMATION_SCHEMA.JOBS
        WHERE creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 90 DAY)
          AND job_type = "QUERY" AND state = "DONE" AND error_result IS NULL' \\
      | ./draft-from-query-history.py --project my-project \\
      | ochakai import -

**It does not connect to anything.** You run the query, with your own
client and your own identity, and pipe the answer here — the same shape
`ochakai seed` takes, and for the same reason: ochakai holds no warehouse
credential and neither does this (design doc 0081 §1). Seeing anybody
else's jobs needs bigquery.jobs.listAll; without it the history is your
own queries, which is worth knowing before you read the counts.

**There is no LLM in here.** Grouping is a deterministic fingerprint —
comments and literals stripped, whitespace collapsed — so the same two
queries group and nothing is invented. What a query *means* is the one
thing this cannot produce, so every draft carries an empty "The question
this answers" heading, and that is the seam: fill it yourself, or hand
the drafts to your own agent, which is where an LLM belongs.

Everything lands as a draft. A query somebody ran forty times is evidence
that it matters, not evidence that it is right — the review queue is
where that gets decided.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import re
import sys
import tarfile

# Literal-ish things a fingerprint should not distinguish. Two queries
# that differ only in the month they filter on are the same question
# asked twice, and grouping them is the whole point.
_COMMENT_LINE = re.compile(r"--[^\n]*")
_COMMENT_BLOCK = re.compile(r"/\*.*?\*/", re.S)
_STRING = re.compile(r"'(?:[^']|'')*'")
_NUMBER = re.compile(r"\b\d+(?:\.\d+)?\b")
_SPACE = re.compile(r"\s+")
# Space around punctuation is typing, not meaning: `m , sum(` and `m, sum(`
# are the same query reformatted, and a fingerprint that told them apart
# would put the same question in the queue twice.
_PUNCT_SPACE = re.compile(r"\s*([(),;])\s*")
# The first table named after FROM or JOIN, for a readable id when the
# rows carry no referenced_tables.
_FROM = re.compile(r"\b(?:from|join)\s+`?([A-Za-z0-9_.\-]+)`?", re.I)
_SLUG_BAD = re.compile(r"[^a-z0-9]+")


def fingerprint(sql: str) -> str:
    """The form two runs of the same query share."""
    s = _COMMENT_BLOCK.sub(" ", sql)
    s = _COMMENT_LINE.sub(" ", s)
    s = _STRING.sub("?", s)
    s = _NUMBER.sub("?", s)
    s = _SPACE.sub(" ", s)
    s = _PUNCT_SPACE.sub(r"\1", s)
    return s.strip().rstrip(";").lower()


def yaml_str(text: str) -> str:
    """A YAML scalar that survives whatever is in it.

    Quoted always, rather than when it looks risky. A title built from a
    table name and a count reads fine until the day it holds a colon and
    a space — and then the whole frontmatter stops parsing, the document
    is stored as a file rather than a concept, and the only sign is a
    note nobody reads. Which is exactly how this was found.
    """
    return '"' + text.replace("\\", "\\\\").replace('"', '\\"') + '"'


def slug(text: str) -> str:
    return _SLUG_BAD.sub("-", text.lower()).strip("-")


def table_names(row: dict, sql: str) -> list[str]:
    """The tables a row touched, as `dataset.table`.

    referenced_tables when the JOBS query selected it — it is the
    warehouse's own answer and survives a query this script's regex
    cannot parse. The FROM match is the fallback, and it is a fallback:
    it sees the first table and nothing else.
    """
    refs = row.get("referenced_tables")
    if isinstance(refs, str):
        try:
            refs = json.loads(refs)
        except json.JSONDecodeError:
            refs = None
    out = []
    for ref in refs or []:
        if isinstance(ref, dict) and ref.get("table_id"):
            out.append(f"{ref.get('dataset_id', '')}.{ref['table_id']}".strip("."))
    if out:
        return out
    m = _FROM.search(sql)
    return [m.group(1).split(".", 1)[-1]] if m else []


def rows_from(stream) -> list[dict]:
    """A JSON array, or one object per line. bq --format=json gives the
    first; a streaming export gives the second."""
    text = stream.read().strip()
    if not text:
        return []
    if text.startswith("["):
        return json.loads(text)
    return [json.loads(line) for line in text.splitlines() if line.strip()]


class Candidate:
    """One query, and what the history says about it."""

    def __init__(self, sql: str, tables: list[str]):
        self.sql = sql
        self.tables = tables
        self.runs = 0
        self.users: set[str] = set()
        self.first: str | None = None
        self.last: str | None = None

    def observe(self, row: dict, sql: str) -> None:
        self.runs += 1
        if row.get("user_email"):
            self.users.add(row["user_email"])
        at = (row.get("creation_time") or "")[:10]
        if at:
            self.first = min(self.first or at, at)
            self.last = max(self.last or at, at)
            # The newest spelling is the one a reader should see: an
            # older run of the same question may name a column that has
            # since been dropped.
            if at == self.last:
                self.sql = sql


def gather(rows: list[dict]) -> dict[str, Candidate]:
    by_print: dict[str, Candidate] = {}
    for row in rows:
        sql = (row.get("query") or "").strip()
        if not sql:
            continue
        # Judged on the fingerprint rather than the raw text: a query
        # opening with a `-- what this is for` comment is still a query,
        # and reading the first word of the raw SQL threw those away.
        fp = fingerprint(sql)
        if not fp.lstrip("( ").startswith("select"):
            continue  # DDL, DML and scripts are not questions
        cand = by_print.get(fp)
        if cand is None:
            cand = by_print[fp] = Candidate(sql, table_names(row, sql))
        if not cand.tables:
            cand.tables = table_names(row, sql)
        cand.observe(row, sql)
    return by_print


def document(cand: Candidate, fp: str, args) -> tuple[str, str]:
    """One draft: its bundle path, and the OKF document."""
    name = slug(cand.tables[0].split(".")[-1]) if cand.tables else "query"
    entry_id = f"{args.prefix}/{name}-{hashlib.sha256(fp.encode()).hexdigest()[:6]}"
    title = f"{name.replace('-', ' ')}: a query run {cand.runs} times"
    window = ""
    if cand.first and cand.last:
        window = f'usage_window: {{ from: "{cand.first}", to: "{cand.last}" }}\n'
    tags = sorted({slug(t.split(".")[-1]) for t in cand.tables} | {"from-query-history"})
    # No `generated:` key. A document that declares its own provenance
    # imports as a *claim* — kept under `received`, answering to no trust
    # tier, and noted once per concept (design doc 0075 §3.1). The
    # instance's own record of who wrote these is truer and quieter: it
    # observed the import. What made them is in the tag, the id prefix
    # and the section below.
    # The id is a placeholder. It is built to be unique and readable, not
    # to be right — whoever promotes this draft renames it to whatever
    # the question turns out to be about.
    body = f"""---
type: Attested Computation
title: {yaml_str(title)}
description: {yaml_str("Drafted from job history; nobody has said yet what question it answers")}
tags: [{", ".join(tags)}]
{window}status: draft
runtime: bigquery
---

## The question this answers

<!-- Nothing here yet, and this script cannot fill it: the history says
what was run, never why. Write the question a person would ask, then
promote this out of `{args.prefix}/` to where that question belongs. -->

## What the history says

- Run **{cand.runs} times**{f" by {len(cand.users)} accounts" if cand.users else ""}\
{f", {cand.first} to {cand.last}" if cand.first else ""}.
- Tables: {", ".join(f"`{t}`" for t in cand.tables) if cand.tables else "not recorded"}.
- Grouped with every run whose SQL differs only in its literals, comments
  or whitespace. The SQL below is the most recent of them.

## Computation

```sql
{cand.sql}
```
"""
    return f"{entry_id}.md", body


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--prefix", default="queries/from-history",
                   help="id prefix the drafts are written under (default: %(default)s)")
    p.add_argument("--min-runs", type=int, default=5,
                   help="how often a query must have been run to be a candidate "
                        "(default: %(default)s). A query run once is a question "
                        "somebody had, not one the team keeps having")
    p.add_argument("--min-users", type=int, default=1,
                   help="how many distinct accounts must have run it (default: "
                        "%(default)s). Raise it to find what more than one person "
                        "needs; it does nothing unless the rows carry user_email")
    p.add_argument("--limit", type=int, default=50,
                   help="most-run first, at most this many (default: %(default)s). "
                        "A review queue nobody can finish is one nobody starts")
    p.add_argument("--project", default="", help="named in the drafts' provenance")
    args = p.parse_args()

    rows = rows_from(sys.stdin)
    cands = gather(rows)
    kept = [
        (fp, c) for fp, c in cands.items()
        if c.runs >= args.min_runs and max(len(c.users), 1) >= args.min_users
    ]
    kept.sort(key=lambda kv: (-kv[1].runs, kv[0]))
    kept = kept[: args.limit]

    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for fp, cand in kept:
            path, doc = document(cand, fp, args)
            data = doc.encode()
            info = tarfile.TarInfo(path)
            info.size = len(data)
            info.mtime = 0
            tar.addfile(info, io.BytesIO(data))
    sys.stdout.buffer.write(buf.getvalue())

    print(
        f"read {len(rows)} jobs, {len(cands)} distinct queries, "
        f"{len(kept)} drafted (>= {args.min_runs} runs); "
        "pipe into `ochakai import -` and rule on them in the review queue",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
