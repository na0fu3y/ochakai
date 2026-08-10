// One concept, whole: the document, its provenance, its files, its
// history, and the actions a reader can take on it.

import { applyStatus, liftRejection, moveEntry, rejectEntry, verifyEntry } from '../actions.js';
import { BASE, api, toast } from '../api.js';
import { hitCard } from '../cards.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { actorStr, conceptURL, crumbTrail, displayTitle, fmtDate, fmtSize, idPath, isVerified, lastVerification, trustOf } from '../format.js';
import { descHTML, md } from '../markdown.js';
import { knownDirs, refreshTree, revealInTree } from '../tree.js';
import { STATUSES, icon } from '../vocab.js';

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

// The material an entry derives from (OKF SPEC §5.1). ochakai shows the
// signals as the writer recorded them and scores nothing: §5.1 leaves
// weighing them to whoever is reading.

export async function viewDetail(id) {
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
    view.innerHTML = `<div class="error-banner">${esc(id)} を読み込めませんでした: ${esc(e.message)}</div>`;
    return;
  }
  // The body, cut from the document at the closing delimiter. Cutting on
  // a delimiter is not parsing: nothing here reads a frontmatter value
  // (design doc 0044 §2.3).
  const docBody = (() => {
    const m = document_.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n([\s\S]*)$/);
    return (m ? m[1] : document_).trim();
  })();

  const prov = [
    observed.created_by && observed.created_by.name ? `created by ${actorStr(observed.created_by)} ${fmtDate(entry.created_at)}` : `created ${fmtDate(entry.created_at)}`,
    lastVerification(observed)
      ? `verified by ${actorStr(lastVerification(observed).by)} ${fmtDate(lastVerification(observed).at)}`
        + ((observed.verified || []).length > 1 ? ` (${observed.verified.length} verifications)` : '')
      : '',
    observed.rejection ? `rejected by ${actorStr(observed.rejection.by)} ${fmtDate(observed.rejection.at)}` : '',
    // updated_by is OKF's generated.by: who the content stands by now,
    // which is not always who created it (design doc 0036 §3.3).
    entry.updated_at && entry.updated_at !== entry.created_at
      ? ((observed.generated || {}).by && observed.generated.by.name
          ? `updated by ${actorStr(observed.generated.by)} ${fmtDate(entry.updated_at)}`
          : `updated ${fmtDate(entry.updated_at)}`)
      : '',
  ].filter(Boolean).join(' · ');

  // A passed stale_after is a prompt to re-check, never a claim the entry
  // is wrong (OKF SPEC §5.5) — the comparison is a plain date one.
  const staleNote = entry.stale_after
    ? (entry.stale_after <= new Date().toISOString().slice(0, 10)
        ? `<div class="status-note">Stale since ${esc(entry.stale_after)} — due for a re-check.</div>`
        : `<div class="provenance">stale after ${esc(entry.stale_after)}</div>`)
    : '';

  const tags = (entry.tags || []).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
  const atts = entry.files || [];

  view.innerHTML = `
    <nav class="crumbs mono">${crumbTrail(entry.id.split('/').slice(0, -1))} <span class="badge">${esc(entry.type)}</span></nav>
    <div class="detail-head">
      <span class="type-ico" style="font-size:1.4rem">${icon(entry.type)}</span>
      <h1>${esc(displayTitle(entry))}</h1>
      <span class="badge status-pick ${esc(entry.status)}" title="ステータスを変える">
        <span aria-hidden="true">${esc(entry.status)}</span>
        <select class="write-only" id="act-status" aria-label="ステータス">
          ${STATUSES.map(s => `<option value="${s}" ${s === entry.status ? 'selected' : ''}>${s}</option>`).join('')}
        </select>
      </span>
      ${isVerified(entry) ? `<span class="badge verified-mark" title="検証台帳から OKF が導く trust の段(SPEC §5.3)">✓ ${trustOf(entry)}</span>` : ''}
      ${entry.rejected ? '<span class="badge rejected-mark" title="レビューされ、受け入れられなかった">rejected</span>' : ''}
      ${tags}
      <span class="actions write-only">
        <a class="btn small" href="#/edit/${idPath(entry.id)}">編集</a>
        <details class="menu" id="more-menu">
          <summary class="btn small" title="その他の操作" aria-label="その他の操作">⋯</summary>
          <div class="menu-body">
            <button id="act-verify" title="${isVerified(entry)
              ? 'もう一度確かめて、やはり正しい — 検証を追記する。レビューのフィードが片付くのはこれによる'
              : 'この concept を確認する — あなたによる検証として記録されます'}">${isVerified(entry) ? '再検証' : '検証'}</button>
            ${entry.rejected
              ? `<button id="act-withdraw" title="裁定を取り下げ、concept を通常の扱いに戻す">却下を取り下げる</button>`
              : `<button class="danger" id="act-reject" title="レビューして受け入れなかった — 検索から隠れ、理由は次のエージェントのために残る">却下…</button>`}
            <button id="act-move" title="別のパスへ移す(参照は書き換えられる)">移動…</button>
            <button class="danger" id="act-delete">削除…</button>
          </div>
        </details>
      </span>
    </div>
    <div class="provenance">${esc(prov)}</div>
    ${observed.rejection && observed.rejection.note
      ? `<div class="status-note">Rejected: ${esc(observed.rejection.note)}</div>` : ''}
    <div id="move-panel" style="display:none;margin-top:.7rem">
      <div class="toolbar" style="margin-bottom:0">
        <input type="text" id="move-to" class="mono" list="move-dirs" value="${esc(entry.id)}"
               style="max-width:28rem" aria-label="新しいパス">
        <datalist id="move-dirs"></datalist>
        <button class="btn small primary" id="move-go">移動</button>
        <button class="btn small" id="move-cancel">キャンセル</button>
      </div>
      <p class="provenance" style="margin-top:.3rem">concept の新しいパスです。ここを指している参照は
      書き換えられ、リビジョン・利用回数・ファイルも concept に付いていきます。</p>
    </div>
    ${entry.status_note ? `<div class="status-note">${esc(entry.status_note)}</div>` : ''}
    ${staleNote}
    ${entry.resource ? `<div class="provenance">resource: ${resourceHTML(entry.resource)}</div>` : ''}
    <div class="tabs" id="tabs">
      <button data-tab="overview" class="active">概要</button>
      <button data-tab="document">ドキュメント</button>
      <button data-tab="links">Links${entry.links && entry.links.length ? ' (' + entry.links.length + ')' : ''}</button>
      <button data-tab="files">Files${atts.length ? ' (' + atts.length + ')' : ''}</button>
      <button data-tab="linked">ここを指しているもの</button>
      <button data-tab="usage">利用</button>
      <button data-tab="history">履歴</button>
    </div>
    <div id="tab-body"></div>`;

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
    // Sources sit under the prose they support rather than in a tab of
    // their own: OKF's own model is body footnotes keyed to source ids, so
    // whoever is reading the body is exactly who needs the list.
    overview: () => `
      ${entry.description ? descHTML(entry.description, 'lead') : ''}
      ${docBody ? `<div class="md">${md(docBody, resolveFile, resolveEntry)}</div>` : ''}
      ${!entry.description && !docBody ? '<div class="empty">description も本文もありません。</div>' : ''}`,
    // The document, whole and unrendered. It replaced tabs that each
    // redrew a slice of the frontmatter in some other shape — which is
    // the duplication document-first exists to remove, and the reason
    // design doc 0044 puts the format itself in front of the reader.
    document: () => `
      <p class="provenance" style="margin:0 0 .8rem">ochakai が保存しているままの concept —
      OKF の frontmatter と markdown です。「編集」が開くのもこのテキストで、export が書き出すのもこれです。</p>
      <pre class="mono">${esc(document_)}</pre>`,
    // Read-only: links come from the body's markdown links (design doc
    // 0024), so the body editor is where they are changed.
    links: () => {
      const links = entry.links || [];
      if (!links.length) return '<div class="empty">本文はまだ他の concept を指していません。</div>';
      return `<p class="provenance" style="margin:0 0 .8rem">本文の markdown リンクから取られている — 変えるには本文を編集する。</p>`
        + '<table class="kv">' + links.map(l => {
        const target = String(l.target || '').replace(/^ochakai:\/\//, '');
        const href = target ? '#/k/' + idPath(target) : null;
        const text = l.text || target.split('/').pop() || target;
        return `<tr><td class="k">${esc(text)}</td>
          <td>${href ? `<a class="mono" href="${href}">${esc(target)}</a>` : `<span class="mono">${esc(target)}</span>`}</td></tr>`;
      }).join('') + '</table>';
    },
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
            <span class="meta">body reference: <code>${ref}</code></span>
            <button class="btn small danger write-only" data-remove-file="${esc(a.path)}" data-name="${esc(a.name)}">外す</button>
          </div>
        </div>`;
      }).join('');
      return `
        ${cards || '<div class="empty">ファイルはありません。</div>'}
        <div class="toolbar write-only" style="margin-top:1rem">
          <input type="file" id="att-file" accept="image/png,image/jpeg,image/webp,application/pdf,text/plain,.txt,.csv,.json" multiple hidden>
          <button class="btn small" id="att-choose">ファイルを選ぶ…</button>
          <span class="provenance" id="att-chosen" style="margin:0"></span>
          <button class="btn small primary" id="att-upload">ファイルを追加</button>
        </div>
        <p class="provenance write-only">Any file, max 5 MiB each. Reference a file from the
        body (<code>![alt](${esc(lastSeg)}/name.png)</code> or <code>[name](${esc(lastSeg)}/name.txt)</code>) so it is findable and survives OKF export.</p>`;
    },
    linked: () => '<div class="empty">…</div>',
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
        toast(files.length > 1 ? `Added ${files.length} files.` : 'Added.');
        viewDetail(entry.id);
      } catch (e) { toast('ファイルの追加に失敗しました: ' + e.message); }
    });
    body.querySelectorAll('[data-remove-file]').forEach(b => b.addEventListener('click', async () => {
      const path = b.dataset.removeFile;
      if (!confirm(`Remove ${b.dataset.name}? 変更はリビジョンとして残ります。`)) return;
      try {
        await api(attPath(path), { method: 'DELETE' });
        toast('Removed.');
        viewDetail(entry.id);
      } catch (e) { toast('Remove failed: ' + e.message); }
    }));
  }

  const body = $('#tab-body');
  // Lazily loaded tabs: fetch once on first open, then re-render from the
  // cached template. A failed fetch shows in place and retries next open.
  const lazy = {
    usage: async () => {
      const u = await api('/api/v1/usage/' + idPath(entry.id));
      return () => `
        <div class="stat-tiles">
          <div class="tile"><div class="num">${u.search_hits ?? 0}</div><div class="lbl">search hits</div></div>
          <div class="tile"><div class="num">${u.fetches ?? 0}</div><div class="lbl">fetches</div></div>
          <div class="tile"><div class="num">${u.worked ?? 0}</div><div class="lbl">worked</div></div>
          <div class="tile"><div class="num">${u.failed ?? 0}</div><div class="lbl">failed</div></div>
        </div>
        <p class="provenance">${u.last_used_at ? 'last used ' + esc(fmtDate(u.last_used_at)) : 'never used'}</p>`;
    },
    linked: async () => {
      const { hits: entries = [] } = await api('/api/v1/search?links_to=' + encodeURIComponent(entry.id) + '&limit=100');
      return () => (entries && entries.length)
        ? `<p class="provenance" style="margin:0 0 .8rem">ここを指しているリンクを持つ concept — この concept 自身のリンクはリンクのタブにある。</p>`
          + entries.map(hitCard).join('')
        : '<div class="empty">この concept を指しているものは、まだありません。</div>';
    },
    history: async () => {
      // ?history is the object's own ledger, at the object's own address
      // (design doc 0046 §3.5); the log.md beside it is the directory's.
      const { revisions } = await api('/api/v1/bundle/' + idPath(entry.id) + '.md?history&limit=200');
      const rows = (revisions || []).map(r => `
        <tr>
          <td class="k mono">#${r.rev}</td>
          <td>
            <div><strong>${esc(r.change)}</strong> · ${esc(actorStr(r.changed_by))} · ${esc(fmtDate(r.changed_at))}</div>
            <details><summary style="cursor:pointer;color:var(--muted);font-size:.84rem">document</summary>
              <pre>${esc(r.document || '')}</pre></details>
          </td>
        </tr>`).join('');
      return () => `
        <p class="provenance" style="margin:0 0 .8rem">Every change, newest first — who made it and the concept as of that change.</p>
        <table class="kv">${rows}</table>`;
    },
  };
  async function showTab(name) {
    document.querySelectorAll('#tabs button').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
    body.innerHTML = tabs[name]();
    if (name === 'files') wireFiles();
    if (lazy[name]) {
      try {
        tabs[name] = await lazy[name]();
        delete lazy[name];
        if (document.querySelector('#tabs button.active')?.dataset.tab === name) body.innerHTML = tabs[name]();
      } catch (e) {
        if (document.querySelector('#tabs button.active')?.dataset.tab === name) {
          body.innerHTML = `<div class="error-banner">Could not load ${esc(name)}: ${esc(e.message)}</div>`;
        }
      }
    }
  }
  document.querySelectorAll('#tabs button').forEach(b => b.addEventListener('click', () => showTab(b.dataset.tab)));
  showTab('overview');

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
        toast(`Marked ${status}.`);
        refreshTree(); // the tree shows status badges
        viewDetail(entry.id);
        return;
      }
    } catch (e) { toast('Failed: ' + e.message); }
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
      toast(isVerified(entry) ? 'Re-verified.' : 'Verified.');
      viewDetail(entry.id);
    } catch (e) { toast('Failed: ' + e.message); }
  });
  $('#act-reject')?.addEventListener('click', async () => {
    closeMenu();
    const note = prompt('なぜ受け入れないのか?(裁定として残るので、エージェントは同じ提案を繰り返さなくなる)', '');
    if (note === null) return;
    try {
      await rejectEntry(entry.id, note);
      toast('Rejected.');
      refreshTree(); // the tree hides rejected entries
      viewDetail(entry.id);
    } catch (e) { toast('Failed: ' + e.message); }
  });
  $('#act-withdraw')?.addEventListener('click', async () => {
    closeMenu();
    try {
      await liftRejection(entry.id);
      toast('Rejection withdrawn.');
      refreshTree();
      viewDetail(entry.id);
    } catch (e) { toast('Failed: ' + e.message); }
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
    if (!confirm(`Soft-delete ${entry.id}? リビジョンの履歴は残ります。`)) return;
    try {
      await api(conceptURL(entry.id), { method: 'DELETE' });
      toast('Deleted.');
      refreshTree();
      location.hash = '#/';
    } catch (e) { toast('Failed: ' + e.message); }
  });
}
