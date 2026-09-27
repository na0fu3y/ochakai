// The questions the page asks before it acts: a yes/no before an act
// that is hard to take back, and the reason a reject records. They were
// window.confirm() and window.prompt(). An embedded browser refuses
// prompt() outright ("prompt() is not supported") and answers confirm()
// with false without showing anything — so a delete, a move or leaving
// the editor did nothing and said nothing, and a Chrome reader who ticked
// "prevent this page from creating additional dialogs" got the same.
// One <dialog> in the page (index.html) asks all of them.

import { $ } from './dom.js';

// The ask still waiting for an answer, if any. A new ask settles it
// first, so no earlier ask's listeners are still attached when this one
// is answered: one answer is one act.
let pending = null;

// ask shows the dialog and resolves to the note (trimmed) when note is
// set, true when it is not, or null when the person backed out
// (キャンセル, Esc, or a click on the backdrop).
function ask({ title, message, ok, note = false, placeholder = '' }) {
  pending?.(null);
  const d = $('#ask-dialog');
  const form = $('#ask-form');
  const text = $('#ask-note');
  const cancel = $('#ask-cancel');
  $('#ask-title').textContent = title;
  $('#ask-message').textContent = message;
  $('#ask-ok').textContent = ok;
  text.hidden = !note;
  text.value = '';
  text.placeholder = placeholder;
  d.showModal();
  (note ? text : $('#ask-ok')).focus();
  return new Promise(resolve => {
    const done = value => {
      pending = null;
      d.removeEventListener('cancel', onEsc);
      form.removeEventListener('submit', onSubmit);
      cancel.removeEventListener('click', onCancel);
      text.removeEventListener('keydown', onKey);
      d.removeEventListener('click', onBackdrop);
      if (d.open) d.close();
      resolve(value);
    };
    const onSubmit = e => {
      e.preventDefault();
      if (!note) { done(true); return; }
      const v = text.value.trim();
      if (!v) { text.focus(); return; }
      done(v);
    };
    const onCancel = () => done(null);
    // Esc: cancel fires as the key is handled, where close is queued and
    // can arrive after the next ask has begun.
    const onEsc = e => { e.preventDefault(); done(null); };
    // ⌘/Ctrl+Enter sends from inside the textarea, where Enter is a newline.
    const onKey = e => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit();
    };
    // A click on the backdrop is a click on the <dialog> itself (palette.js).
    const onBackdrop = e => { if (e.target === d) done(null); };
    pending = done;
    d.addEventListener('cancel', onEsc);
    form.addEventListener('submit', onSubmit);
    cancel.addEventListener('click', onCancel);
    text.addEventListener('keydown', onKey);
    d.addEventListener('click', onBackdrop);
  });
}

// askConfirm resolves to true when the person chose ok, false otherwise.
export async function askConfirm(title, message, ok) {
  return (await ask({ title, message, ok })) === true;
}

// askRejectNote resolves to the reason a reject records, trimmed, or null.
export function askRejectNote(id) {
  return ask({
    title: '却下の理由',
    message: `${id} は削除され、この理由が裁定として残ります(履歴は残ります)。`,
    ok: '却下する',
    note: true,
    placeholder: '何が正しくないか、どこを読めば分かるか',
  });
}
