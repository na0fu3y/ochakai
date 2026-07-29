# The improvement loop

Agents draft, a human verifies, and what happened next is measured. This
page walks that loop once, end to end, against the quick start's knowledge
base — the [demo bundle](../examples/demo) has enough in it that each step
comes back with something.

## Try four prompts

Once an agent is connected ([README](../README.md#connect-an-agent)), four
prompts walk the whole thing.

**Recall.** Ask something the knowledge base can answer and watch the agent
reach for it rather than guess:

> What do we already know about revenue? Use ochakai before answering.

One `get_context` call comes back with the entries in full, so the agent
starts from your definitions instead of inventing one.

**Write back.** Tell it something worth keeping:

> Revenue in August runs about 15% below a normal month — it's seasonal,
> not a problem. Save that to ochakai so the next session has it.

It lands as a **draft**, attributed to the agent that wrote it. It is not
trusted yet, and search says so.

**Review.** Open `ochakai ui`, find it in the review queue, and press
Verify — or Reject with a reason, which is kept so agents stop re-proposing
it. This is the half no agent does for you.

**Close the loop.** When knowledge turns out to be wrong, say so:

> That golden query returned a number that doesn't match the finance
> close. Report it as failed in ochakai, with why.

`report_outcome` moves the entry into the re-verification feed instead of
letting the next agent trust it blind. Verifying it again empties the feed.

If nothing comes back on the first prompt, the agent has not actually
connected — `ochakai whoami` says which server it is talking to and as whom.

To make the loop automatic rather than habitual, install the
[Claude Code hooks](../examples/claude-code): a UserPromptSubmit hook injects
relevant knowledge before the agent starts (recall), and a Stop hook asks it
once per data session to save what it learned (write-back) — both without an
LLM.

## The web UI: the human half

Agents write drafts; somebody has to read them. The bundled web UI is where a
human reviews what agents learned — search and filter by status, browse the
knowledge as a folder tree (hierarchical IDs are directories), read an entry
with its links and usage counts, then verify / deprecate / reject (with the
reason) in one click. One self-contained page, no build step; deliberately
**not** a BI tool — no charts, no query execution, no chat.

![The draft review queue: entries agents wrote back, waiting for a human to
verify or reject them](images/webui-review.png)

![The knowledge base as a folder tree: an entry's id is its path, so the
sidebar is the way in](images/webui-tree.png)

![An entry: status, provenance, and the tabs for its attributes, links,
backlinks, usage and revision history](images/webui-entry.png)

Same page, two identities: `ochakai ui` serves it on loopback acting as *you*
(zero deploy, edits recorded as `human:<you>`), and `ochakai serve-ui`
deploys it as a team-shared service — same container image as the server,
just `--args=serve-ui`
([deploy guide §5b](../deploy/cloudrun/README.md)).

## Three feeds, and finishing them

Two feeds put the re-verification queue in front of that reviewer: a
*verification age* feed (oldest `verified_at` first) so stale golden queries
surface, and a *needs review* feed (`sort=failed`) that lists the entries
agents reported wrong, worst first. Both empty the same way: re-verifying an
entry — "I checked it again and it is still right" — stamps a fresh
`verified_at` and takes it out of either feed, so the queues are something a
reviewer can finish rather than a ledger that only grows.

![The re-verification feed, filtered to entries agents reported wrong, with
the type and status filters above it](images/webui-wrong.png)

A third feed, *stale* (`sort=stale_after`), lists entries past the expiry
their own author declared — that one clears by editing the entry to
re-declare the date, since the date is a claim the writer made rather than
something the server observed.

Whether any of the three is holding anything is one call — `ochakai stats`,
and the Review tab's badge — so a queue going quiet stops looking like a
queue being empty; with `--exit-code` it is a cron job away from telling your
team (design doc [0049](design/0049-queue-counts.md)). And when a cited
document changes, `?source=<uri>` answers the other direction: every entry
derived from it, straight from the source's own line on the entry page.

## What is measured

Per-entry usage counts show whether the knowledge is being used;
`ochakai stats` reports the instance's totals, including the searches that
came back with nothing (design doc
[0051](design/0051-instance-metrics-and-search-misses.md)). Outcome reports
(`report_outcome`: worked / failed) are the evidence-based half of staleness,
next to the time-based `verified_at` and `stale_after` — an agent that ran a
golden query and got a wrong number says so, and the entry rises in the
re-verification feed for a human or agent to re-check. Re-checking is itself
recorded (`ochakai verify`, or the web UI's Verify), which is what empties
the feed (design doc [0025](design/0025-closing-the-loop.md)).

To keep golden queries trustworthy without waiting for an agent to trip over
one, run them as canaries from your CI:
[golden query canary](guides/golden-query-canary.md).
