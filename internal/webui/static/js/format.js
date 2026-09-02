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
// The same instant with its clock time. Provenance is drawn by the day,
// which is the right grain for a line a reader skims — but two events
// inside one day then print as one date, and the pair the trust chip
// hangs on (confirmed, then changed) is exactly a pair that can land
// minutes apart. Where the order is the point, the order is shown.
export function fmtDateTime(s) {
  if (!s) return '';
  const d = new Date(s);
  return isNaN(d) ? s : d.toLocaleString(undefined,
    { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
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
// The header's provenance line: when this concept was written,
// confirmed and last changed, and who each of those was.
//
// An actor is named once. A delegated identity is two identities wide —
// the caller is kept beside the person it acted for, never dropped
// (design doc 0065 §3) — so a concept somebody created and later edited
// through the same embedded application printed that pair twice, and
// filled a phone's header with one name repeated. Nothing is dropped
// here either: the events are grouped under the actor who did them, in
// the order that actor first appears, and an event whose actor is
// unknown groups with the others that have none.
export function provenanceLine(entry, observed) {
  const o = observed || {};
  const lv = lastVerification(o);
  // The 更新 date is generated.at — when the content last meaningfully
  // changed (OKF SPEC §5.2) — rather than updated_at, which moves for a
  // write that only reformatted the document; drawing generated.by
  // beside updated_at named a date generated never claimed (design doc
  // 0064 fixed the same mismatch in the JSON).
  const changedAt = (o.generated || {}).at || entry.updated_at;
  const events = [
    { text: `作成 ${fmtDate(entry.created_at)}`, by: o.created_by },
    lv
      ? {
        text: `検証 ${fmtDate(lv.at)}`
          + ((o.verified || []).length > 1 ? `(計 ${o.verified.length} 件)` : ''),
        by: lv.by,
      }
      : null,
    changedAt && changedAt !== entry.created_at
      ? { text: `更新 ${fmtDate(changedAt)}`, by: (o.generated || {}).by }
      : null,
  ].filter(Boolean);

  return groupedByActor(events);
}
// The grouping both provenance lines share: events under the actor who
// did them, in the order that actor first appears. Shared because the
// claim a bundle carries is read the same way as the ledger this
// instance keeps — one reading, two sources.
function groupedByActor(events) {
  const groups = [];
  for (const e of events) {
    const who = actorStr(e.by);
    const g = groups.find(x => x.who === who);
    if (g) g.texts.push(e.text);
    else groups.push({ who, texts: [e.text] });
  }
  return groups.map(g => (g.who ? g.who + ' が' : '') + g.texts.join('・')).join(' · ');
}
// An actor as a *document* spells one, which is not the shape a read
// hands back. OKF writes `<kind>:<name>` (SPEC §7), but a hand-written
// bundle usually carries a bare producer string instead — `analysis_agent/
// claude-fable-5` — and a colon inside one of those must not be mistaken
// for a kind. Only the kinds ochakai names are read as kinds; everything
// else is the name entire, and anything that is not a person is
// something running, so the icon follows actorStr's rule rather than a
// second one.
export function parseActorText(s) {
  if (typeof s !== 'string' || !s.trim()) return null;
  const i = s.indexOf(':');
  const kind = i > 0 ? s.slice(0, i) : '';
  if (kind === 'human' || kind === 'process') return { kind, name: s.slice(i + 1) || s };
  return { kind: 'process', name: s };
}
// What an imported document claimed about itself, which the store keeps
// beside the concept as `received` (design doc 0046 §2.2).
//
// This is a claim and never a ruling: an import rules on nothing, so the
// trust tier above is unmoved by anything here (design doc 0009 §3.2).
// Drawing it is the whole point — the bundle says an agent wrote this
// and a person confirmed it, and until now the only place a reader could
// see that was the raw YAML one tab over.
export function receivedLine(received) {
  const r = received || {};
  const gen = r.generated || {};
  const vs = (Array.isArray(r.verified) ? r.verified : []).filter(v => v && (v.at || v.by));
  const last = vs.length ? vs[vs.length - 1] : null;
  const events = [
    gen.at || gen.by
      ? { text: gen.at ? `生成 ${fmtDate(gen.at)}` : '生成', by: parseActorText(gen.by) }
      : null,
    last
      ? {
        text: (last.at ? `検証 ${fmtDate(last.at)}` : '検証')
          + (vs.length > 1 ? `(計 ${vs.length} 件)` : ''),
        by: parseActorText(last.by),
      }
      : null,
  ].filter(Boolean);
  return events.length ? groupedByActor(events) : '';
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
// When the content moved after the newest confirmation, this is the
// instant it moved; otherwise the empty string.
//
// The ledger keeps every confirmation, and a confirmation never claims
// the content stayed put afterwards — what an edit takes away is the
// tier, which stands only on a verification of the current content
// (design doc 0138). So nothing here is a state the server stores: it is
// the two ledgers read side by side, which is the reading the spec
// leaves to the consumer. generated.at is the last *meaningful*
// change, so a write that only reformatted the document does not raise
// it and does not raise this (unlike updated_at, which moves for any
// write at all).
export function editedSinceVerified(observed) {
  const v = lastVerification(observed);
  const at = ((observed || {}).generated || {}).at;
  if (!v || !at) return '';
  const changed = Date.parse(at), confirmed = Date.parse(v.at);
  return !isNaN(changed) && !isNaN(confirmed) && changed > confirmed ? at : '';
}

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
// A link to one section of one concept. A bare "#anchor" cannot do this
// job: the page routes on the hash, so an address holding only an anchor
// has lost the concept it was an anchor within — reloading it lands on
// the home page. The heading rides in the route instead, as a query
// suffix an anchor can never contain (headingAnchor drops `#%?`), which
// is what makes the split below unambiguous.
export const headingHash = (id, anchor) => '#/k/' + idPath(id) + '?h=' + encodeURIComponent(anchor);
// The inverse, run on the still-encoded path so that a `?h=` a writer
// put inside a segment survives as text: the route's own separator is
// the first one, and the id is decoded segment by segment after the cut.
export function parseKPath(raw) {
  const s = String(raw ?? '');
  const at = s.indexOf('?h=');
  const idPart = at === -1 ? s : s.slice(0, at);
  const heading = at === -1 ? '' : dec(s.slice(at + 3));
  return { id: idPart.split('/').map(dec).join('/'), heading };
}
// A half-typed or truncated address — "%zz", or a "%E3%81" a chat client
// cut in two — is a bad address, not an exception. Throwing out of the
// router leaves the page showing the concept the reader was on while the
// bar names another one; the raw text finds nothing, which says what
// happened.
function dec(s) {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}
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
  let prefix = '', out = '<a href="#/" title="ルートの索引を開きます">/</a>';
  for (const d of dirs) {
    prefix += d + '/';
    out += `<a href="${dirHash(prefix)}">${esc(d)}/</a>`;
  }
  return leaf === undefined ? out : out + esc(leaf);
}
