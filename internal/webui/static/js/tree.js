// The sidebar: the id hierarchy as folders, which is the page's primary
// navigation (design docs 0014, 0016).

import { READ_ONLY, api } from './api.js';
import { descendSingleRoad } from './descend.js';
import { $ } from './dom.js';
import { esc } from './escape.js';
import { dirHash, displayTitle, idPath } from './format.js';
import { icon } from './vocab.js';

// The folder view of the ID hierarchy (design docs 0014, 0016) is the
// primary navigation: a persistent sidebar where the id's segments are
// directories, loaded one level per request; the root is the top-level
// segments. Expanded nodes survive navigation within the session and
// are restored (re-fetched) on a tree refresh.
export const browse = {
  open: new Set(), // keys: prefix
  // root is the level the tree starts at, which is not always the
  // bundle's root — see descendSingleRoad. Read by the home page so
  // that what it lists and what the sidebar shows are one answer.
  root: '',
  // ready resolves when root holds that answer. The home view renders
  // from the same boot as the tree and got there first, so reading root
  // without waiting listed the corridor the sidebar had just walked
  // past — the two panels disagreeing about where the base begins.
  ready: Promise.resolve(),
};

// knownDirs are the directory prefixes the tree has loaded, plus the
// root — the completions offered where a destination path is typed.
export function knownDirs() {
  const dirs = new Set(['']);
  document.querySelectorAll('#tree details.node').forEach(d => dirs.add(d.dataset.prefix));
  return [...dirs].sort();
}

// refreshTree (re)loads the root level; wireNodes then reloads every
// node restored as open. Called at boot, from the ↻ button, and after
// writes that change what the tree shows (create, delete, status).
export function refreshTree() {
  const done = loadTree();
  // Every refresh republishes the answer, because a write can turn a
  // corridor into a room (or back).
  browse.ready = done.catch(() => {});
  return done;
}

async function loadTree() {
  const tree = $('#tree');
  try {
    const walked = await descendSingleRoad(
      await api('/api/v1/bundle/index.md'), '',
      p => api(`/api/v1/bundle/${idPath(p.replace(/\/$/, ''))}/index.md`));
    browse.root = walked.prefix;
    const body = levelHTML(walked.level, walked.prefix);
    tree.innerHTML = (walkedFromHTML(walked.prefix) + body)
      || '<div class="empty">まだナレッジがありません。</div>';
    wireNodes(tree);
    // A deep link may have rendered before the tree existed (boot) or
    // before this refresh; re-reveal the current entry or directory in
    // the new tree (a trailing slash makes revealInTree expand the
    // directory itself, not just its ancestors).
    const m = location.hash.match(/^#\/(?:k|edit)\/(.+)$/);
    const dm = location.hash.match(/^#\/dir\/(.+)$/);
    if (m) revealInTree(m[1].split('/').map(decodeURIComponent).join('/'));
    else if (dm) revealInTree(dm[1].split('/').map(decodeURIComponent).join('/').replace(/\/*$/, '/'));
    else markTreeSelection();
  } catch (e) {
    tree.innerHTML = `<div class="error-banner" role="alert">ツリーを読み込めませんでした: ${esc(e.message)}</div>`;
  }
}
$('#tree-refresh').addEventListener('click', refreshTree);

// walkedFromHTML names the corridor the tree skipped, with the way back
// to the real root. The tree starting somewhere other than the bundle's
// root is a thing a reader has to be able to see, and undo: without this
// line the page would be claiming an address it does not have.
function walkedFromHTML(prefix) {
  if (!prefix) return '';
  return `<div class="walked-from"><a href="${dirHash('')}" title="バンドルの一番上を開きます">/</a>${
    esc(prefix.replace(/\/$/, ''))} から表示しています</div>`;
}

export function nodeHTML(prefix, label, count) {
  // The ＋ prefills the editor's ID with this directory — no retyping the
  // full path to create knowledge next to its neighbors.
  return `<details class="node" data-prefix="${esc(prefix)}" ${browse.open.has(prefix) ? 'open' : ''}>
    <summary>${label}<span class="count">${count}</span><a class="node-new write-only"
      href="#/new/${prefix.split('/').filter(Boolean).map(encodeURIComponent).join('/')}/"
      title="${esc(prefix)} にナレッジを作成します">＋</a></summary>
    <div class="children"><div class="empty">…</div></div>
  </details>`;
}

// levelHTML renders one browse response: subdirectories, then the
// concepts at this level — title-first with a short status hint; the id
// and full status ride in the tooltip. A directory's name is a link to
// its index page (the ▸ marker and the rest of the row still toggle
// expansion). Concepts are draggable: dropping one on a directory moves
// it there (references rewritten server-side) — except on a read-only
// deployment, where the move would only 403 (design doc 0040 §2.3). A
// level is rendered from a browse response, so READ_ONLY is known by
// the time this runs; the drag handlers check it again at drag time.
export function levelHTML(res, prefix) {
  const dirs = (res.dirs || []).map(dir => nodeHTML(prefix + dir.name + '/',
    `<span class="type-ico">📁</span><a class="dir-name mono" href="${dirHash(prefix + dir.name)}"
       title="${esc(prefix + dir.name)}/ の索引を開きます">${esc(dir.name)}/</a>`, dir.count)).join('');
  const concepts = (res.concepts || []).map(e => `
    <a class="tree-entry" data-id="${esc(e.id)}" href="#/k/${idPath(e.id)}" draggable="${READ_ONLY ? 'false' : 'true'}"
       title="${esc(e.id)} · ${esc(e.status)}">
      <span class="type-ico" title="${esc(e.type)}">${icon(e.type)}</span>
      <span class="entry-title">${esc(displayTitle(e))}</span>
      ${e.status === 'stable' ? '' : `<span class="status-dot ${esc(e.status)}" title="${esc(e.status)}"></span>`}
    </a>`).join('');
  return dirs + concepts;
}

// wireNodes attaches lazy loading to every not-yet-wired node under
// root, and immediately loads the ones restored as open.
export function wireNodes(root) {
  root.querySelectorAll('details.node:not([data-wired])').forEach(d => {
    d.dataset.wired = '1';
    const key = d.dataset.prefix;
    d.addEventListener('toggle', () => {
      d.open ? browse.open.add(key) : browse.open.delete(key);
      if (d.open) loadNode(d);
    });
    if (d.open) loadNode(d);
  });
}

// loadNode fetches a node's children once; the in-flight promise is
// kept on the element so revealInTree can await a load it didn't start.
// A failed load clears both, so reopening (or revealing) retries.
export function loadNode(d) {
  if (d._load) return d._load;
  d._load = (async () => {
    const box = d.querySelector(':scope > .children');
    const prefix = d.dataset.prefix;
    try {
      const res = await api(`/api/v1/bundle/${idPath(prefix)}/index.md`);
      const body = levelHTML(res, prefix);
      const note = res.truncated
        ? `<div class="truncation-note">この階層の先頭 1000 件を表示しています。これだけ広いディレクトリは、サブディレクトリに分けることをおすすめします。</div>` : '';
      box.innerHTML = (body + note) || '<div class="empty">空です。</div>';
      wireNodes(box);
      markTreeSelection();
    } catch (e) {
      d._load = null; // reopen retries
      box.innerHTML = `<div class="error-banner" role="alert">読み込みに失敗しました: ${esc(e.message)}</div>`;
    }
  })();
  return d._load;
}

// revealInTree expands the tree along an entry's ancestor directories
// (loading each level as needed) so the current entry is visible and
// highlighted — the tree always shows where in the hierarchy you are.
export async function revealInTree(id) {
  const segs = String(id).split('/');
  let prefix = '';
  for (let i = 0; i < segs.length - 1; i++) {
    prefix += segs[i] + '/';
    const p = prefix;
    const d = [...document.querySelectorAll('#tree details.node')].find(n => n.dataset.prefix === p);
    if (!d) return; // parent level not present (yet) — nothing to reveal
    if (!d.open) d.open = true; // its toggle listener records the state
    await loadNode(d);
  }
  markTreeSelection();
}

// markTreeSelection highlights the tree row for the route currently
// shown — an entry (detail or its editor) or a directory's index page —
// if that part of the tree is loaded.
export function markTreeSelection() {
  const m = location.hash.match(/^#\/(?:k|edit)\/(.+)$/);
  const id = m ? m[1].split('/').map(decodeURIComponent).join('/') : null;
  let active = null;
  document.querySelectorAll('#tree .tree-entry').forEach(a => {
    const on = a.dataset.id === id;
    a.classList.toggle('active', on);
    if (on) active = a;
  });
  const dm = location.hash.match(/^#\/dir\/(.+)$/);
  const dirPrefix = dm ? dm[1].split('/').map(decodeURIComponent).join('/').replace(/\/*$/, '/') : null;
  document.querySelectorAll('#tree details.node').forEach(d => {
    const on = d.dataset.prefix === dirPrefix;
    const sum = d.querySelector(':scope > summary');
    sum.classList.toggle('active', on);
    if (on) active = sum;
  });
  active?.scrollIntoView({ block: 'nearest' });
}

// An open ⋯ menu closes on any click outside it, and on Escape.
document.addEventListener('click', e => {
  document.querySelectorAll('details.menu[open]').forEach(m => { if (!m.contains(e.target)) m.open = false; });
});
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') document.querySelectorAll('details.menu[open]').forEach(m => { m.open = false; });
});
