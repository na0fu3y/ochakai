# Sample web UI

A browser UI for the ochakai REST API ([api/openapi.yaml](../../api/openapi.yaml)),
in the spirit of metadata catalogs like OpenMetadata but with ochakai's
constraints kept on purpose: **one self-contained page** — no build step,
no framework, no CDN — so it stays easy to read, fork, and rebrand. The
page itself lives in [internal/webui/index.html](../../internal/webui/index.html)
and ships two ways: `ochakai ui` serves it locally with *your* Google
identity (zero deploy), and this directory deploys it on Cloud Run as a
team-shared service (design doc
[0006](../../docs/design/0006-web-ui-serving.md)). It currently covers
the whole API surface:

- **Explore** — search with type / status / tag filters, plus the
  *verification age* feed (`sort=verified_at`, the canary feed for
  re-checking golden queries).
- **Entry pages** — overview (markdown body), attributes (SQL et al.),
  links between entries, and usage counts; with the status workflow as
  one-click actions (verify / deprecate / reject with a `status_note`,
  soft-delete).
- **Create & edit** — drafts by default, hierarchical IDs, attrs as JSON.
- **Compile** — metrics + dimensions + filters + time grain → SQL, with
  verified golden queries shown alongside.
- **Export OKF** — one click, your knowledge is yours.

This started as a minimal sample and is growing toward a standalone web UI.
It will stay a thin client of the public REST API — anything the UI can do,
an agent can do — and it will stay simple; features that need server-side
state or an LLM belong elsewhere.

## Run it

The quickest way needs no deployment at all — the CLI serves the same
page on loopback, proxying API calls with your own gcloud/ADC identity
(edits are recorded as `human:<you>`):

```sh
ochakai ui    # http://127.0.0.1:8098, against the `ochakai use` selection
```

To run *this* sample locally, against the compose stack from the repo root:

```sh
docker compose -f deploy/compose.yaml up      # ochakai on :8080
PORT=8090 OCHAKAI_URL=http://localhost:8080 go run ./examples/webui
```

Then open http://localhost:8090. `main.go` serves the static page and
reverse-proxies `/api/v1` (and `/mcp`) to ochakai — same-origin, so no CORS.
On Cloud Run it attaches this service's identity token, so the ochakai
deployment stays IAM-restricted and browser users are recorded as
`agent:<sa-email>` (see the comment in [main.go](main.go)).

Without `OCHAKAI_URL` it serves the static page only; the page then calls
whatever base URL you set via the chip in the top-right corner.

To deploy it on Cloud Run as its own IAP-fronted service next to an
IAM-restricted ochakai — including why it is a separate service and the
identity trade-offs versus `ochakai ui` — follow
[deploy/cloudrun/README.md §5b](../../deploy/cloudrun/README.md#5b-optional-the-sample-web-ui-behind-iap-separate-service-by-design).
