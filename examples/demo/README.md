# A demo knowledge base

Nine entries about revenue at an invented online shop, written as an
[OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
v0.2 bundle. Enough of a knowledge base to see the whole shape of one: a metric
and the verified computation behind it, the glossary term and the table catalog
entry they both rest on, the policy that decides the number, and an `Insight`
that says how to *read* the series — the type that carries the knowledge no
semantic layer holds.

Load it into a running server:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

Then try `ochakai context "why is revenue down?"`,
`ochakai backlinks glossary/completed-order`, and `ochakai search --sort
stale_after`. The entries link to each other with ordinary markdown links, so
the graph is already there — nothing declares it.

Deliberately not uniform: two entries are **drafts** so the review queue is not
empty, one `Reference` is **deprecated**, and one draft's `stale_after` has
already passed so the stale feed has an occupant. That is what a real knowledge
base looks like a month in.

`ochakai import` reads every `.md` file as a concept, so it skips this README —
it has no `type` — and says so. That one `skip:` line is expected.

## The numbers are made up

Every figure here is invented: the baselines, the seasonal percentages, the
thresholds, the table and project names, `example.co.jp`. Nothing was measured.
This is a teaching example — read it for the *shape* of an entry and copy that,
not the contents. Importing it into a knowledge base your agents actually use
would teach them confident nonsense about a shop that does not exist.
