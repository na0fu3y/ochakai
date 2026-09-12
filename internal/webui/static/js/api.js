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

// Which posture the deployment is in is learned from /api/v1/stats
// rather than from a header (design doc 0087 §4): the wire is frozen at
// /api/v1 and a new header is not a response-only addition, while a
// field on a response schema is. It is asked for once, at startup,
// because the answer is a property of the deployment, not of a request.
export async function markPosture() {
  try {
    const s = await api('/api/v1/stats');
    document.body.classList.toggle('sandbox', s.sandbox === true);
    // The same mechanism for the mode beside it (design doc 0087): a
    // deployment that does not say what it is takes what you write. The
    // banners are separate because the sentence is — one says what you
    // write will be erased, the other says nobody here is who they say.
    document.body.classList.toggle('insecure-dev', s.insecure_dev === true);
    markCapabilities(s);
    markVersion(s.version);
  } catch (e) { /* a banner is not worth failing the page over */ }
}

// Which builds are answering, in the topbar beside the name (design doc
// 0087 §4's shape, applied to the question somebody asks once rather
// than to a posture). Two of them, because there are two: `ochakai
// serve` does not serve this page, so the build that handed the browser
// these files is always a different process from the one answering
// /api/v1, and the deploy tooling has a variable whose only purpose is
// to put them on different versions for one apply (`webui_image_tag`).
//
// One number while they agree — that number is both, and labelling it
// twice would spend the topbar on a distinction nobody has. Both, named,
// the moment they differ: that is the state the operating guide's first
// upgrade trap is about, and its damage (a web UI older than the API
// passes the delegation header through instead of stripping it, design
// doc 0064) is silent everywhere else.
//
// Neither side is guessed at. A server too old to carry the field and a
// page served by something that is not one of the two commands each go
// unnamed rather than assumed, and if that leaves nothing to say the
// element stays hidden, as it was before either half existed.
function markVersion(server) {
  const page = servedBy();
  if (!page && !server) return;
  const el = $('#ver');
  if (page && server && page !== server) {
    el.textContent = `UI ${page} / API ${server}`;
    el.title = 'このページを配ったビルドと、応答している API のビルドが違う。'
      + 'ページ側は API より前か API と一緒に上げること — 後にすると書き込みの帰属が静かに壊れる';
    el.classList.add('differs');
  } else {
    // One of them, or the one that spoke. The label stays on when only
    // one side answered: an unlabelled number reads as the deployment's,
    // and half of one is not that.
    el.textContent = page && server ? server : `${page ? 'UI' : 'API'} ${page || server}`;
  }
  el.hidden = false;
}

// The build that served this page, written into the meta by `ochakai ui`
// / `ochakai serve-ui` as they served it. The token survives when the
// page is opened as a plain file, which is not a version — it is the one
// way this page is read by something that did not serve it.
const UI_VERSION_TOKEN = '__OCHAKAI_UI_VERSION__';

function servedBy() {
  const v = document.querySelector('meta[name="ochakai-ui-version"]')?.content || '';
  return v === UI_VERSION_TOKEN ? '' : v;
}

// What this deployment cannot do, and who gets told how to change it
// (design doc 0131).
//
// The capability is everybody's, because everybody is who a button that
// can only fail would have lied to — so the affordance goes away for
// every caller, the way the write affordances go away on a read-only
// deployment. The variable that turns it on is an operator's sentence,
// and it arrives only for a caller the server considers an
// administrator: **the page never decides who is who**, it renders what
// the answer carried (design doc 0130 §1). It is kept here and drawn on
// the files tab — the one place somebody meets the refusal — rather
// than as a page-wide banner.
export let FILES_VARIABLE = '';
function markCapabilities(s) {
  // Absent is not "off". A server that predates the field answers
  // nothing here, and the page goes on offering what it always offered
  // — guessing would hide a working upload on every older deployment.
  const files = s.files;
  if (!files || files.enabled !== false) return;
  document.body.classList.add('no-files');
  FILES_VARIABLE = files.variable || '';
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
    toast('書き出しに失敗しました: ' + res.status);
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

// ms is how long it stays. The default is sized for "保存しました。" —
// a sentence that is read in a glance and confirms what the reader
// already expected. A toast that reports a reinterpretation is neither
// (design doc 0113 §4): it names a line of the document and what the
// server made of it, so the caller asks for longer.
export function toast(msg, ms = 2200) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.remove('show'), ms);
}
