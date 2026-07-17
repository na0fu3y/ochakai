// Package webui embeds the web UI: one self-contained index.html (no
// build step, no framework, no CDN) that talks to /api/v1 relative to
// its own origin. The page knows nothing about authentication — that is
// the serving proxy's job, which is what lets the same page ship two
// ways (design doc 0006):
//
//   - `ochakai ui` serves it on loopback and proxies with the caller's
//     own Google identity (human:<email>);
//   - examples/webui serves it on Cloud Run and proxies with its
//     service identity (agent:<sa-email>).
package webui

import _ "embed"

// Index is the whole UI.
//
//go:embed index.html
var Index []byte
