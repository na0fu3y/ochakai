// Package webui embeds the web UI: a page, a stylesheet and the ES
// modules behind them (no build step, no framework, no CDN) that talk to
// /api/v1 relative to their own origin. The page knows nothing about
// authentication — that is the serving proxy's job, which is what lets
// the same page ship two ways (design doc 0006):
//
//   - `ochakai ui` serves it on loopback and proxies with the caller's
//     own Google identity (human:<email>);
//   - `ochakai serve-ui` serves it as a deployed service (Cloud Run)
//     and proxies with its service identity (process:<sa-email>).
//
// It was one file until the modules were split out of it. What the
// browser fetches is what is written here — the split buys module
// boundaries and tests, not a build.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// Files is the UI as it is served: index.html at the root, app.css
// beside it, and the modules under js/.
var Files, _ = fs.Sub(static, "static")
