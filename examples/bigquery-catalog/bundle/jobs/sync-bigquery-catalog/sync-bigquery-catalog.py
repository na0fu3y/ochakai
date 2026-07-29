#!/usr/bin/env python3
"""Project BigQuery table metadata into ochakai as BigQuery Table entries.

This is the `computation` of the Attested Computation entry beside it
(sync-bigquery-catalog.md). An agent asked to refresh the catalog supplies
parameter values and runs this file; per OKF SPEC §10.3 it must not author
a projection of its own instead.

ochakai has no connectors: a harvester inside the server would need
warehouse credentials it deliberately does not hold. This runs outside,
under your own service account, against the same REST API the CLI uses.

Reads BigQuery, writes ochakai, and holds no secret: BigQuery is reached
with Application Default Credentials, and ochakai with a Google-minted ID
token for the Cloud Run service.

    pip install google-cloud-bigquery google-auth requests

The run prints a receipt to stdout as JSON — the fields the entry's
executor declares — and progress to stderr.
"""

from __future__ import annotations

import argparse
import base64
import datetime
import json
import sys

import google.auth
import google.auth.transport.requests
import google.oauth2.id_token
import requests
from google.cloud import bigquery

TIMEOUT = 30


# --------------------------------------------------------------- identity


def id_token_for(audience: str) -> str:
    """Mint a Google ID token for a private Cloud Run service.

    Returns "" when the environment cannot mint one — a user's own ADC
    cannot — which is fine against a local OCHAKAI_INSECURE_DEV server and
    fatal against Cloud Run, where the server says so itself with a 403.
    """
    try:
        request = google.auth.transport.requests.Request()
        return google.oauth2.id_token.fetch_id_token(request, audience)
    except Exception as err:  # noqa: BLE001 — any failure means "no token"
        print(f"note: no ID token ({err}); sending unauthenticated", file=sys.stderr)
        return ""


def identity_from(token: str) -> str:
    """Read the email claim out of our own ID token.

    Not verified, and it does not need to be: we minted it a moment ago,
    and it is used only to recognize our own past writes. The server does
    verify — that is where the claim actually decides anything (design doc
    0002 §2).
    """
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload)).get("email", "")
    except Exception:  # noqa: BLE001
        return ""


# --------------------------------------------------------------- document


def flatten(fields, prefix: str = "") -> list[dict]:
    """Flatten a BigQuery schema, so a RECORD's leaves read as items.sku."""
    out: list[dict] = []
    for f in fields:
        name = prefix + f.name
        out.append(
            {
                "name": name,
                "type": f.field_type + ("[]" if f.mode == "REPEATED" else ""),
                "required": f.mode == "REQUIRED",
                "description": (f.description or "").replace("|", r"\|").replace("\n", " "),
            }
        )
        out.extend(flatten(f.fields or (), name + "."))
    return out


def build_body(table, columns: list[dict], usage: dict | None, window: tuple[str, str]) -> str:
    lines = ["# Columns", "", "| column | type | description |", "|---|---|---|"]
    for c in columns:
        kind = c["type"] + (" (required)" if c["required"] else "")
        lines.append(f"| `{c['name']}` | {kind} | {c['description']} |")

    lines += ["", "# Physical layout", ""]
    part = table.time_partitioning
    if part:
        needs_filter = table.require_partition_filter or getattr(part, "require_partition_filter", False)
        line = f"- **Partitioned** by `{part.field or '_PARTITIONTIME'}` ({part.type_})"
        if needs_filter:
            line += " — **a partition filter is required**; a query without one is rejected"
        lines.append(line)
    else:
        lines.append("- Not partitioned")
    if table.clustering_fields:
        lines.append("- **Clustered** by " + ", ".join(f"`{f}`" for f in table.clustering_fields))
    if table.num_rows is not None:
        lines.append(f"- Rows: {table.num_rows} (observed {window[1]})")
    if usage:
        lines.append(
            f"- Queried {usage['queries']} times by {usage['users']} accounts "
            f"between {window[0]} and {window[1]}"
        )

    if table.view_query:
        lines += ["", "# View definition", "", "```sql", table.view_query, "```"]

    lines += [
        "",
        "# Caveats",
        "",
        "_Nothing recorded yet._ Write what an agent has to know before it",
        "queries this table — the filter that is always required, the column",
        "that lies, the timezone the timestamps are in. Then `ochakai verify`",
        "this entry: the sync leaves verified entries alone, so your text will",
        "not be overwritten tomorrow morning.",
    ]
    return "\n".join(lines) + "\n"


def frontmatter(keys: dict) -> str:
    """Render the entry's metadata as the YAML block an OKF document opens with.

    Values go through json.dumps because JSON is a subset of YAML 1.2:
    every string it emits is a valid double-quoted scalar and every list
    and map a valid flow collection, so a table description full of
    colons, quotes or newlines cannot break the document. That is worth
    more here than block style would be, and it keeps this file free of a
    YAML dependency.
    """
    return "".join(
        f"{k}: {json.dumps(v, ensure_ascii=False)}\n"
        for k, v in keys.items()
        if v not in ("", None, [], {})
    )


def build_document(table, fq: str, dataset: str, usage: dict | None,
                   window: tuple[str, str], frequent: int) -> str:
    """The entry as an OKF document: YAML frontmatter, then the body.

    A concept *is* the document (design doc 0043) — there is no typed
    write surface beside the format, and a JSON body at a concept's
    address is a 415 naming this shape. The frontmatter is the metadata
    and the markdown is the body.

    `status` is deliberately absent: create defaults it to draft, and a
    replace leaves whatever is stored alone. `title` is absent too — the
    id's last segment is the display name (design doc 0022), and that
    segment is already the table id. The keys this instance owns —
    `generated`, `verified`, `created_by` — are never written from here:
    provenance is the server's observation of who called, not something a
    caller asserts (design doc 0009).
    """
    resource = f"bigquery://{fq}"
    tags = ["bigquery", dataset]
    if table.view_query:
        tags.append("view")
    if usage and usage["queries"] >= frequent:
        tags.append("frequently-queried")

    # The table cites itself: `ochakai search --source bigquery://…` then
    # returns this entry together with every insight and computation
    # someone wrote citing the same table (design doc 0037).
    source: dict = {"resource": resource, "title": fq}
    if table.modified:
        source["last_modified"] = table.modified.date().isoformat()
    if usage:
        # Query counts belong here, never in ochakai's own usage
        # telemetry: that counts how often *knowledge* was retrieved, this
        # counts how often a *table* was queried. Mixing the two would
        # corrupt the --sort usage review feed.
        source["usage_count"] = usage["queries"]

    keys = {
        "type": "BigQuery Table",
        "resource": resource,
        "description": (table.description or "").split("\n")[0][:280],
        "tags": tags,
        "sources": [source],
    }
    if usage:
        keys["usage_window"] = {"from": window[0], "to": window[1]}
    body = build_body(table, flatten(table.schema), usage, window)
    return f"---\n{frontmatter(keys)}---\n\n{body}"


# ----------------------------------------------------------------- upsert


class Ochakai:
    """A client of /api/v1/bundle — one object, one address.

    A concept lives at `<id>.md` in the bundle and that is the only
    address it has (design doc 0046 §3.5): the same path reads it, writes
    it and deletes it, and what the bytes become is decided by the
    document rather than by which endpoint was called.
    """

    def __init__(self, url: str, token: str):
        self.url = url.rstrip("/")
        self.headers = {"Authorization": f"Bearer {token}"} if token else {}

    def _at(self, entry_id: str) -> str:
        return f"{self.url}/api/v1/bundle/{entry_id}.md"

    def read(self, entry_id: str) -> tuple[dict | None, str]:
        """The entry as it stands and the ETag to write against, or (None, "").

        The ETag is a hash of the stored bytes and is opaque: hand it back
        in If-Match exactly as it arrived (design doc 0030). It is not a
        timestamp, and nothing here parses it.
        """
        r = requests.get(self._at(entry_id), headers=self.headers, timeout=TIMEOUT)
        if r.status_code == 404:
            return None, ""
        r.raise_for_status()
        return r.json(), r.headers.get("ETag", "")

    def write(self, entry_id: str, document: str, *, if_match: str = "",
              only_if_absent: bool = False) -> bool:
        """PUT the document. Returns whether anything actually changed.

        One call for both cases, because a PUT states what the object
        should say and existence is expressed by the precondition:
        `If-None-Match: *` for "only if the id is free", `If-Match` for
        "only if it still says this". A body identical to what is stored
        writes nothing and says so with Ochakai-Unchanged, so a daily run
        over an unchanged table leaves no revision behind.
        """
        headers = dict(self.headers, **{"Content-Type": "text/markdown"})
        if only_if_absent:
            headers["If-None-Match"] = "*"
        elif if_match:
            headers["If-Match"] = if_match
        r = requests.put(self._at(entry_id), data=document.encode("utf-8"),
                         headers=headers, timeout=TIMEOUT)
        r.raise_for_status()
        return r.headers.get("Ochakai-Unchanged") != "true"


def ruled_on(view: dict) -> str:
    """Why this entry is a person's now, or "" while it is still the sync's.

    Three separate signals, because ochakai keeps them separate (design
    doc 0043 §3.2): the **lifecycle** is what the writer declared, the
    **trust tier** is what the verification ledger derives, and a
    **rejection** is a live ruling beside both. Verifying an entry does
    not move its status — a ruling and a publication are different acts —
    so a sync that watched `status` alone would go on overwriting an entry
    somebody had just confirmed, which is the one thing this guard exists
    to prevent.
    """
    summary = view.get("summary", {})
    if summary.get("rejected"):
        return "rejected"
    if summary.get("trust", "unverified") != "unverified":
        return summary["trust"]
    status = summary.get("status", "stable")
    return "" if status == "draft" else status


def upsert(api: Ochakai, entry_id: str, document: str, identity: str, counts: dict) -> None:
    """Write the projection, unless a person has taken it over.

    The rule, in full: write only while nobody has ruled on the entry and
    this account was its last writer, and make the write conditional so an
    edit landing between the read and the write loses the race instead of
    being erased by it (design doc 0030). Verifying an entry — or simply
    editing it — takes it out of the sync for good. That is the same line
    design doc 0015 §3.1 draws for MCP, applied from outside: a machine
    does not overwrite what a human ruled on. ochakai needs no owner field
    and no authorization for this; the projection and provenance carry it.
    """
    view, etag = api.read(entry_id)
    if view is None:
        api.write(entry_id, document, only_if_absent=True)
        counts["written"] += 1
        print(f"create {entry_id}", file=sys.stderr)
        return

    if verdict := ruled_on(view):
        counts["skipped"] += 1
        print(f"skip   {entry_id} — {verdict}, a human ruled on it", file=sys.stderr)
        return

    # Who wrote the words that stand there now (design doc 0052): under
    # delegation this is the end user, which is exactly right — a person
    # who edited through an application is a person who edited.
    writer = view.get("observed", {}).get("generated", {}).get("by", {}).get("name", "")
    if identity and writer != identity:
        counts["skipped"] += 1
        print(f"skip   {entry_id} — last written by {writer}", file=sys.stderr)
        return

    if api.write(entry_id, document, if_match=etag):
        counts["written"] += 1
        print(f"sync   {entry_id}", file=sys.stderr)
    else:
        counts["unchanged"] += 1
        print(f"same   {entry_id}", file=sys.stderr)


# -------------------------------------------------------------------- run


def usage_counts(client: bigquery.Client, project: str, region: str, days: int) -> dict:
    """Query counts per table from the job history.

    Seeing anyone else's jobs needs bigquery.jobs.listAll
    (roles/bigquery.resourceViewer). Without it this counts only the jobs
    this account ran — which is worse than counting nothing, because the
    numbers still look plausible.
    """
    sql = f"""
        SELECT ref.dataset_id AS dataset_id,
               ref.table_id   AS table_id,
               COUNT(*)                     AS queries,
               COUNT(DISTINCT j.user_email) AS users
        FROM `region-{region}`.INFORMATION_SCHEMA.JOBS AS j,
             UNNEST(j.referenced_tables) AS ref
        WHERE j.creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL {days} DAY)
          AND j.job_type = 'QUERY'
          AND j.state = 'DONE'
          AND j.error_result IS NULL
          AND ref.project_id = @project
        GROUP BY dataset_id, table_id
    """
    config = bigquery.QueryJobConfig(
        query_parameters=[bigquery.ScalarQueryParameter("project", "STRING", project)]
    )
    return {
        f"{row.dataset_id}.{row.table_id}": {"queries": row.queries, "users": row.users}
        for row in client.query(sql, job_config=config).result()
    }


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--project", required=True, help="project holding the tables")
    p.add_argument("--datasets", required=True, nargs="+",
                   help="datasets to sync. There is no 'all datasets' default on "
                        "purpose: scope is the one knob that keeps a curated base curated")
    p.add_argument("--ochakai-url", required=True, help="e.g. https://ochakai-<hash>.run.app")
    p.add_argument("--jobs-region", default="",
                   help="region for INFORMATION_SCHEMA.JOBS, e.g. us or asia-northeast1. "
                        "Unset skips the job history and writes no usage counts")
    p.add_argument("--usage-days", type=int, default=30)
    p.add_argument("--frequent-threshold", type=int, default=30,
                   help="queries over the window at or above which the entry is "
                        "tagged frequently-queried")
    p.add_argument("--prefix", default="catalog/bigquery", help="id prefix")
    p.add_argument("--sync-identity", default="",
                   help="the account this runs as (default: the ID token's email claim)")
    p.add_argument("--dry-run", action="store_true", help="print the documents instead of writing")
    args = p.parse_args()

    today = datetime.datetime.now(datetime.timezone.utc).date()
    window = ((today - datetime.timedelta(days=args.usage_days)).isoformat(), today.isoformat())

    token = "" if args.dry_run else id_token_for(args.ochakai_url)
    identity = args.sync_identity or identity_from(token)
    api = Ochakai(args.ochakai_url, token)
    client = bigquery.Client(project=args.project)

    usage = {}
    if args.jobs_region:
        print(f"reading job history for the last {args.usage_days} days "
              f"in region-{args.jobs_region}", file=sys.stderr)
        usage = usage_counts(client, args.project, args.jobs_region, args.usage_days)

    counts = {"tables_seen": 0, "written": 0, "unchanged": 0, "skipped": 0, "failed": 0}
    for dataset in args.datasets:
        for item in client.list_tables(dataset):
            if item.table_type not in ("TABLE", "VIEW"):
                continue
            counts["tables_seen"] += 1
            name = item.table_id
            fq = f"{args.project}.{dataset}.{name}"
            entry_id = f"{args.prefix}/{args.project}/{dataset}/{name}"
            try:
                table = client.get_table(f"{args.project}.{dataset}.{name}")
                document = build_document(table, fq, dataset,
                                          usage.get(f"{dataset}.{name}"), window,
                                          args.frequent_threshold)
                if args.dry_run:
                    # The document, exactly as it would be written: what
                    # goes over the wire is what an export brings back and
                    # what `ochakai get` prints, so a dry run is readable
                    # by the person who would have to review the entry.
                    print(f"=== {entry_id}.md")
                    print(document)
                    continue
                upsert(api, entry_id, document, identity, counts)
            except Exception as err:  # noqa: BLE001 — one bad table is not a bad run
                counts["failed"] += 1
                print(f"FAIL   {entry_id} — {err}", file=sys.stderr)

    # The receipt the entry's executor declares. Every field here is one a
    # run has to bring back; checking them is the consumer's job, never
    # ochakai's (OKF SPEC §10.5).
    print(json.dumps({"sync_identity": identity, **counts}))
    return 1 if counts["failed"] else 0


if __name__ == "__main__":
    sys.exit(main())
