"""Deterministic attester for jobs/sync-bigquery-catalog.

Checks a receipt produced by skills/run-a-python-job.md. The first two
checks mirror attesters/sql_equality.py in the OKF reference bundle, which
does the same two jobs for `runtime: bigquery`:

  1. Provenance — did the executor run what was sanctioned? For a SQL
     computation that question is about the query text. Here it is about
     scope: every entry id is derived as
     `<prefix>/<project>/<dataset>/<table>`, so a run under a different
     prefix, project or dataset list did not refresh this catalog. It
     wrote a second one, at addresses nobody is watching, and left the
     first to go stale in silence. Palantir's ontology manager asks the
     same question before it saves an object type — a primary key must be
     unique and must not change between builds — and this is that check
     for a catalog whose primary key is its bundle path.

  2. Fidelity — does the reported outcome hold together? Every table the
     run enumerated — plus one dataset entry per dataset it was scoped to
     — has to be accounted for by exactly one outcome, and none of them
     may have failed. `tables_seen` is counted before a write is attempted
     and the five outcomes after it, so a mismatch means a table left the
     loop without being counted. `missing` is an outcome and not a
     failure: a table dropped between the listing and the read is a
     warehouse's ordinary day, and a check that turns red for it is a red
     nobody reads. What guards against the catalog actually draining away
     is check 4, which sees the trend, not the night.

  3. Ownership — the run still projected something. `written` and
     `unchanged` are the two outcomes that leave an entry the sync's;
     `skipped` is the one that says a person took it. A run where every
     table was skipped refreshed nothing, and checks 1 and 2 call that a
     clean night — the counts conserve, nothing failed, the catalog is
     the same size as yesterday. It is what a projection that has quietly
     stopped looks like, and equally what a catalog nobody needs a
     schedule for looks like; both are worth a person's attention.

  4. Continuity — only when a previous receipt is supplied: the catalog
     did not collapse between two runs. A dataset that silently loses read
     access looks exactly like a warehouse that got smaller.

Every field is type-checked before it is compared, and a value of the
wrong shape is a failed verdict rather than a coerced one. A verifier is
the wrong place to be accommodating: `sorted("shop")` is
`['h','o','p','s']`, which compares equal to every anagram of it, and
`True` is an instance of `int`, so a receipt of booleans would otherwise
conserve its way to a clean verdict.

Returns a verdict dict:
  { "ok": bool, "reason": str | None, "details": { ... } }

Never uses an LLM. Never makes network calls. Never reads the knowledge
base. Safe to run consumer-side, and safe to run from the scheduler that
ran the job.
"""

from __future__ import annotations

import argparse
import json
import sys
from typing import Any

# The fields skills/run-a-python-job.md says a run has to bring back, and
# the same list the concept declares in `executor.receipt`.
RECEIPT_FIELDS = (
    "sync_identity",
    "project",
    "datasets",
    "prefix",
    "tables_seen",
    "written",
    "unchanged",
    "skipped",
    "missing",
    "failed",
)

# A catalog may legitimately shrink — a dataset is retired, a table is
# dropped. It may not halve overnight. This is a floor on the ratio
# between two runs, not a claim about the warehouse.
DEFAULT_MIN_RATIO = 0.5

_SCOPE_STRINGS = ("sync_identity", "project", "prefix")
_OUTCOMES = ("written", "unchanged", "skipped", "missing", "failed")


def _verdict(ok: bool, reason: str | None, **details: Any) -> dict[str, Any]:
    return {"ok": ok, "reason": reason, "details": details}


def _count(value: Any) -> int | None:
    """A non-negative integer, or None. Booleans are not integers here."""
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


def _datasets(value: Any) -> list[str] | None:
    """The dataset list, sorted, or None when it is not a list of names.

    A bare string is refused rather than sorted, because sorting one gives
    its letters and any anagram would then compare equal to it.
    """
    if isinstance(value, str) or not isinstance(value, (list, tuple)):
        return None
    if not value or not all(isinstance(d, str) and d for d in value):
        return None
    return sorted(value)


def _scope(source: dict[str, Any]) -> tuple[dict[str, Any] | None, str]:
    """The four fields entry ids are derived from, or why they are unusable."""
    out: dict[str, Any] = {}
    for key in _SCOPE_STRINGS:
        value = source.get(key)
        if not isinstance(value, str) or not value:
            return None, f"{key} is missing or not a non-empty string"
        out[key] = value
    datasets = _datasets(source.get("datasets"))
    if datasets is None:
        return None, "datasets is not a non-empty list of names"
    out["datasets"] = datasets
    return out, ""


def attest(
    *,
    sanctioned: dict[str, Any],
    receipt: dict[str, Any],
    previous: dict[str, Any] | None = None,
    min_ratio: float = DEFAULT_MIN_RATIO,
) -> dict[str, Any]:
    """Verify one catalog sync receipt.

    Args:
      sanctioned: the run as it was authorized — `sync_identity`, `project`,
                  `datasets` and `prefix`. These are the parameter values
                  the concept was verified with, not whatever the run chose.
      receipt:    the JSON object the run printed to stdout.
      previous:   the receipt of the last run that passed, if there is one.
                  Omit it for the first run; the continuity check is then
                  skipped rather than assumed.
      min_ratio:  the floor for `tables_seen` against `previous`.

    Returns:
      A verdict dict. Callers MUST treat the catalog as unrefreshed when
      verdict["ok"] is False — the entries that did get written are still
      there, and that is the point: a bad run is visible rather than
      undone.
    """
    if absent := [f for f in RECEIPT_FIELDS if f not in receipt]:
        return _verdict(False, "receipt is missing fields", missing=absent,
                        receipt_keys=sorted(receipt))

    # 1. Provenance. The sanctioned side is checked first and separately,
    # so an under-specified authorization reads as the caller's mistake
    # rather than as an accusation against the run.
    want, why = _scope(sanctioned)
    if want is None:
        return _verdict(False, f"sanctioned scope is unusable: {why}",
                        sanctioned_keys=sorted(sanctioned))
    got, why = _scope(receipt)
    if got is None:
        return _verdict(False, f"receipt scope is unusable: {why}")
    if want != got:
        differing = {k: {"sanctioned": want[k], "ran_with": got[k]}
                     for k in want if want[k] != got[k]}
        return _verdict(False, "run scope does not match the sanctioned scope",
                        differing=differing)

    # 2. Fidelity.
    counts: dict[str, int] = {}
    for field in ("tables_seen", *_OUTCOMES):
        value = _count(receipt[field])
        if value is None:
            return _verdict(False, f"{field} is not a non-negative integer",
                            value=repr(receipt[field]))
        counts[field] = value

    # One outcome per table enumerated, plus one per dataset in scope —
    # each dataset's own entry leaves the run by the same five doors.
    accounted = sum(counts[f] for f in _OUTCOMES)
    expected = counts["tables_seen"] + len(got["datasets"])
    if accounted != expected:
        return _verdict(False, "objects seen and outcomes accounted for disagree",
                        tables_seen=counts["tables_seen"],
                        dataset_entries=len(got["datasets"]),
                        accounted=accounted, counts=counts)

    if counts["failed"]:
        return _verdict(False, "the run reported failed tables",
                        failed=counts["failed"])

    # 3. Ownership. `expected` is never zero — a run is scoped to at least
    # one dataset, whose own entry it must keep current or be told a
    # person took the whole catalog.
    projected = counts["written"] + counts["unchanged"]
    if not projected:
        return _verdict(False, "the run projected nothing: every entry was skipped or gone",
                        tables_seen=counts["tables_seen"], skipped=counts["skipped"],
                        missing=counts["missing"])

    # 4. Continuity.
    if previous is not None:
        before = _count(previous.get("tables_seen"))
        if before is None:
            return _verdict(False, "previous receipt has no usable tables_seen",
                            previous_tables_seen=repr(previous.get("tables_seen")))
        if before and counts["tables_seen"] < before * min_ratio:
            return _verdict(
                False, "the catalog shrank past the floor between two runs",
                tables_seen=counts["tables_seen"], previous=before,
                min_ratio=min_ratio)

    return _verdict(True, None, scope=got, counts=counts)


def main(argv: list[str] | None = None) -> int:
    """Read a receipt on stdin, take the sanctioned scope as flags, print a verdict.

    The reference bundle's attester is a module and nothing else, which is
    right for one a consumer calls in-process before it displays a number.
    This one is the last step of a scheduled job, where the caller is a
    shell — so it is both, and the step that decides whether a run counted
    does not start with "write yourself a driver".

    Exit status is the verdict: 0 attested, 1 not. A scheduler can gate on
    it without reading the JSON.
    """
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--sync-identity", required=True,
                   help="the account the run was scheduled to use")
    p.add_argument("--project", required=True)
    p.add_argument("--datasets", required=True, nargs="+")
    p.add_argument("--prefix", default="catalog/bigquery")
    p.add_argument("--previous", metavar="FILE",
                   help="the last receipt that passed; omit on the first run, and "
                        "the continuity check is skipped rather than assumed")
    args = p.parse_args(argv)

    receipt = json.load(sys.stdin)
    previous = None
    if args.previous:
        with open(args.previous, encoding="utf-8") as f:
            previous = json.load(f)

    verdict = attest(
        sanctioned={
            "sync_identity": args.sync_identity,
            "project": args.project,
            "datasets": args.datasets,
            "prefix": args.prefix,
        },
        receipt=receipt,
        previous=previous,
    )
    json.dump(verdict, sys.stdout, indent=2, ensure_ascii=False)
    print()
    return 0 if verdict["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
