# Web UI screenshots

`webui-review.png`, `webui-entry.png`, `webui-wrong.png` and
`webui-tree.png` are captures of the real Web UI serving
[examples/demo](../../examples/demo). They are shot from the committed
bundle rather than a corpus that only ever existed on one machine, so
anyone can reproduce them when the UI changes.

## Plant the knowledge base

Any empty Postgres will do; make a scratch database and drop it when
finished.

```sh
createdb shots   # or: docker exec <pg> psql -U t -d t -c 'CREATE DATABASE shots'
OCHAKAI_DATABASE_URL='postgres://…/shots?sslmode=disable' \
  OCHAKAI_INSECURE_DEV=true PORT=8095 go run ./cmd/ochakai serve &

export OCHAKAI_URL=http://127.0.0.1:8095
go run ./cmd/ochakai import examples/demo --adopt-verified
```

The bundle already carries the two drafts the review queue needs and a
draft whose `stale_after` has passed. The re-verification feed is the one
thing it cannot ship, because a failure report is an event rather than a
document — report one, and read a few entries so the usage counts on the
cards are not all zero:

```sh
go run ./cmd/ochakai report tables/shop-orders failed --note "Ran the daily
  revenue check at 07:40 JST and the day came out 13% short; MAX(created_at)
  was 05:58, so the 06:00 partition had not landed yet."
go run ./cmd/ochakai context "why is revenue down?"
go run ./cmd/ochakai get metrics/revenue
```

Then serve the UI as yourself:

```sh
go run ./cmd/ochakai ui --port 8098
```

## Shoot

Four hash routes, at a 1440 CSS-px width and a device scale factor of 2.
The heights are chosen so each shot ends just under its last card; the UI
is a fixed-viewport shell whose panes scroll internally, so the height is
a crop, not a measurement.

| Route | File | Height |
|---|---|---|
| `#/review` | `webui-review.png` | 570 |
| `#/k/metrics/revenue` | `webui-entry.png` | 340 |
| `#/search/reported-wrong` | `webui-wrong.png` | 375 |
| `#/` | `webui-tree.png` | 510 |

Two things have to be forced, or the pictures come out wrong in ways that
are easy to miss:

- **Light scheme.** Headless Chrome defaults to dark.
- **An `en-US` locale.** The entry page prints its dates with
  `toLocaleDateString()`, which reads ICU's default locale — and on macOS
  that follows the system, so `--lang=en-US`, `--accept-lang` and
  `LANG`/`LC_ALL` all leave it Japanese (`2026年7月27日`) while
  `navigator.language` reports `en-US` and looks fine. Override it over
  the DevTools protocol with `Emulation.setLocaleOverride` (light scheme
  via `Emulation.setEmulatedMedia`), and check the result in the page:
  `Intl.DateTimeFormat().resolvedOptions().locale` must be `en-US`.

The entry route scrolls its sidebar to reveal the selected node, which
clips the panel header; scroll back to the top before capturing.

Downscale each to 1800px wide, which keeps all four under 1 MB together:

```sh
sips --resampleWidth 1800 docs/images/webui-*.png
```

Read every PNG before committing it. Reject any that is dark, dated in
Japanese, mostly empty, or showing an error or empty state — a dead
backend renders as a tidy "502 Bad Gateway" card that is easy to mistake
for content at a glance.
