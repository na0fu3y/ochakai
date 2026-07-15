// Package webui embeds the sample web UI so the ochakai binary can serve
// it at /ui. It is a single static HTML file — same origin as the REST API
// (no CORS setup), and reachable through `gcloud run services proxy` when
// the service is IAM-restricted. Users building their own UI can copy
// index.html as a starting point or ignore it entirely.
package webui

import _ "embed"

//go:embed index.html
var Index []byte
