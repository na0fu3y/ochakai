// Writing a concept: the frontmatter in fields, the document itself in a
// textarea, and one document under both.

import { api, toast } from '../api.js';
import { templateDocument, TYPE_REQUIRED } from '../documents.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { conceptURL, idPath } from '../format.js';
import {
  FIELDS, fieldsMarkup, listItemMarkup, otherKeys, rowMarkup, valueOf,
} from '../frontmatter.js';
import { refreshQueues } from '../queues.js';
import { route } from '../router.js';
import { refreshTree } from '../tree.js';

// prefix (create only) seeds the ID field — the per-directory ＋
// in the tree routes here as #/new/<prefix>/.
//
// The two panes are one document (design doc 0130). The fields are drawn
// from the server's read of the text below them, and every field edit
// goes back through the same face and lands in that text — so what is
// saved is what the textarea holds, exactly as before, and the form is a
// way of writing it rather than a second thing to keep in sync.
//
// Design doc 0130 §3.1 records why the fields were taken away, and
// §3.2 the condition for having them back: not a YAML parser in the page, whose failure mode is
// dropping a producer's key without saying so, but a face on the server.
// This is that face used. What it does not touch stays untouched — key
// order, comments, quoting, and every key the form has no editor for,
// which the form names rather than hides.
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
            ? 'ナレッジの置き場所です(移動はナレッジのページから行います)。'
            : 'ディレクトリを「/」で区切ったフルパスです。最後の区切りがナレッジの名前になります(例: 売上、monthly-revenue)。一緒に読まれるべきものは、一緒に置いてください。'}</div>
        </div>`;

  view.innerHTML = `
    <div class="section-title">${editing ? `${esc(entryID)} を編集` : 'ナレッジの新規作成'}</div>
    <form class="form wide" id="editor">
      ${idField}
      <div class="editor-panes">
        <div class="pane">
          <div class="pane-head">frontmatter</div>
          <div id="fm-error"></div>
          <div id="fm-fields"></div>
          <div id="fm-other"></div>
        </div>
        <div class="pane">
          <div class="pane-head">ドキュメント</div>
          <textarea id="e-doc" rows="30" class="mono" spellcheck="false" required>${esc(doc)}</textarea>
          <div class="hint"><code>---</code> の行にはさまれた OKF frontmatter、そのあとに markdown を書きます。左の欄はこの本文を読んで描かれ、左で直すとここが書き換わります。どちらで書いても、保存されるのはこのドキュメントです。他のナレッジへは、そのパスへの markdown リンク(<code>[revenue](/metrics/revenue.md)</code>)で結ぶと、両方向のリンクになります。ochakai が定義していないキーは書いたまま保たれます。サーバーが持つキー(<code>generated</code>・<code>verified</code>・<code>created_by</code>)は、あなたが決めるものではないため、書いてあっても無視されます。</div>
        </div>
      </div>
      <div id="editor-error"></div>
      <div class="toolbar" style="justify-content:flex-end">
        <a class="btn" href="${editing ? '#/k/' + idPath(entryID) : '#/search'}">キャンセル</a>
        <button class="btn primary write-only" type="submit">${editing ? '保存' : '作成'}</button>
      </div>
    </form>`;

  const docEl = $('#e-doc');
  const fields = $('#fm-fields');
  let dirty = false;
  // What the textarea held when the form was last drawn from it. A hand
  // edit is anything that moves it away from this, and it is what the
  // form has to be redrawn from — a field showing a value the text no
  // longer carries is the lie this pairing could tell.
  let drawn = null;
  // The document as the type dropdown seeded it, and untouched. While it
  // is still that, changing the type reseeds the whole template; once
  // somebody has written in it, changing the type edits the one key
  // (documents.js has carried this rule since the dropdown was alone).
  let seeded = editing ? null : docEl.value;

  // frontmatter posts the document to the structured face and hands back
  // what it answered, or null when it refused — which happens while
  // somebody is halfway through typing a frontmatter block, so it is
  // reported in the panel rather than as a failure.
  async function frontmatter(body) {
    try {
      const out = await api('/api/v1/frontmatter', { method: 'POST', body });
      $('#fm-error').innerHTML = '';
      return out;
    } catch (e) {
      $('#fm-error').innerHTML = `<div class="error-banner" role="alert">このドキュメントの frontmatter を欄に出せません: ${esc(e.message)}</div>`;
      fields.innerHTML = '';
      $('#fm-other').innerHTML = '';
      drawn = null;
      return null;
    }
  }

  function draw(out) {
    docEl.value = out.document;
    drawn = out.document;
    fields.innerHTML = fieldsMarkup(out.values);
    drawOther(out.keys);
  }

  // The keys the form has no editor for. Named rather than hidden: what
  // makes "the form is the document" true is that a producer's own key
  // survives it (SPEC §4.1), and a writer has no way to know that unless
  // the page says so.
  function drawOther(keys) {
    const rest = otherKeys(keys);
    $('#fm-other').innerHTML = rest.length
      ? `<div class="hint">この欄に無いキーはそのまま残っています: ${rest.map(k => `<code>${esc(k)}</code>`).join('、')}。右のドキュメントで直せます。</div>`
      : '';
  }

  // One round trip at a time, in the order they were asked for. Every
  // write sends the document as it stands, so two of them in flight
  // would both send the text from before the first — and the second
  // answer, arriving last, would put back a document without the first
  // edit in it. Tabbing quickly between two fields is enough.
  let queue = Promise.resolve();
  const inOrder = fn => (queue = queue.then(fn, fn));

  // push writes one key back into the document. undefined removes it:
  // ochakai does not write a key its writer left out (design doc 0046
  // §3.9), so a cleared field takes the key with it rather than storing
  // an empty one.
  async function push(sets, unsets) {
    if (!(sets && Object.keys(sets).length) && !(unsets && unsets.length)) return;
    return inOrder(async () => {
      const body = { document: docEl.value };
      if (sets && Object.keys(sets).length) body.set = sets;
      if (unsets && unsets.length) body.unset = unsets;
      await pushed(body);
    });
  }

  async function pushed(body) {
    const out = await frontmatter(body);
    if (!out) return;
    // The textarea is refreshed and the fields are not: the person is
    // still in them, and redrawing under a cursor moving from one field
    // to the next would take the focus with it. The keys can still have
    // changed — a cleared key is gone — so the note below them is.
    docEl.value = out.document;
    drawn = out.document;
    dirty = true;
    drawOther(out.keys);
  }


  // pushField collects one field out of the DOM and sends it.
  async function pushField(field) {
    const value = valueOf(field, rawOf(field));
    if (value === undefined) return push(null, [field.key]);
    return push({ [field.key]: value });
  }

  // rawOf reads one field back out of the form: every control carries
  // the key it belongs to, and a repeated one carries its column too, so
  // this walks markers rather than guessing at structure.
  function rawOf(field) {
    const all = sel => Array.from(fields.querySelectorAll(sel));
    const key = `[data-fm-key="${field.key}"]`;
    if (field.kind === 'list') return all(`${key}:not([data-fm-path])`).map(valueIn);
    if (field.kind === 'object') {
      const out = {};
      for (const col of field.columns) {
        const sel = `${key}[data-fm-path="${col.name}"]`;
        out[col.name] = col.kind === 'list' ? all(sel).map(valueIn) : valueIn(fields.querySelector(sel));
      }
      return out;
    }
    if (field.kind === 'rows') {
      return all(`[data-fm-rows="${field.key}"] > .row-card`).map(card => {
        const out = {};
        for (const col of field.columns) out[col.name] = valueIn(card.querySelector(`[data-fm-path="${col.name}"]`));
        return out;
      });
    }
    return valueIn(fields.querySelector(key));
  }

  const valueIn = el => (el ? (el.type === 'checkbox' ? el.checked : el.value) : '');
  const fieldOf = key => FIELDS.find(f => f.key === key);

  // A change, not an input: a field is sent when its writer has finished
  // with it, so one round trip per value rather than one per keystroke.
  fields.addEventListener('change', async ev => {
    const el = ev.target.closest('[data-fm-key]');
    if (!el) return;
    const field = fieldOf(el.getAttribute('data-fm-key'));
    if (!field) return;
    // The type is the one field that can reseed the whole document: an
    // untouched template is the template's to replace, and a document
    // somebody has written in is theirs (documents.js).
    if (field.key === 'type' && seeded !== null && docEl.value === seeded) {
      await inOrder(async () => {
        seeded = templateDocument(el.value, '');
        const out = await frontmatter({ document: seeded });
        if (out) draw(out);
        dirty = true;
      });
      return;
    }
    const sets = {};
    const value = valueOf(field, rawOf(field));
    if (value !== undefined) sets[field.key] = value;
    // A type change carries the key its type is refused without: an
    // Attested Computation with no runtime is rejected by the server,
    // and reporting success for a document that cannot be stored is
    // worse than doing nothing (documents.js says the same).
    if (field.key === 'type') {
      for (const line of (TYPE_REQUIRED[el.value] || '').split('\n')) {
        const i = line.indexOf(':');
        if (i < 0) continue;
        const other = fieldOf(line.slice(0, i));
        if (other && valueOf(other, rawOf(other)) === undefined) sets[line.slice(0, i)] = line.slice(i + 2);
      }
    }
    await push(sets, value === undefined ? [field.key] : null);
    // Only the seeding branch redraws, so a key the type change added
    // has to reach its own field by hand.
    for (const k of Object.keys(sets)) {
      if (k === field.key) continue;
      const input = fields.querySelector(`[data-fm-key="${k}"]:not([data-fm-path])`);
      if (input && !input.value) input.value = sets[k];
    }
  });

  // Adding and removing a repeated row edits the markup rather than
  // redrawing the form, for the reason push does not redraw it: the
  // other fields hold what somebody typed and has not sent yet.
  fields.addEventListener('click', async ev => {
    const add = ev.target.closest('.fm-add');
    const del = ev.target.closest('.fm-del');
    if (!add && !del) return;
    ev.preventDefault();
    if (del) {
      const row = del.closest('.row-card, .fm-item');
      const holder = row.parentElement;
      row.remove();
      renumber(holder);
      await pushField(fieldOf(holder.closest('[data-fm-field]').getAttribute('data-fm-field')));
      return;
    }
    const field = fieldOf(add.getAttribute('data-fm-add'));
    const path = add.getAttribute('data-fm-add-path') || '';
    const holder = path
      ? fields.querySelector(`[data-fm-list="${field.key}"][data-fm-list-path="${path}"]`)
      : fields.querySelector(`[data-fm-rows="${field.key}"], [data-fm-list="${field.key}"]:not([data-fm-list-path])`);
    const i = holder.children.length;
    holder.insertAdjacentHTML('beforeend',
      field.kind === 'rows' ? rowMarkup(field, i, {}) : listItemMarkup(field.key, path, i, '', field));
    const fresh = holder.lastElementChild.querySelector('input, textarea, select');
    if (fresh) fresh.focus();
    // Nothing is sent yet: an empty row is not a value, and it becomes
    // one when somebody types in it.
  });

  function renumber(holder) {
    Array.from(holder.children).forEach((el, i) => {
      el.setAttribute('data-fm-i', i);
      const n = el.querySelector('.row-head .n');
      if (n) n.textContent = String(i + 1);
    });
  }

  // The textarea is the other half of the pair. A hand edit is read back
  // when it settles rather than on every keystroke — a frontmatter block
  // halfway through being typed is not one to redraw a form from.
  docEl.addEventListener('change', () => inOrder(async () => {
    if (docEl.value === drawn) return;
    const out = await frontmatter({ document: docEl.value });
    if (out) draw(out);
  }));

  const first = await frontmatter({ document: docEl.value });
  if (first) draw(first);

  $('#editor').addEventListener('input', () => { dirty = true; });
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
    const document_ = docEl.value;
    // Say what is wrong here rather than waiting for the 400. The server
    // stays the only judge of a document; this just saves a round trip on
    // the mistake a text editor makes most (design doc 0130 §3).
    if (!/^---\r?\n[\s\S]*?\r?\n---/.test(document_.trim())) {
      errBox.innerHTML = '<div class="error-banner" role="alert">ドキュメントには YAML frontmatter が必要です: <code>---</code> の行、キー、そして閉じる <code>---</code> の行。</div>';
      return;
    }
    try {
      const saved = await api(conceptURL(entryId), {
        method: 'PUT', doc: document_, onlyIfAbsent: !editing, ifMatch: version,
      });
      dirty = false;
      // The server says which of the three the write was (design doc
      // 0097), so the toast can stop guessing: "保存しました。" after a
      // write that stored nothing is the one message a curator cannot
      // check.
      const said = saved && saved.plan === 'unchanged' ? '変更はありません。' : editing ? '保存しました。' : '作成しました。';
      // And what it read differently than this box wrote it (design doc
      // 0113). The curator is the one person who can go fix the line,
      // and until the notes were in the body this screen was the surface
      // that never saw them — the header was there, and nothing read it.
      const notes = (saved && saved.notes) || [];
      toast(notes.length ? said + ' ただし: ' + notes.join(' / ') : said, notes.length ? 9000 : undefined);
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
        ? `(<a href="#/k/${idPath(entryId)}">既にある ${esc(entryId)} を開く</a>か、別の ID を選んでください)`
        : '';
      // The precondition failed: somebody else saved while this form was
      // open. Say that rather than showing the server's sentence about
      // ETags, and do not offer to retry — the answer is to read what
      // they wrote, because the text in this box is now the older of two
      // edits and only the person holding it knows which parts survive.
      if (editing && e.code === 'precondition_failed') {
        errBox.innerHTML = `<div class="error-banner" role="alert">このナレッジは、この画面を開いてから他の人が保存しました。上書きを避けるため保存していません。書いた内容は、この欄に残っています。<a href="#/k/${idPath(entryId)}" target="_blank" rel="noopener">新しい版を別タブで開き</a>、残すべきところを移してから、もう一度この画面を開いてください。</div>`;
        return;
      }
      errBox.innerHTML = `<div class="error-banner" role="alert">${editing ? '保存' : '作成'}に失敗しました: ${esc(e.message)}${dupLink}</div>`;
    }
  });
}
