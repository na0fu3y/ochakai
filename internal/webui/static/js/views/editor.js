// Writing a concept: the document itself in a textarea, with the fields
// the page can fill in for you edited back into its frontmatter.

import { api, toast } from '../api.js';
import { templateDocument, withFrontmatterKey, withType } from '../documents.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { conceptURL, idPath } from '../format.js';
import { refreshQueues } from '../queues.js';
import { route } from '../router.js';
import { refreshTree } from '../tree.js';
import { KNOWN_TYPES } from '../vocab.js';

// prefix (create only) seeds the ID field — the per-directory ＋
// in the tree routes here as #/new/<prefix>/.
//
// Editing is editing the document (design doc 0044). The row editors for
// sources and parameters are gone: filling them meant parsing YAML in the
// browser, and a hand-written parser whose failure mode is silently
// dropping a producer key is the one thing 0043 §3.6 had just decided
// against. What comes back instead is a parse check before save and a
// template per type.
export async function viewEditor(id, prefix = '') {
  const editing = id !== null;
  let doc = templateDocument('Insight', '');
  let entryID = prefix;
  // The version this edit is against: the ETag of the read that filled
  // the textarea, echoed back when it is saved.
  //
  // Without it this form was a blind overwrite. Two curators on the same
  // concept, or one who left a tab open over lunch, and the later save
  // silently won — losing prose a person wrote, on the surface this
  // product says only a person can operate. The REST API has had the
  // precondition since design doc 0030 and the CLI sends it; this page
  // was the one client that did not.
  let version = '';
  if (editing) {
    try {
      const v = await api(conceptURL(id));
      doc = v.document || '';
      entryID = v.id;
      version = v.etag || '';
    } catch (e) {
      view.innerHTML = `<div class="error-banner" role="alert">${esc(id)} を読み込めませんでした: ${esc(e.message)}</div>`;
      return;
    }
  }

  // The id is one field, the way the address is one string (design doc
  // 0016) and the way the Move panel already takes it. Its last segment
  // is the entry's name unless a title overrides it (design doc 0022).
  const idField = `
        <div class="field">
          <label class="top" for="e-id">ID</label>
          <input type="text" id="e-id" class="mono" value="${esc(entryID)}"
                 placeholder="sales/monthly-revenue"
                 ${editing ? 'disabled' : 'required'}>
          <div class="hint">${editing
            ? 'concept の置き場所(移動は concept のページから)。'
            : 'ディレクトリを「/」で区切ったフルパス。最後の区切りが concept の名前(例: 売上、monthly-revenue)。一緒に読まれるべきものは一緒に置く。'}</div>
        </div>`;

  view.innerHTML = `
    <div class="section-title">${editing ? `${esc(entryID)} を編集` : '新しいナレッジの concept'}</div>
    <form class="form" id="editor">
      <div class="row">
        ${idField}
        ${editing ? '' : `
        <div class="field">
          <label class="top" for="e-type">型</label>
          <select id="e-type">${KNOWN_TYPES.map(t =>
            `<option value="${t}"${t === 'Insight' ? ' selected' : ''}>${t}</option>`).join('')}</select>
          <div class="hint">Sets <code>type:</code> in the document below, with any key that type is refused without.
          ドキュメントを編集するまでは全体が入れ直され、編集した後はその行だけが変わります。
          保存されるのはこのドキュメントです。</div>
        </div>`}
      </div>
      <div class="field">
        <label class="top" for="e-doc">ドキュメント</label>
        <textarea id="e-doc" rows="26" class="mono" spellcheck="false" required>${esc(doc)}</textarea>
        <div class="hint">OKF frontmatter between <code>---</code> lines, then markdown.
        他の concept へは、そのパスへの markdown リンクで結ぶ — <code>[revenue](/metrics/revenue.md)</code> —
        and it becomes a link both ways. ochakai が定義していないキーは書いたまま保たれます。
        サーバーが持つキー(<code>generated</code>・<code>verified</code>・<code>created_by</code>)は
        are not yours to set and are ignored if present.</div>
      </div>
      <div id="editor-error"></div>
      <div class="toolbar" style="justify-content:flex-end">
        <a class="btn" href="${editing ? '#/k/' + idPath(entryID) : '#/search'}">キャンセル</a>
        <button class="btn primary write-only" type="submit">${editing ? 'Save' : 'Create'}</button>
      </div>
    </form>`;

  let dirty = false;
  $('#editor').addEventListener('input', () => { dirty = true; });
  const typeSel = $('#e-type');
  if (typeSel) {
    // Two modes, and the question that picks between them is the document
    // itself — not the form's dirty flag, which a <select> raises with its
    // own `input` before this `change` ever runs, so gating on it made the
    // dropdown do nothing at all, ever.
    //
    // Untouched, the document is still the one this seeded, and reseeding
    // it whole costs nothing. Written in, it is the writer's, so the
    // dropdown edits the type line and leaves the rest — the same line
    // edit the detail page's status change makes.
    let seeded = $('#e-doc').value;
    typeSel.addEventListener('change', () => {
      const doc = $('#e-doc').value;
      if (doc === seeded) {
        seeded = templateDocument(typeSel.value, '');
        $('#e-doc').value = seeded;
        return;
      }
      $('#e-doc').value = withType(doc, typeSel.value);
    });
  }
  window.onbeforeunload = () => (dirty ? '' : undefined);

  $('#editor').addEventListener('submit', async ev => {
    ev.preventDefault();
    const errBox = $('#editor-error');
    errBox.innerHTML = '';
    const entryId = editing ? entryID : $('#e-id').value.trim().replace(/^\/+|\/+$/g, '');
    if (!entryId) {
      errBox.innerHTML = '<div class="error-banner" role="alert">ID は必須です。</div>';
      return;
    }
    const document_ = $('#e-doc').value;
    // Say what is wrong here rather than waiting for the 400. The server
    // stays the only judge of a document; this just saves a round trip on
    // the mistake a text editor makes most (design doc 0044 §2.3).
    if (!/^---\r?\n[\s\S]*?\r?\n---/.test(document_.trim())) {
      errBox.innerHTML = '<div class="error-banner" role="alert">ドキュメントには YAML frontmatter が要ります: <code>---</code> の行、キー、そして閉じる <code>---</code> の行。</div>';
      return;
    }
    try {
      const saved = await api(conceptURL(entryId), {
        method: 'PUT', doc: document_, onlyIfAbsent: !editing, ifMatch: version,
      });
      dirty = false;
      // The server says which of the three the write was (design doc
      // 0097), so the toast can stop guessing: "Saved." after a write
      // that stored nothing is the one message a curator cannot check.
      toast(saved && saved.plan === 'unchanged' ? 'No change.' : editing ? 'Saved.' : 'Created.');
      refreshTree();
      refreshQueues(); // an edit is what clears the past-expiry queue (design doc 0037 §2.2)
      location.hash = '#/k/' + idPath((saved && saved.id) || entryId);
      if (editing) route(); // hash may be unchanged; re-render
    } catch (e) {
      // The id is taken. This asked the sentence for as long as there
      // was nothing else to ask — and the sentence it matched is one the
      // server rewrote twice since, once to name what holds the id and
      // what to do about it. The condition arrives as a code now, on a
      // 412 rather than a 409 because the page writes with If-None-Match
      // and that is a precondition failing (design doc 0083).
      const dupLink = !editing && e.code === 'already_exists'
        ? ` — <a href="#/k/${idPath(entryId)}">view the existing ${esc(entryId)}</a> (edit it, or pick another ID)`
        : '';
      // The precondition failed: somebody else saved while this form was
      // open. Say that rather than showing the server's sentence about
      // ETags, and do not offer to retry — the answer is to read what
      // they wrote, because the text in this box is now the older of two
      // edits and only the person holding it knows which parts survive.
      if (editing && e.code === 'precondition_failed') {
        errBox.innerHTML = `<div class="error-banner" role="alert">この concept は、この画面を開いてから誰かが保存しました。
          上書きを避けるため保存していません — 書いたものはこの欄に残っています。
          <a href="#/k/${idPath(entryId)}" target="_blank" rel="noopener">新しい版を別タブで開いて</a>、
          残すべきところを移してから、もう一度この画面を開いてください。</div>`;
        return;
      }
      errBox.innerHTML = `<div class="error-banner" role="alert">${editing ? 'Save' : 'Create'} failed: ${esc(e.message)}${dupLink}</div>`;
    }
  });
}
