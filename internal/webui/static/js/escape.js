// The one function that must never be forgotten, in a module of its
// own: everything the page interpolates goes through it, and keeping it
// clear of the document means the modules that only turn values into
// markup import nothing a browser has to provide — which is what lets a
// test hold them.

export function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
