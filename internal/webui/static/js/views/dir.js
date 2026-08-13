// The home page and a directory's own page — the web rendering of the
// index.md the OKF export writes at every level (design doc 0046 §3.7).

import { api, downloadBundle } from '../api.js';
import { dirCard, fileCard, hitCard } from '../cards.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { crumbTrail, idPath } from '../format.js';
import { revealInTree } from '../tree.js';
import { explore } from './explore.js';

// loadDirIndex fetches one browse level and renders it into container
// (which the caller seeded with a placeholder). Concept cards reuse the
// search result card — browse concepts carry no attrs/files, so
// they render as title, status, and description.
export async function loadDirIndex(container, prefix, emptyText) {
  try {
    const res = await api(prefix ? `/api/v1/bundle/${idPath(prefix)}/index.md` : '/api/v1/bundle/index.md');
    if (!container.isConnected) return; // navigated away while loading
    const dirs = (res.dirs || []).map(d => dirCard(prefix, d)).join('');
    const concepts = (res.concepts || []).map(hitCard).join('');
    const files = (res.files || []).map(fileCard).join('');
    const note = res.truncated
      ? `<div class="truncation-note">この階層の先頭 1000 concept を表示 — これだけ広いディレクトリは
         サブディレクトリに分けたほうがよいでしょう。</div>` : '';
    container.innerHTML = (dirs + concepts + files + note) || `<div class="empty">${emptyText}</div>`;
  } catch (e) {
    if (!container.isConnected) return;
    container.innerHTML = `<div class="error-banner" role="alert">一覧を読み込めませんでした: ${esc(e.message)}</div>`;
  }
}

export function viewDir(rawPrefix) {
  const clean = rawPrefix.replace(/\/+$/, '');
  const prefix = clean ? clean + '/' : '';
  revealInTree(prefix); // expand the tree to (and including) this directory
  const newHref = '#/new/' + (clean ? idPath(clean) + '/' : '');
  const segs = clean ? clean.split('/') : [];
  view.innerHTML = `
    <nav class="crumbs mono">${crumbTrail(segs.slice(0, -1), segs.at(-1))}</nav>
    <div class="toolbar">
      <div class="section-title" style="margin:0"><span class="type-ico">📁</span> <span class="mono">/${esc(prefix)}</span></div>
      <span class="grow"></span>
      ${clean ? `<a class="btn small" href="#/search/in/${idPath(clean)}"
        title="${esc(prefix)} の下だけを検索">🔍 ここを検索</a>` : ''}
      <a class="btn small write-only" href="${newHref}" title="${prefix ? esc(prefix) + ' に' : 'ルートに'} concept を作る">＋ ここに concept を作る</a>
    </div>
    <div id="dir-index"><div class="empty">…</div></div>`;
  loadDirIndex($('#dir-index'), prefix, 'ここには何もありません — ディレクトリは、その中の concept があってはじめて存在します。');
}

export function viewHome() {
  view.innerHTML = `
    <div class="section-title" style="font-size:1.5rem">🍵 ochakai</div>
    <p style="color:var(--muted);max-width:42rem">ナレッジはフォルダのツリーです。concept の id が
    そのままパスになる(例: <code>queries/sales/monthly-revenue</code>)ので、サイドバーのツリーが
    入口になります。まとめて読むものは同じ場所に置き、ドキュメントを読むように辿ってください。
    どこを見ればよいか分からないときに、検索を使います。</p>
    <div class="searchbox" style="max-width:36rem">
      <input type="text" id="home-q" placeholder="メトリクス・検証済みクエリ・insight・用語・テーブルを検索…" autocomplete="off">
      <a class="btn" id="home-go" href="#/search">検索</a>
    </div>
    <ul class="home-links">
      <li><a href="#/review">レビューキュー</a> — エージェントが書いた draft を検証・却下する</li>
      <li><a href="#/search/reported-wrong">間違いと報告された</a> — 検証済みなのに間違いだったナレッジ</li>
      <!-- Gated twice on purpose: the link is the write affordance, and the
           item around it would otherwise leave a gap in the row. -->
      <li class="write-only"><a class="write-only" href="#/new">＋ concept を作る</a></li>
      <li><a href="#" id="home-export" title="ナレッジベースを OKF バンドル(tar.gz)として書き出す">OKF を書き出す</a></li>
    </ul>
    <div id="home-index" style="margin-top:1.4rem"><div class="empty">…</div></div>`;
  $('#home-q').addEventListener('keydown', e => {
    if (e.key === 'Enter') { explore.q = e.target.value; location.hash = '#/search'; }
  });
  $('#home-go').addEventListener('click', () => { explore.q = $('#home-q').value; });
  $('#home-export').addEventListener('click', downloadBundle);
  if (matchMedia('(pointer: fine)').matches) $('#home-q').focus({ preventScroll: true });
  loadDirIndex($('#home-index'), '', 'まだナレッジがありません — 最初の concept を作りましょう。');
}
