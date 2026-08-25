// The quick-open palette: Ctrl+K (⌘K) from anywhere, type, Enter, and
// the entry is open. Documentation services all keep this door — a
// reader who knows what they want should not have to walk to the search
// page to name it — and it is a read, so it belongs to every deployment,
// read-only ones included.
//
// It is a client of the same /api/v1/search the search view uses, with
// the same empty-query behavior: nothing typed lists by demand
// (sort=usage), which is this page's version of "recent pages" — the
// entries this base is actually asked for.

import { api } from './api.js';
import { $ } from './dom.js';
import { esc } from './escape.js';
import { displayTitle, entryHash } from './format.js';
import { icon } from './vocab.js';
import { debounce, explore, viewExplore } from './views/explore.js';

const dialog = () => $('#palette');

// The rows the palette holds: concept hits, plus one synthetic row that
// hands the query to the full search view — the palette jumps, the
// search view explains, and a query that needs filters or a snippet
// belongs there.
let rows = [];
let active = 0;

function render() {
  const list = $('#palette-list');
  const q = $('#palette-q').value.trim();
  const attrs = i => `class="palette-row${i === active ? ' active' : ''}"${i === active ? ' id="palette-active"' : ''} role="option" aria-selected="${i === active}" data-i="${i}"`;
  const items = rows.map((r, i) => r.search
    ? `<div ${attrs(i)}>
         <span class="type-ico" aria-hidden="true">🔍</span>
         <span class="palette-title">「${esc(q)}」を検索</span><span class="palette-id">結果の一覧と絞り込みを開きます</span>
       </div>`
    : `<div ${attrs(i)}>
         <span class="type-ico" aria-hidden="true">${icon(r.hit.type)}</span>
         <span class="palette-title">${esc(displayTitle(r.hit))}</span>
         <span class="palette-id mono">${esc(r.hit.id)}</span>
         <span class="badge ${esc(r.hit.status)}">${esc(r.hit.status)}</span>
       </div>`).join('');
  list.innerHTML = items || `<div class="empty">${q ? '一致するナレッジが見つかりません。' : 'まだナレッジがありません。'}</div>`;
  list.querySelectorAll('.palette-row').forEach(el => {
    el.addEventListener('click', () => go(Number(el.dataset.i)));
    // Hover moves the selection the way the arrows do, so the keyboard
    // and the pointer never disagree about which row Enter would open.
    el.addEventListener('mousemove', () => {
      if (active === Number(el.dataset.i)) return;
      active = Number(el.dataset.i);
      render();
    });
  });
  const marked = list.querySelector('.palette-row.active');
  if (marked) marked.scrollIntoView({ block: 'nearest' });
  $('#palette-q').setAttribute('aria-activedescendant', marked ? 'palette-active' : '');
}

async function load() {
  const q = $('#palette-q').value.trim();
  const my = load._seq = (load._seq || 0) + 1;
  try {
    const p = q ? 'q=' + encodeURIComponent(q) : 'sort=usage';
    const { hits = [] } = await api(`/api/v1/search?${p}&limit=8`);
    if (my !== load._seq || !dialog().open) return; // a newer keystroke answered
    rows = hits.map(hit => ({ hit }));
    if (q) rows.push({ search: true });
    active = 0;
    render();
  } catch (e) {
    if (my !== load._seq || !dialog().open) return;
    $('#palette-list').innerHTML = `<div class="error-banner" role="alert">検索に失敗しました: ${esc(e.message)}</div>`;
  }
}

function go(i) {
  const r = rows[i];
  if (!r) return;
  dialog().close();
  if (r.search) {
    // Hand the words to the search view the way its own box would. A
    // hash set to its current value fires no hashchange, so the view is
    // redrawn directly when it is already the route.
    explore.q = $('#palette-q').value.trim();
    if (location.hash === '#/search') viewExplore();
    else location.hash = '#/search';
    return;
  }
  location.hash = entryHash(r.hit);
}

export function openPalette() {
  const d = dialog();
  if (d.open) return;
  d.showModal();
  const input = $('#palette-q');
  input.value = '';
  input.focus();
  rows = [];
  active = 0;
  load();
}

export function initPalette() {
  const d = dialog();
  const input = $('#palette-q');
  // ⌘K where the keyboard has a ⌘; Ctrl+K everywhere else. The button's
  // hint follows the machine it is shown on.
  if (/Mac|iP(hone|ad|od)/.test(navigator.platform || '')) {
    const kbd = $('#palette-btn kbd');
    if (kbd) kbd.textContent = '⌘K';
  }
  window.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      d.open ? d.close() : openPalette();
    }
  });
  $('#palette-btn').addEventListener('click', openPalette);
  // A click on the backdrop is a click on the <dialog> itself — its box
  // only ever receives clicks on its children.
  d.addEventListener('click', e => { if (e.target === d) d.close(); });
  input.addEventListener('input', () => debounce(load, 150));
  input.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!rows.length) return;
      active = (active + (e.key === 'ArrowDown' ? 1 : rows.length - 1)) % rows.length;
      render();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      go(active);
    }
  });
}
