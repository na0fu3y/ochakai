// One call shape for the whole page: the origin it talks to, the errors
// it turns responses into, the read-only banner, and the toast.

import { $ } from './dom.js';

// The page always talks to its own origin — both serving paths
// (`ochakai ui`, `ochakai serve-ui`) proxy /api/v1 with the right
// credentials (design doc 0006), so a configurable base URL could only
// break that. Opened as a plain file (development), it targets a local
// `ochakai serve`.
export const BASE = location.protocol.startsWith('http') ? location.origin : 'http://localhost:8080';

// READ_ONLY comes from the server's own answer rather than from anything
// configured here, so the page cannot disagree with the deployment it is
// pointed at (design doc 0040 §2.3).
export let READ_ONLY = null;

export function setReadOnly(on) {
  if (READ_ONLY === on) return;
  READ_ONLY = on;
  document.body.classList.toggle('read-only', on);
}

// A sandbox is learned from /api/v1/stats rather than from a header
// (design doc 0087 §4): the wire is frozen at /api/v1 and a new header
// is not a response-only addition, while a field on a response schema
// is. It is asked for once, at startup, because the answer is a property
// of the deployment rather than of a request.
export async function markSandbox() {
  try {
    const s = await api('/api/v1/stats');
    document.body.classList.toggle('sandbox', s.sandbox === true);
  } catch (e) { /* a banner is not worth failing the page over */ }
}

// The archive is a representation of the bundle root, asked for with an
// Accept header (design doc 0046 §3.5) — and a plain <a href> cannot
// send one, so the download goes through fetch and a blob URL.
//
// That means the archive lands in the page's memory before it lands on
// disk, which a streaming <a download> would not have done. It is the
// price of the bundle having one address instead of an endpoint named
// after downloading, and it is bounded by what a curated knowledge base
// holds. A real backup belongs in CI, where `ochakai export` streams it
// (docs/guides/operating.md).
export async function downloadBundle(e) {
  e.preventDefault();
  const res = await fetch(BASE + '/api/v1/bundle/', { headers: { Accept: 'application/gzip' } });
  if (!res.ok) {
    toast('Export failed: ' + res.status);
    return;
  }
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = 'ochakai-okf.tar.gz';
  a.click();
  URL.revokeObjectURL(url);
}

export async function api(path, opts = {}) {
  // Every call here wants the structured form. It matters at the paths
  // that serve a file by default — index.md and log.md are documents,
  // and this page asks for the listing behind them (design doc 0046
  // §§3.7-3.8).
  const init = { method: opts.method || 'GET', headers: { Accept: 'application/json' } };
  if (opts.raw !== undefined) {
    init.body = opts.raw; // raw bytes (file upload), no JSON envelope
  } else if (opts.doc !== undefined) {
    init.headers['Content-Type'] = 'text/markdown';
    init.body = opts.doc; // an OKF document — the one way to write knowledge
  } else if (opts.body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(opts.body);
  }
  if (opts.onlyIfAbsent) {
    init.headers['If-None-Match'] = '*';
  }
  // The version the caller read, echoed back so the write refuses rather
  // than overwrites when the concept moved under it (design doc 0030).
  if (opts.ifMatch) {
    init.headers['If-Match'] = opts.ifMatch;
  }
  const res = await fetch(BASE + path, init);
  // A read-only deployment says so on every response (design doc 0040).
  // The page learns it from whatever request it made first and hides the
  // write affordances; a button that can only ever 403 is a lie.
  setReadOnly(res.headers.get('Ochakai-Read-Only') === 'true');
  if (!res.ok) {
    // Two halves, and the page must not confuse them: `error` is a
    // sentence for a person and may be reworded in any release
    // (docs/compatibility.md), `code` is the part to branch on. Anything
    // here that decides what to *do* reads err.code; err.message is only
    // ever shown.
    let msg = res.status + ' ' + res.statusText, code = '';
    try {
      const text = await res.text();
      try {
        const body = JSON.parse(text);
        msg = body.error || text;
        code = body.code || '';
      } catch { msg = text || msg; }
    } catch { /* keep status text */ }
    const err = new Error(msg);
    err.code = code;
    throw err;
  }
  if (res.status === 204) return null;
  const data = await res.json();
  // The version this read saw, for a later conditional write. It is the
  // ETag the server sent rather than the content hash rebuilt out of the
  // body: the header is the validator, and a page that reassembled it
  // would be a second opinion about quoting that nothing checks.
  //
  // Non-enumerable, so it does not appear in JSON.stringify, in for..in,
  // or in anything this page sends back.
  const etag = res.headers.get('ETag');
  if (etag && data && typeof data === 'object') {
    Object.defineProperty(data, 'etag', { value: etag });
  }
  return data;
}

export function toast(msg) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.remove('show'), 2200);
}
