// The writes that are not an edit: move, and the rulings. They live
// together because two views issue them — the detail page and the review
// queue — and a ruling implemented twice is one that gets fixed once.

import { READ_ONLY, api, toast } from './api.js';
import { withStatus } from './documents.js';
import { $ } from './dom.js';
import { conceptURL, idPath } from './format.js';
import { refreshQueues } from './queues.js';
import { refreshTree } from './tree.js';

// moveEntry renames an entry (POST /api/v1/move) — the server carries
// revisions, usage, and files along and rewrites inbound
// references, so nothing breaks. Shared by tree drag & drop and the
// detail view's Move action.
export async function moveEntry(from, to) {
  if (!to || to === from) return false;
  try {
    const moved = await api('/api/v1/move', { method: 'POST', body: { from, to } });
    toast(`${moved.id} へ移動しました。`);
    refreshTree();
    // If the moved entry is on screen, follow it to its new address.
    const m = location.hash.match(/^#\/(?:k|edit)\/(.+)$/);
    if (m && m[1].split('/').map(decodeURIComponent).join('/') === from) {
      location.hash = '#/k/' + idPath(moved.id);
    }
    return true;
  } catch (e) {
    toast('移動に失敗しました: ' + e.message);
    return false;
  }
}

// movePrefixEntry moves a whole directory (POST /api/v1/move with
// from_prefix/to_prefix). Everything addressed under it goes in one
// transaction — including the rejected concepts, the deleted ones and the
// loose files this page never lists — so it moves whole or not at all
// (design doc 0132). The count comes back because it is the part nobody
// on this side could have worked out.
export async function movePrefixEntry(from, to) {
  if (!to || to === from) return false;
  try {
    const { moved } = await api('/api/v1/move', { method: 'POST', body: { from_prefix: from, to_prefix: to } });
    toast(`${to}/ へ移動しました（${moved} 件）。`);
    refreshTree();
    // A concept under the moved directory may be on screen; its address
    // changed, so send the reader to the directory that now holds it
    // rather than leaving them on a 404.
    const m = location.hash.match(/^#\/(?:k|edit)\/(.+)$/);
    if (m && (m[1].split('/').map(decodeURIComponent).join('/') + '/').startsWith(from + '/')) {
      location.hash = '#/dir/' + to.split('/').map(encodeURIComponent).join('/');
    }
    return true;
  } catch (e) {
    toast('移動に失敗しました: ' + e.message);
    return false;
  }
}

// Tree interactions beyond navigation: the per-directory ＋ (which must
// not toggle its node — preventDefault stops both the summary toggle and
// native link navigation, so route via the hash), and drag & drop moves:
// drop an entry on a directory (or the tree background = the root) to
// move it there, keeping its last segment.
{
  const tree = $('#tree');
  // Links inside a directory row (the per-directory ＋ and the name's
  // index-page link) must not toggle the node — preventDefault stops
  // both the summary toggle and native link navigation, so route via
  // the hash. The directory view then reveals (and expands) its node.
  tree.addEventListener('click', e => {
    const a = e.target.closest('.node-new, a.dir-name');
    if (!a) return;
    e.preventDefault();
    location.hash = a.getAttribute('href');
  });
  let dragId = null;
  let dragPrefix = null;
  const clearDrop = () => {
    tree.classList.remove('drop-hover');
    tree.querySelectorAll('summary.drop-hover').forEach(s => s.classList.remove('drop-hover'));
  };
  tree.addEventListener('dragstart', e => {
    // No drag ever starts on a read-only deployment, so dragId stays null
    // and dragover/drop do nothing either: the move could only 403, and a
    // gesture that always fails is the same lie as a button that always
    // fails (design doc 0040 §2.3). Checked here rather than only on the
    // rendered draggable attribute so a row rendered before the first
    // response settled READ_ONLY cannot slip through.
    if (READ_ONLY) { e.preventDefault(); return; }
    const entry = e.target.closest('.tree-entry');
    if (entry) {
      dragId = entry.dataset.id;
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', dragId);
      return;
    }
    // A directory is dragged by its own summary row. data-prefix carries
    // a trailing slash; the wire wants a path, so it is trimmed at the
    // point the request is built rather than stored two ways.
    const dir = e.target.closest('details.node > summary');
    if (!dir) return;
    dragPrefix = dir.parentElement.dataset.prefix.replace(/\/$/, '');
    if (!dragPrefix) return; // the root is not a directory that moves
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', dragPrefix + '/');
  });
  tree.addEventListener('dragend', () => { dragId = dragPrefix = null; clearDrop(); });
  tree.addEventListener('dragover', e => {
    if (!dragId && !dragPrefix) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    clearDrop();
    const sum = e.target.closest('details.node > summary');
    if (sum) sum.classList.add('drop-hover');
    else tree.classList.add('drop-hover');
  });
  tree.addEventListener('dragleave', e => { if (e.target === tree) clearDrop(); });
  tree.addEventListener('drop', e => {
    if (!dragId && !dragPrefix) return;
    e.preventDefault();
    const sum = e.target.closest('details.node > summary');
    const prefix = sum ? sum.parentElement.dataset.prefix : '';
    const from = dragId || dragPrefix;
    const wasDir = !dragId;
    clearDrop();
    dragId = dragPrefix = null;
    const to = prefix + from.split('/').pop();
    if (to === from) return; // dropped on its own directory
    if (wasDir) {
      // Into itself at any depth: there would be no answer to where the
      // concepts end up (design doc 0132 §6). The server refuses it too;
      // refusing here keeps the gesture from looking like it worked.
      if ((to + '/').startsWith(from + '/')) return;
      if (confirm(`${from}/ → ${to}/ へ移動しますか？\n配下のすべて（却下されたもの・削除済みのもの・ファイルを含む）が` +
        `まとめて動きます。途中で止まることはありません。`)) {
        movePrefixEntry(from, to);
      }
      return;
    }
    if (confirm(`${from} → ${to} へ移動しますか？\nこのナレッジを指している参照は自動で書き換わり、履歴も付いていきます。`)) {
      moveEntry(from, to);
    }
  });
}

// Apply a status transition (full-replacement PUT), prompting for a
// status_note when the transition needs a recorded reason. Returns true on
// success, false if the user cancelled the note prompt. Throws on API
// failure so callers can toast it. Shared by the detail view and the
// review queue; the entry may be a full Knowledge or a SearchHit (which
// embeds one), since both carry the writable fields.
export async function applyStatus(id, status) {
  // Read the document, change the one line, write it back. The read is
  // what makes this safe: the page never holds the whole entry, only the
  // projection, so anything assembled from what it holds would be missing
  // the rest of the document.
  const v = await api(conceptURL(id));
  await api(conceptURL(id), {
    method: 'PUT', doc: withStatus(v.document || '', status),
  });
  return true;
}

// The two rulings. Neither edits the document: confirming an entry and
// turning it down are judgments about it, so they have their own
// endpoints and leave the lifecycle status alone (design doc 0043
// §§3.2-3.3). Verifying is an append, so the first confirmation and the
// tenth re-check are the same call.
export async function verifyEntry(id) {
  await api('/api/v1/review/' + idPath(id), { method: 'POST', body: { ruling: 'verified' } });
  refreshQueues();
  return true;
}
export async function rejectEntry(id, note) {
  await api('/api/v1/review/' + idPath(id), { method: 'POST', body: { ruling: 'rejected', note } });
  refreshQueues();
  return true;
}
export async function liftRejection(id) {
  await api('/api/v1/review/' + idPath(id), { method: 'POST', body: { ruling: 'withdrawn' } });
  refreshQueues();
  return true;
}
