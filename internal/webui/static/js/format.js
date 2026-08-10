// Pure functions from stored values to what a reader sees: dates, ages,
// sizes, actors, and the hashes and paths an id turns into. Nothing here
// touches the network, and only crumbTrail touches the DOM's escaping —
// which is what makes this the half of the page a test can hold.

import { esc } from './escape.js';

export function fmtDate(s) {
  if (!s) return '';
  const d = new Date(s);
  return isNaN(d) ? s : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
// The delegating caller is part of the name, never dropped: an entry
// written by a person through an embedded application must not read like
// one they wrote themselves (design doc 0027). This is the curation
// surface, so it is where the distinction has to be visible.
// The producer is shown for the same reason and with the same care: it is
// what the caller said it was running, so a reviewer deciding whether to
// trust a draft can see that a model wrote it — and which build (design
// doc 0052).
export function actorStr(a) {
  if (!a || !a.name) return '';
  const notes = [a.via ? a.via + ' 経由' : '', a.producer ? a.producer + ' 使用' : ''].filter(Boolean);
  return (a.kind === 'human' ? '👤 ' : '🤖 ') + a.name +
    (notes.length ? '(' + notes.join('・') + ')' : '');
}
// The ledgers a read carries beside the document (design doc 0043
// §§3.2-3.3). Verification is plural and stored oldest-first, so the
// newest is the last one.
export function lastVerification(observed) {
  const vs = (observed || {}).verified || [];
  return vs.length ? vs[vs.length - 1] : null;
}
// A summary answers "who confirmed this" with OKF's tier (SPEC §5.3);
// the ledger itself travels in observed (design docs 0043 §3.5, 0046
// §3.10).
export function trustOf(e) { return e.trust || 'unverified'; }
export function isVerified(e) { return trustOf(e) !== 'unverified'; }

// Whole days since an ISO timestamp (null for unparseable input).
export function daysSince(s) {
  if (!s) return null;
  const d = new Date(s);
  return isNaN(d) ? null : Math.floor((Date.now() - d.getTime()) / 86400000);
}
export function fmtAge(days) {
  if (days === null) return '';
  if (days <= 0) return '今日';
  if (days === 1) return '昨日';
  return days + ' 日前';
}
export function fmtSize(n) {
  if (!n && n !== 0) return '';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB';
  return (n / (1024 * 1024)).toFixed(1) + ' MB';
}
// URL path for an entry; the id is a path and its slashes stay real
// separators (design doc 0016).
export function idPath(id) {
  return String(id).split('/').map(encodeURIComponent).join('/');
}
export const entryHash = e => '#/k/' + idPath(e.id);
// API path for a concept: it lives in the bundle at its id plus `.md`,
// which is the only address it has (design doc 0046 §3.5).
export const conceptURL = id => '/api/v1/bundle/' + idPath(id) + '.md';
// Display name: the title when set, else the id's last segment — with
// title optional, the filename usually is the name (design doc 0022).
export const displayTitle = e => e.title || String(e.id).split('/').pop();

// URL for a directory's index page; accepts "a/b" or "a/b/".
export const dirHash = p => '#/dir/' + idPath(String(p).replace(/\/+$/, ''));

// crumbTrail renders a breadcrumb of directories: each links to its
// index page and the root "/" goes home (which shows the root index). A
// separator belongs to the directory it closes ("insights/"), so the
// whole crumb is one click target. `leaf` is the current page's own
// segment, shown as plain text — a directory page names itself that way,
// while an entry's breadcrumb stops at its folder (the entry's own name
// is right below it, as the heading).
export function crumbTrail(dirs, leaf) {
  let prefix = '', out = '<a href="#/" title="ルートの索引">/</a>';
  for (const d of dirs) {
    prefix += d + '/';
    out += `<a href="${dirHash(prefix)}">${esc(d)}/</a>`;
  }
  return leaf === undefined ? out : out + esc(leaf);
}
