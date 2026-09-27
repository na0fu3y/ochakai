// The reason a reject asks for, asked on the page. It was window.prompt(),
// which an embedded browser refuses outright ("prompt() is not
// supported") and Chrome stops showing once somebody ticks "prevent this
// page from creating additional dialogs" — either way the ruling could
// not be made — and which gave a ruling's reason one line to be written in.

import { $ } from './dom.js';

// The ask still waiting for an answer, if any. A new ask settles it
// first, so no earlier ask's listeners are still attached when a reason
// is submitted: one reason is one ruling.
let pending = null;

// askRejectNote resolves to the reason, trimmed, or null when the person
// backed out (キャンセル, Esc, or a click on the backdrop).
export function askRejectNote(id) {
  pending?.(null);
  const d = $('#reject-dialog');
  const note = $('#reject-note');
  $('#reject-what').textContent = id;
  note.value = '';
  d.showModal();
  note.focus();
  return new Promise(resolve => {
    const done = value => {
      pending = null;
      d.removeEventListener('cancel', onEsc);
      $('#reject-form').removeEventListener('submit', onSubmit);
      $('#reject-cancel').removeEventListener('click', onCancel);
      note.removeEventListener('keydown', onKey);
      d.removeEventListener('click', onBackdrop);
      if (d.open) d.close();
      resolve(value);
    };
    const onSubmit = e => {
      e.preventDefault();
      const v = note.value.trim();
      if (!v) { note.focus(); return; }
      done(v);
    };
    const onCancel = () => done(null);
    // Esc: cancel fires as the key is handled, where close is queued and
    // can arrive after the next ask has begun.
    const onEsc = e => { e.preventDefault(); done(null); };
    // ⌘/Ctrl+Enter sends from inside the textarea, where Enter is a newline.
    const onKey = e => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) $('#reject-form').requestSubmit();
    };
    // A click on the backdrop is a click on the <dialog> itself (palette.js).
    const onBackdrop = e => { if (e.target === d) done(null); };
    pending = done;
    d.addEventListener('cancel', onEsc);
    $('#reject-form').addEventListener('submit', onSubmit);
    $('#reject-cancel').addEventListener('click', onCancel);
    note.addEventListener('keydown', onKey);
    d.addEventListener('click', onBackdrop);
  });
}
