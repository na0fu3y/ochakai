// One concept, whole: the document, its provenance, its files, its
// history, and the actions a reader can take on it.

import { applyStatus, moveEntry, rejectEntry, verifyEntry } from '../actions.js';
import { BASE, FILES_VARIABLE, api, toast } from '../api.js';
import { hitCard } from '../cards.js';
import { copyText } from '../clipboard.js';
import { diffHTML, diffLines, diffStats } from '../diff.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { actorStr, conceptURL, crumbTrail, displayTitle, editedSinceVerified, fmtDate, fmtDateTime, fmtSize, idPath, isVerified, lastVerification, parseKPath, provenanceLine, receivedLine, trustOf } from '../format.js';
import { checkTargets } from '../links.js';
import { descHTML, md } from '../markdown.js';
import { headingAnchors, permalinks, tocHTML } from '../outline.js';
import { knownDirs, refreshTree, revealInTree } from '../tree.js';
import { STATUSES, icon } from '../vocab.js';
import { explore, viewExplore } from './explore.js';

// A resource is whatever the writer put there: an external URL, a
// bundle-relative path, a scope descriptor. Only http(s) becomes a real
// link — the same line md() draws for body links — and everything is
// escaped either way, because these strings are user input on their way
// into an attribute.
export function resourceHTML(ref) {
  const s = String(ref ?? '');
  if (!s) return '';
  return /^https?:/.test(s)
    ? `<a class="mono" href="${esc(s)}" target="_blank" rel="noopener">${esc(s)}</a>`
    : `<span class="mono">${esc(s)}</span>`;
}

export const windowText = w => w && (w.from || w.to) ? `${w.from || '…'} → ${w.to || '…'}` : '';

// The tab bar's labels, in one place because the failure message names
// the tab it could not load and would otherwise spell it a second way.
export const TAB_LABELS = {
  overview: '概要',
  document: 'ドキュメント',
  sources: '出典',
  links: 'リンク',
  files: 'ファイル',
  usage: '利用状況',
  history: '履歴',
};

// A source row's plain labelled pairs (OKF SPEC §5.1). The other keys
// the spec gives a source are not here because each has a place of its
// own: `resource` is the address the row leads with, `title` and `id`
// name it, and `usage_count` carries the window it was counted over.
//
// SOURCE_KNOWN is every key that has one of those places, so that what
// is left is a producer's own (SPEC §4.1) and can be drawn under its own
// name rather than dropped — a reading of the document that showed less
// than the document says would be worth less than the document.
const SOURCE_FIELDS = [
  ['author', '作成者'],
  ['last_modified', '最終更新'],
];
const SOURCE_KNOWN = new Set(['resource', 'id', 'title', 'usage_count', 'usage_window',
  ...SOURCE_FIELDS.map(([k]) => k)]);

// A producer's own value, drawn as text. A scalar is what it says; a
// nested shape is JSON, which is at least the shape it has — the
// alternative is `[object Object]`, which is a value the reader cannot
// tell from a bug.
const scalarText = v => (v !== null && typeof v === 'object') ? JSON.stringify(v) : String(v);

// The concepts this session has already found: a body that links to a
// neighbour is usually one of several that do, and a concept that is
// there does not stop being there while a reader walks between them.
//
// Only the positive answer is kept. A missing one is asked again on
// every render, because it is the answer that can change under the
// reader — the page they are looking at offers to write the concept the
// link is missing, and coming back to find the link still red would
// have the page contradicting what it just did.
const found = new Set();

// The links a body draws are routes, so the ids come back out of them
// the same way the router reads them.
const idFromHash = href => parseKPath(String(href).replace(/^#\/k\//, '')).id;

// A page asks about no more than this many neighbours. A probe is a
// whole concept read — the contract has no lighter way to ask whether
// something exists (design doc 0107 froze the core) — and a body citing
// dozens of others is a body whose reader is not waiting on the colour
// of the thirty-first link.
const MAX_PROBES = 30;

// The page behind a link to knowledge nobody has written. A wiki has
// always answered this address with a page rather than an error, and for
// the reason that matters here: the reader arrived holding a name, which
// is the most useful thing anyone has about the concept that is missing.
// So the name is offered back to them twice — as a search over what does
// exist, and, for whoever may write, as the id of a new document.
export async function viewMissing(id) {
  const segs = String(id).split('/');
  const name = segs.at(-1) || id;
  view.innerHTML = `
    <nav class="crumbs mono">${crumbTrail(segs.slice(0, -1))}</nav>
    <div class="detail-head"><h1>${esc(name)}</h1></div>
    <p class="provenance"><code>${esc(id)}</code></p>
    <div class="status-note">このナレッジはまだありません。リンクをたどってここに来た場合は、リンク元が古いか、まだ誰も書いていません。</div>
    <div class="toolbar">
      <button class="btn small" id="missing-search" title="この名前で検索します">🔍「${esc(name)}」を検索</button>
      <a class="btn small write-only" href="#/new/${idPath(id)}" title="この id でナレッジを作成します">＋ ここに作成</a>
    </div>
    <div id="missing-near" style="margin-top:1.2rem"></div>`;
  $('#missing-search').addEventListener('click', () => {
    explore.q = name;
    explore.ageFeed = explore.failedFeed = explore.expiredFeed = false;
    explore.source = '';
    if (location.hash === '#/search') viewExplore();
    else location.hash = '#/search';
  });
  // Whatever the base does have under that name, in place. A search that
  // fails says nothing here: the page's answer is already given above,
  // and an error banner under it would describe a second problem the
  // reader did not have.
  const near = $('#missing-near');
  try {
    const { hits = [] } = await api('/api/v1/search?q=' + encodeURIComponent(name) + '&limit=5');
    if (!near.isConnected || !hits.length) return;
    near.innerHTML = `<div class="section-title">近い名前のナレッジ</div>` + hits.map(hitCard).join('');
  } catch { /* the page stands without it */ }
}

export async function viewDetail(id, heading = '') {
  revealInTree(id); // in parallel with the entry fetch; harmless on 404
  view.innerHTML = '<div class="empty">…</div>';
  let entry, observed, document_;
  try {
    // A read is {id, document, summary, observed, files} (design
    // doc 0043 §3.5). The summary is flattened onto entry so the header
    // reads one object; the document is the entry itself.
    const v = await api(conceptURL(id));
    entry = Object.assign({ id: v.id, files: v.files || [] }, v.summary);
    observed = v.observed || {};
    document_ = v.document || '';
  } catch (e) {
    // A concept that is not there is not an error the reader caused, and
    // an error banner is what a page says when something went wrong.
    // What went wrong here is that somebody followed a link to knowledge
    // nobody has written — which is a page of its own, with the two ways
    // on: find what does exist, or write it.
    if (e.code === 'not_found') { viewMissing(id); return; }
    view.innerHTML = `<div class="error-banner" role="alert">${esc(id)} を読み込めませんでした: ${esc(e.message)}</div>`;
    return;
  }
  // The body, cut from the document at the closing delimiter. Cutting on
  // a delimiter is not parsing: nothing here reads a frontmatter value
  // (design doc 0044 §2.3).
  const docBody = (() => {
    const m = document_.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n([\s\S]*)$/);
    return (m ? m[1] : document_).trim();
  })();

  // The pair the page is read by: generated is who the content stands by
  // and when it last meaningfully changed (OKF SPEC §5.2), verified is
  // who confirmed it and when. Both, and the creation, are one line —
  // grouped under whoever did them, so an actor is named once
  // (format.js, provenanceLine).
  const edited = editedSinceVerified(observed);
  const prov = provenanceLine(entry, observed);

  // The document's own frontmatter, parsed once by the server and shared
  // by everything on this page that needs a key out of it — the claim
  // line below and the 出典 tab. One request either way: the tab used to
  // make it on open, and now the header has already made it, so opening
  // 出典 draws immediately.
  //
  // Posting the bytes rather than naming the concept is deliberate
  // (design doc 0130 §3.3): there is no id, no ETag and nothing stored,
  // the browser still parses no YAML, and a read-only deployment answers
  // it.
  let frontmatterOnce;
  const frontmatter = () => (frontmatterOnce ??= api('/api/v1/frontmatter',
    { method: 'POST', body: { document: document_ } }).catch(() => ({})));

  // A passed stale_after is a prompt to re-check, never a claim the entry
  // is wrong (OKF SPEC §5.5) — the comparison is a plain one against now.
  // A moment still in the future says nothing a reader needs yet, so
  // nothing is drawn until it passes.
  //
  // Date.parse reads both spellings the key takes (design doc 0133), and
  // reads a bare date as the UTC midnight opening it, which is what the
  // server compares too. Comparing the strings instead would have put a
  // datetime and a date in the wrong order on the day they share.
  const staleAt = entry.stale_after ? Date.parse(entry.stale_after) : NaN;
  const staleNote = !Number.isNaN(staleAt) && staleAt <= Date.now()
    ? `<div class="status-note">${esc(entry.stale_after)} から期限切れです。内容を確かめ直してください。</div>`
    : '';

  const tags = (entry.tags || []).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
  const atts = entry.files || [];

  view.innerHTML = `
    <nav class="crumbs mono">${crumbTrail(entry.id.split('/').slice(0, -1))} <span class="badge">${esc(entry.type)}</span>
      <button type="button" class="copy-btn inline" data-copy="ochakai://${esc(entry.id)}"
              data-copy-done="ochakai:// のアドレスをコピーしました。"
              title="このナレッジのアドレスをコピーします" aria-label="このナレッジのアドレスをコピー">ochakai://</button></nav>
    <div class="detail-head">
      <span class="type-ico" style="font-size:1.4rem">${icon(entry.type)}</span>
      <h1>${esc(displayTitle(entry))}</h1>
      <span class="badge status-pick ${esc(entry.status)}" title="ステータスを変更します">
        <span aria-hidden="true">${esc(entry.status)}</span>
        <select class="write-only" id="act-status" aria-label="ステータス">
          ${STATUSES.map(s => `<option value="${s}" ${s === entry.status ? 'selected' : ''}>${s}</option>`).join('')}
        </select>
      </span>
      ${isVerified(entry) ? `<span class="badge verified-mark" title="検証台帳から OKF が導く trust の段です(SPEC §5.3)">✓ ${trustOf(entry)}</span>` : ''}
      ${edited ? `<span class="badge edited-mark" title="${esc(fmtDateTime(lastVerification(observed).at))} の検証より後、${esc(fmtDateTime(edited))} に内容が変わっています。検証は編集で失効しません(OKF SPEC §5.2)ので ✓ はそのままです — 読み直して問題なければ再検証してください">検証後に編集</span>` : ''}
      ${tags}
      <span class="actions write-only">
        <a class="btn small" href="#/edit/${idPath(entry.id)}">編集</a>
        <details class="menu" id="more-menu">
          <summary class="btn small" title="その他の操作" aria-label="その他の操作">⋯</summary>
          <div class="menu-body">
            <button id="act-verify" title="${isVerified(entry)
              ? '検証を追記します。再検証のフィードはこれで解消します'
              : 'あなたによる検証として記録します'}">${isVerified(entry) ? '再検証' : '検証'}</button>
            <button class="danger" id="act-reject" title="理由を残して削除します。理由は履歴に残りますが、同じ id への書き戻しは塞ぎません">却下…</button>
            <button id="act-move" title="別のパスへ移します(参照は自動で書き換わります)">移動…</button>
            <button class="danger" id="act-delete">削除…</button>
          </div>
        </details>
      </span>
    </div>
    <div class="provenance">${esc(prov)}</div>
    <div class="provenance received-claim" id="received-line" hidden></div>
    <div id="move-panel" style="display:none;margin-top:.7rem">
      <div class="toolbar" style="margin-bottom:0">
        <input type="text" id="move-to" class="mono" list="move-dirs" value="${esc(entry.id)}"
               style="max-width:28rem" aria-label="新しいパス">
        <datalist id="move-dirs"></datalist>
        <button class="btn small primary" id="move-go">移動</button>
        <button class="btn small" id="move-cancel">キャンセル</button>
      </div>
      <p class="provenance" style="margin-top:.3rem">ナレッジの新しいパスです。ここを指している参照は自動で書き換わり、リビジョン・利用回数・ファイルもナレッジに付いていきます。</p>
    </div>
    ${entry.status_note ? `<div class="status-note">${esc(entry.status_note)}</div>` : ''}
    ${staleNote}
    ${entry.resource ? `<div class="provenance">resource: ${resourceHTML(entry.resource)}</div>` : ''}
    <div class="tabs" id="tabs">
      <button data-tab="overview" class="active">${TAB_LABELS.overview}</button>
      <button data-tab="document">${TAB_LABELS.document}</button>
      <button data-tab="sources">${TAB_LABELS.sources}</button>
      <button data-tab="links">${TAB_LABELS.links}</button>
      <button data-tab="files">${TAB_LABELS.files}${atts.length ? ' (' + atts.length + ')' : ''}</button>
      <button data-tab="usage">${TAB_LABELS.usage}</button>
      <button data-tab="history">${TAB_LABELS.history}</button>
    </div>
    <div id="tab-body"></div>`;

  // A concept this instance received rather than wrote says who the
  // bundle claimed made and confirmed it. The line stays hidden when
  // there is no claim, which is every concept written here — so a base
  // nobody imported into looks exactly as it did.
  //
  // It is drawn under the ledger's own line and says which one it is,
  // because the two disagree by design: an import rules on nothing
  // (design doc 0009 §3.2), so a document asserting a human confirmed it
  // arrives, correctly, as unverified.
  frontmatter().then(({ values = {} } = {}) => {
    const line = receivedLine(values.received);
    const el = $('#received-line');
    if (!line || !el) return;
    el.textContent = '取り込み時の申告: ' + line;
    el.title = 'バンドルが自分について申告した内容です(設計ドキュメント 0046 §2.2)。'
      + '取り込みは裁定をしないので、上の trust の段はこれでは動きません — '
      + '数えるのは、この ochakai の上で人が行った検証だけです。';
    el.hidden = false;
  });

  // File paths and image resolution (design doc 0008). Body links
  // are OKF bundle references: relative to the entry document's directory
  // (the id's parent), or bundle-root when they start with "/".
  const lastSeg = entry.id.split('/').pop();
  const docDir = entry.id.includes('/') ? entry.id.slice(0, entry.id.lastIndexOf('/')) : '';
  // A file is addressed by the path it lives at (design doc 0046 §3.3).
  // Writing one from here puts it at the entry's canonical <id>/<name>;
  // reading one follows the path the entry reported, which is where an
  // imported file actually is.
  const attPath = path => '/api/v1/bundle/' + idPath(path);
  const attURL = path => BASE + attPath(path);
  const canonicalPath = name => entry.id + '/' + name;
  const normalize = p => {
    const out = [];
    for (const seg of p.split('/')) {
      if (!seg || seg === '.') continue;
      if (seg === '..') out.pop(); else out.push(seg);
    }
    return out.join('/');
  };
  const resolveFile = ref => {
    const p = normalize(ref.startsWith('/') ? ref : (docDir ? docDir + '/' : '') + ref);
    const canonical = a => entry.id + '/' + a.name;
    let hit = atts.find(a => p === a.path || p === canonical(a));
    // Forgiving fallback: names are unique within the entry, so a filename
    // match resolves references written against a layout we don't know.
    hit = hit || atts.find(a => a.name === ref.split('/').pop());
    return hit ? attURL(hit.path || canonicalPath(hit.name)) : null;
  };
  // Links to other entries, resolved the way the server derives them from
  // this same body (design doc 0024): SPEC §6's two forms, bundle-absolute
  // "/x/y.md" and relative "./y.md" against the document's directory. A
  // reference to anything else — a file, an external URL, an
  // anchor, a URI scheme — is not an entry link and stays literal text.
  const resolveEntry = ref => {
    const target = String(ref).split(/[#?]/)[0];
    if (!target) return null;
    let id;
    if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(target)) return null; // a scheme: outside the bundle
    else if (!target.endsWith('.md')) return null; // a file, not an entry
    else {
      const rel = target.slice(0, -'.md'.length);
      id = normalize(rel.startsWith('/') ? rel : (docDir ? docDir + '/' : '') + rel);
    }
    return id ? '#/k/' + idPath(id) : null;
  };

  const tabs = {
    overview: () => {
      // The body with its outline: ids on the headings, and a table of
      // contents when there are enough of them to need a map. The
      // description stays out of both — its headings describe, they do
      // not structure the document.
      const body_ = docBody ? headingAnchors(md(docBody, resolveFile, resolveEntry)) : { html: '', headings: [] };
      // Each heading also gains a link to itself, so a section can be
      // sent as an address rather than described.
      const bodyHTML = body_.html ? permalinks(body_.html, entry.id) : '';
      return `
      ${entry.description ? descHTML(entry.description, 'lead') : ''}
      ${tocHTML(body_.headings)}
      ${docBody ? `<div class="md">${bodyHTML}</div>` : ''}
      ${!entry.description && !docBody ? '<div class="empty">description も本文もありません。</div>' : ''}`;
    },
    // The document, whole and unrendered. It replaced tabs that each
    // redrew a slice of the frontmatter in some other shape — which is
    // the duplication document-first exists to remove, and the reason
    // design doc 0044 puts the format itself in front of the reader.
    document: () => `<pre class="mono">${esc(document_)}</pre>`,
    // What the document derives from (OKF SPEC §5.1), read out of the
    // document by the server rather than by the page: the browser parses
    // no YAML (design doc 0130 §3.3), and this is the same face the
    // editor draws its fields from. The rows are shown as the writer
    // recorded them and nothing is scored — §5.1 leaves weighing them to
    // whoever is reading.
    //
    // They had been visible only as YAML in the document tab, while the
    // body cites them by `sources[].id` in a footnote whose text is the
    // writer's own line — so the address the citation stands on, and when
    // anybody last touched it, were on the page and unreadable.
    sources: () => '<div class="empty">…</div>',
    // Both directions of one graph, in one tab. They were two, and the
    // reader who wanted "what is this connected to" had to know which of
    // two words named the direction they meant before they could look;
    // the rows answer the same question and only the arrow differs, which
    // a heading can say for a third of the tab bar's width.
    //
    // Outgoing is in hand: links come from the body's markdown links
    // (design doc 0024), so the body editor is where they are changed.
    // Incoming is a request, made when the tab is first opened.
    links: () => {
      const links = entry.links || [];
      const rows = links.map(l => {
        const target = String(l.target || '').replace(/^ochakai:\/\//, '');
        const href = target ? '#/k/' + idPath(target) : null;
        const text = l.text || target.split('/').pop() || target;
        return `<tr><td class="k">${esc(text)}</td>
          <td>${href ? `<a class="mono" href="${href}">${esc(target)}</a>` : `<span class="mono">${esc(target)}</span>`}</td></tr>`;
      }).join('');
      return `
        <div class="section-title">リンク先${links.length ? ` (${links.length})` : ''}</div>
        ${rows ? `<table class="kv">${rows}</table>` : '<div class="empty">本文はまだ他のナレッジを指していません。</div>'}
        <div id="linked-sec">${linkedHTML || '<div class="section-title">参照元</div><div class="empty">…</div>'}</div>`;
    },
    // A deployment with no bucket holds markdown concepts only (design
    // doc 0075 §1), so everything that would add a file can only be
    // refused, and the page stops offering it (design doc 0131). Which
    // deployment this is arrives from the server and lands on <body> as
    // a class, so the swap is CSS rather than a branch here: the tab
    // renders the same markup whenever it is opened, and nothing in it
    // has to have waited for /api/v1/stats to come back.
    files: () => {
      const cards = atts.map(a => {
        const isImage = (a.media_type || '').startsWith('image/');
        const thumb = isImage
          ? `<a class="thumb" href="${attURL(a.path)}" target="_blank" rel="noopener"><img src="${attURL(a.path)}" alt="${esc(a.name)}" loading="lazy"></a>`
          : `<a class="thumb thumb-file" href="${attURL(a.path)}" target="_blank" rel="noopener">${a.media_type === 'application/pdf' ? 'PDF' : 'TXT'}</a>`;
        const ref = isImage ? `![${esc(a.name)}](${esc(lastSeg)}/${esc(a.name)})` : `[${esc(a.name)}](${esc(lastSeg)}/${esc(a.name)})`;
        return `
        <div class="card att-card">
          ${thumb}
          <div class="att-meta">
            <span class="mono">${esc(a.name)}</span>
            <span class="meta">${esc(a.media_type || '')} · ${fmtSize(a.size)}${a.created_by && a.created_by.name ? ' · ' + esc(actorStr(a.created_by)) : ''}${a.created_at ? ' · ' + esc(fmtDate(a.created_at)) : ''}</span>
            <span class="meta">本文からの参照: <code>${ref}</code></span>
            <button class="btn small danger write-only" data-remove-file="${esc(a.path)}" data-name="${esc(a.name)}">外す</button>
          </div>
        </div>`;
      }).join('');
      return `
        ${cards || '<div class="empty files-only">ファイルはありません。</div>'}
        <div class="empty files-off">このデプロイはファイルを保存しません。${FILES_VARIABLE
          ? `<br><code>${esc(FILES_VARIABLE)}</code> にバケットを設定すると添付できるようになります。`
          : ''}</div>
        <div class="toolbar write-only files-only" style="margin-top:1rem">
          <input type="file" id="att-file" accept="image/png,image/jpeg,image/webp,application/pdf,text/plain,.txt,.csv,.json" multiple hidden>
          <button class="btn small" id="att-choose">ファイルを選択…</button>
          <span class="provenance" id="att-chosen" style="margin:0"></span>
          <button class="btn small primary" id="att-upload">ファイルを追加</button>
        </div>
        <p class="provenance write-only files-only">本文から参照しておくと
        (<code>![alt](${esc(lastSeg)}/name.png)</code> または <code>[name](${esc(lastSeg)}/name.txt)</code>)、埋め込みできるようになります。</p>`;
    },
    usage: () => '<div class="empty">…</div>',
    history: () => '<div class="empty">…</div>',
  };

  function wireFiles() {
    const fileInput = $('#att-file');
    $('#att-choose').addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', () => {
      $('#att-chosen').textContent = [...fileInput.files].map(f => f.name).join(', ');
    });
    $('#att-upload').addEventListener('click', async () => {
      const files = [...fileInput.files];
      if (!files.length) { toast('先にファイルを選んでください。'); return; }
      try {
        for (const f of files) {
          await api(attPath(canonicalPath(f.name)), { method: 'PUT', raw: f });
        }
        toast(files.length > 1 ? `${files.length} 件のファイルを追加しました。` : 'ファイルを追加しました。');
        viewDetail(entry.id);
      } catch (e) { toast('ファイルの追加に失敗しました: ' + e.message); }
    });
    body.querySelectorAll('[data-remove-file]').forEach(b => b.addEventListener('click', async () => {
      const path = b.dataset.removeFile;
      if (!confirm(`${b.dataset.name} を外しますか？変更はリビジョンとして残ります。`)) return;
      try {
        await api(attPath(path), { method: 'DELETE' });
        toast('ファイルを外しました。');
        viewDetail(entry.id);
      } catch (e) { toast('ファイルを外せませんでした: ' + e.message); }
    }));
  }

  // What the reports said (design doc 0137). The numbers above say how
  // bad it is; this says what was wrong, in the words of the caller that
  // ran it — which is the half a reviewer opening the failed feed came
  // for.
  //
  // Only the reports carrying a note are here, so the heading says the
  // count of notes rather than of reports: the tiles above are the
  // report counts, and two numbers that look alike and mean different
  // things is worse than one.
  const reportsHTML = (reports) => {
    if (!reports || !reports.length) return '';
    const rows = reports.map(r => `
      <li class="report ${r.outcome === 'failed' ? 'failed' : 'worked'}">
        <div class="report-head">
          <span class="badge ${r.outcome === 'failed' ? 'rejected-mark' : 'verified-mark'}">${r.outcome === 'failed' ? '失敗' : '成功'}</span>
          <span>${esc(actorStr(r.by))}</span>
          <span>${esc(fmtDate(r.at))}</span>
        </div>
        <p class="report-note">${esc(r.note)}</p>
      </li>`).join('');
    return `
      <h3 class="report-title">報告された内容(${reports.length} 件)</h3>
      <ul class="reports">${rows}</ul>
      <p class="provenance">note の付いた報告だけを新しい順に最大 10 件。報告の件数は上の成功・失敗です。生の報告は 180 日で消えるので、それより古いものは残っていません。</p>`;
  };

  // The incoming half of the links tab, once it has been fetched. It is
  // held here rather than in the lazy table below because it is half of a
  // tab: what the two directions cost is not the same, and a failure to
  // reach the server about the backlinks must not take the outgoing links
  // — which are already in hand, and are right — off the page with it.
  let linkedHTML = '';
  let asking = false;
  const body = $('#tab-body');
  // Lazily loaded tabs: fetch once on first open, then re-render from the
  // cached template. A failed fetch shows in place and retries next open.
  const lazy = {
    usage: async () => {
      const u = await api('/api/v1/usage/' + idPath(entry.id));
      return () => `
        <div class="stat-tiles">
          <div class="tile"><div class="num">${u.search_hits ?? 0}</div><div class="lbl">検索ヒット</div></div>
          <div class="tile"><div class="num">${u.fetches ?? 0}</div><div class="lbl">取得</div></div>
          <div class="tile"><div class="num">${u.worked ?? 0}</div><div class="lbl">成功</div></div>
          <div class="tile"><div class="num">${u.failed ?? 0}</div><div class="lbl">失敗</div></div>
        </div>
        <p class="provenance">${u.last_used_at ? '最終利用日: ' + esc(fmtDate(u.last_used_at)) : 'まだ使われていません'}</p>
        ${reportsHTML(u.reports)}`;
    },
    // The frontmatter as structure, from the one face that reads it
    // (design doc 0130 §3.3). The document is already in hand, so this
    // posts the bytes this page drew rather than naming the concept:
    // there is no id, no ETag and nothing stored, and a read-only
    // deployment answers it.
    sources: async () => {
      const { values = {} } = await frontmatter();
      const rows = Array.isArray(values.sources) ? values.sources.filter(r => r && typeof r === 'object') : [];
      // The window the counts were counted over is the entry's, and a
      // source may name its own for itself — so the one below the table
      // is the entry's and a row that overrides it says so in place.
      const window_ = windowText(values.usage_window);
      return () => {
        if (!rows.length) return '<div class="empty">この文書は出典を挙げていません。</div>';
        const trs = rows.map(r => {
          const meta = [];
          for (const [key, label] of SOURCE_FIELDS) {
            if (r[key] !== undefined && r[key] !== null && r[key] !== '') meta.push(`${label} ${scalarText(r[key])}`);
          }
          if (r.usage_count !== undefined && r.usage_count !== null) {
            const own = windowText(r.usage_window);
            meta.push(`参照回数 ${scalarText(r.usage_count)}${own ? `(${own})` : ''}`);
          }
          for (const [key, v] of Object.entries(r)) {
            if (!SOURCE_KNOWN.has(key)) meta.push(`${key} ${scalarText(v)}`);
          }
          // The name is the writer's title, and the id behind it when
          // there is none: a source with neither is its address, which
          // the value cell is already showing.
          const name = r.title || r.id || '';
          return `<tr>
            <td class="k">${esc(name)}</td>
            <td>${r.resource ? resourceHTML(r.resource) : '<span class="provenance">場所がありません</span>'}
              ${meta.length ? `<div class="provenance">${esc(meta.join(' · '))}</div>` : ''}
              ${r.id ? `<div class="provenance">本文からの参照: <code>[^${esc(r.id)}]</code></div>` : ''}
            </td></tr>`;
        }).join('');
        return `<table class="kv">${trs}</table>
          ${window_ ? `<p class="provenance">参照回数を数えた期間: ${esc(window_)}</p>` : ''}`;
      };
    },
    history: async () => {
      // ?history is the object's own ledger, at the object's own address
      // (design doc 0046 §3.5); the log.md beside it is the directory's.
      const { revisions } = await api('/api/v1/bundle/' + idPath(entry.id) + '.md?history&limit=200');
      // Newest first, so a revision's neighbor below is what it changed
      // from — the oldest diffs against nothing, and is the document.
      const revs = revisions || [];
      const rows = revs.map((r, i) => {
        const stats = diffStats(diffLines((revs[i + 1] || {}).document || '', r.document || ''));
        const diff = diffHTML((revs[i + 1] || {}).document || '', r.document || '');
        return `
        <tr>
          <td class="k mono">#${r.rev}</td>
          <td>
            <div><strong>${esc(r.change)}</strong> · ${esc(actorStr(r.changed_by))} · ${esc(fmtDate(r.changed_at))}
              <span class="diffstat"><span class="add">+${stats.added}</span> <span class="del">−${stats.removed}</span></span></div>
            ${r.note ? `<div class="status-note">理由: ${esc(r.note)}</div>` : ''}
            ${diff
              ? `<details${i === 0 ? ' open' : ''}><summary style="cursor:pointer;color:var(--muted);font-size:.84rem">差分</summary>${diff}</details>`
              : `<div class="provenance">ドキュメントに変更はありません。</div>`}
            <details><summary style="cursor:pointer;color:var(--muted);font-size:.84rem">ドキュメント</summary>
              <pre>${esc(r.document || '')}</pre></details>
          </td>
        </tr>`;
      }).join('');
      return () => `<table class="kv">${rows}</table>`;
    },
  };
  // What links at this concept — the one question its own document cannot
  // answer (design doc 0106). The read that drew this page carries the
  // first rows of it; this asks search for the whole set, which is what a
  // curator following an edge is after, and it asks once.
  async function fillLinked() {
    if (linkedHTML || asking) return;
    asking = true;
    let html;
    try {
      const { hits = [] } = await api('/api/v1/search?links_to=' + encodeURIComponent(entry.id) + '&limit=100');
      html = linkedHTML = `<div class="section-title">参照元${hits.length ? ` (${hits.length})` : ''}</div>`
        + (hits.length ? hits.map(hitCard).join('') : '<div class="empty">このナレッジを指しているものは、まだありません。</div>');
    } catch (e) {
      // Nothing is cached, so opening the tab again asks again.
      html = `<div class="section-title">参照元</div>
        <div class="error-banner" role="alert">参照元を読み込めませんでした: ${esc(e.message)}</div>`;
    } finally {
      asking = false;
    }
    // The reader may have walked on while the request was out; the
    // section this answers is the one that asked.
    const sec = $('#linked-sec');
    if (sec) {
      sec.innerHTML = html;
      markDead();
    }
  }

  // Links to concepts that are not there, drawn as what they are. The
  // verdict is this render's, and the marking runs after every tab
  // render — a tab body is redrawn from its template each time it opens,
  // which throws the classes away with the markup.
  const missing = new Set();
  function markDead() {
    for (const a of body.querySelectorAll('a[href^="#/k/"]')) {
      if (missing.has(idFromHash(a.getAttribute('href')))) {
        a.classList.add('dead');
        a.title = 'リンク先のナレッジがありません';
      }
    }
  }

  async function showTab(name) {
    document.querySelectorAll('#tabs button').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
    body.innerHTML = tabs[name]();
    if (name === 'files') wireFiles();
    if (name === 'links') fillLinked();
    if (lazy[name]) {
      try {
        tabs[name] = await lazy[name]();
        delete lazy[name];
        if (document.querySelector('#tabs button.active')?.dataset.tab === name) body.innerHTML = tabs[name]();
      } catch (e) {
        if (document.querySelector('#tabs button.active')?.dataset.tab === name) {
          body.innerHTML = `<div class="error-banner" role="alert">${esc(TAB_LABELS[name] || name)} を読み込めませんでした: ${esc(e.message)}</div>`;
        }
      }
    }
    markDead();
  }
  document.querySelectorAll('#tabs button').forEach(b => b.addEventListener('click', () => showTab(b.dataset.tab)));
  showTab('overview');

  // A heading named by the route: the overview renders synchronously
  // above, so the element is there to scroll to. getElementById rather
  // than a selector, because an anchor is the heading's own words and a
  // Japanese one would have to be escaped to sit in a selector.
  if (heading) document.getElementById(heading)?.scrollIntoView();

  // ¶ — the reader gets the address in the bar and on the clipboard,
  // and the view is not redrawn to give it to them. replaceState leaves
  // route._current holding the previous hash until the next navigation;
  // its only reader is the editor's unsaved-changes restore, which
  // cannot be open behind this view.
  //
  // The listener sits on the tab body rather than on the view, because
  // the view outlives every render and would collect one of these per
  // concept opened.
  body.addEventListener('click', e => {
    const a = e.target.closest && e.target.closest('a.hlink');
    if (!a) return;
    e.preventDefault();
    const href = a.getAttribute('href');
    history.replaceState(null, '', href);
    document.getElementById(parseKPath(href.replace(/^#\/k\//, '')).heading)?.scrollIntoView();
    copyText(location.href, '見出しへのリンクをコピーしました。');
  });

  // Ask about the neighbours this body names — the ones this session has
  // not already found.
  (async () => {
    const targets = new Set();
    for (const l of entry.links || []) {
      const t = String(l.target || '').replace(/^ochakai:\/\//, '');
      if (t) targets.add(t);
    }
    for (const a of body.querySelectorAll('a[href^="#/k/"]')) targets.add(idFromHash(a.getAttribute('href')));
    // A heading's own ¶ is a link to this concept, which is the one
    // concept on the page already known to be there.
    targets.delete(entry.id);
    const ask = [...targets].filter(t => t && !found.has(t)).slice(0, MAX_PROBES);
    if (!ask.length) return;
    // Whatever the probes did not find out about is left in neither set,
    // so a server that was briefly unreachable teaches this session
    // nothing rather than teaching it that every neighbour is fine.
    const seen = await checkTargets(ask, t => api(conceptURL(t)), 4);
    for (const t of seen.found) found.add(t);
    for (const t of seen.dead) missing.add(t);
    // The reader may have walked on while the probes were out; the view
    // holding this body is the one that asked.
    if (body.isConnected) markDead();
  })();

  // Status is a picker, not a row of transition buttons: any transition
  // the API allows is offered (provenance over authorization, design doc
  // 0002). It is the lifecycle value alone — confirming and rejecting are
  // rulings with their own actions in the ⋯ menu. Deprecating asks for a
  // reason; a cancelled prompt puts the picker back.
  const statusPick = $('#act-status');
  statusPick.addEventListener('change', async () => {
    const status = statusPick.value;
    try {
      if (await applyStatus(entry.id, status)) {
        toast(`ステータスを ${status} にしました。`);
        refreshTree(); // the tree shows status badges
        viewDetail(entry.id);
        return;
      }
    } catch (e) { toast('ステータスを変更できませんでした: ' + e.message); }
    statusPick.value = entry.status;
  });
  // The Move panel: a destination input seeded with the current path,
  // with every directory the tree has loaded (plus the root) offered as
  // a completion — pick one or edit the path directly.
  const closeMenu = () => { $('#more-menu').open = false; };
  $('#act-verify')?.addEventListener('click', async () => {
    closeMenu();
    try {
      await verifyEntry(entry.id);
      toast(isVerified(entry) ? '再検証しました。' : '検証しました。');
      viewDetail(entry.id);
    } catch (e) { toast('検証できませんでした: ' + e.message); }
  });
  $('#act-reject')?.addEventListener('click', async () => {
    closeMenu();
    const note = prompt('却下の理由をご記入ください。このナレッジは削除され、理由が裁定として残ります(履歴は残ります)。', '');
    if (note === null || note.trim() === '') return;
    try {
      await rejectEntry(entry.id, note);
      toast('却下しました。');
      refreshTree();
      location.hash = '#/';
    } catch (e) { toast('却下できませんでした: ' + e.message); }
  });
  $('#act-move').addEventListener('click', () => {
    closeMenu();
    $('#move-panel').style.display = '';
    const last = entry.id.split('/').pop();
    $('#move-dirs').innerHTML = knownDirs().map(p => `<option value="${esc(p + last)}">`).join('');
    $('#move-to').focus();
  });
  $('#move-cancel').addEventListener('click', () => { $('#move-panel').style.display = 'none'; });
  $('#move-go').addEventListener('click', () => moveEntry(entry.id, $('#move-to').value.trim()));
  $('#move-to').addEventListener('keydown', e => {
    if (e.key === 'Enter') moveEntry(entry.id, $('#move-to').value.trim());
  });
  $('#act-delete').addEventListener('click', async () => {
    closeMenu();
    if (!confirm(`${entry.id} を削除しますか？リビジョンの履歴は残ります。`)) return;
    try {
      await api(conceptURL(entry.id), { method: 'DELETE' });
      toast('削除しました。');
      refreshTree();
      location.hash = '#/';
    } catch (e) { toast('削除できませんでした: ' + e.message); }
  });
}
