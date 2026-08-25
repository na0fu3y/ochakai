// Marking the words a reader searched for, in the results they came
// back to. A hit list where nothing says why a card matched leaves the
// reader to re-run the query with their eyes.
//
// The matching half is a string function so it can be tested without a
// DOM, and the DOM half walks text nodes only — it runs after a card is
// rendered, so it cannot corrupt markup or turn prose into a tag.

// The words a query holds. Whitespace-separated, U+3000 included, since
// a Japanese query typed with an ideographic space is one a person meant
// as two words. Longest first, so an overlapping pair marks the longer
// run rather than stopping at the shorter one.
export function terms(q) {
  const seen = new Set();
  for (const w of String(q ?? '').split(/\s+/u)) {
    const t = w.trim();
    if (t) seen.add(t);
  }
  return [...seen].sort((a, b) => b.length - a.length);
}

// The text cut into runs, each saying whether it matched. Matching is a
// case-insensitive substring scan rather than a regular expression:
// a query is user input, and building a pattern out of it is where the
// escaping bugs live. Substring also means Japanese needs no word
// boundaries, which it does not have.
// Lowercased without changing where anything is. `toLowerCase` is not
// length-preserving — U+0130 (İ) lowercases to two code units — and the
// offsets found in the folded text are used to cut the original, so one
// such character shifts every mark after it by one. A character whose
// lowercase is not one unit keeps its own form here: it stops matching
// case-insensitively, which is a smaller loss than marking the wrong
// word (and than the empty <mark> the shift could leave behind).
function fold(s) {
  const lower = s.toLowerCase();
  if (lower.length === s.length) return lower;
  let out = '';
  for (const ch of s) {
    const c = ch.toLowerCase();
    out += c.length === ch.length ? c : ch;
  }
  return out;
}

export function segments(text, ts) {
  const s = String(text ?? '');
  if (!s || !ts || !ts.length) return s ? [{ text: s, hit: false }] : [];
  const hay = fold(s);
  // Every match from every term, then merged: two terms overlapping in
  // one place is one mark, not two nested ones.
  const spans = [];
  for (const t of ts) {
    const needle = fold(t);
    if (!needle) continue;
    for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, i + 1)) {
      spans.push([i, i + needle.length]);
    }
  }
  if (!spans.length) return [{ text: s, hit: false }];
  spans.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  const merged = [spans[0].slice()];
  for (const [from, to] of spans.slice(1)) {
    const last = merged[merged.length - 1];
    if (from <= last[1]) last[1] = Math.max(last[1], to);
    else merged.push([from, to]);
  }
  const out = [];
  let at = 0;
  for (const [from, to] of merged) {
    if (from > at) out.push({ text: s.slice(at, from), hit: false });
    out.push({ text: s.slice(from, to), hit: true });
    at = to;
  }
  if (at < s.length) out.push({ text: s.slice(at), hit: false });
  return out;
}

// highlightIn marks the terms inside one element, in place. It walks
// text nodes, so an attribute — a title, an href — is never touched, and
// a node already inside a <mark> is left alone rather than nested.
export function highlightIn(el, ts) {
  if (!el || !ts || !ts.length) return;
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  const todo = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (n.parentElement && n.parentElement.closest('mark')) continue;
    if (n.nodeValue && n.nodeValue.trim()) todo.push(n);
  }
  for (const node of todo) {
    const parts = segments(node.nodeValue, ts);
    if (!parts.some(p => p.hit)) continue;
    const frag = document.createDocumentFragment();
    for (const p of parts) {
      if (p.hit) {
        const mark = document.createElement('mark');
        mark.textContent = p.text;
        frag.appendChild(mark);
      } else {
        frag.appendChild(document.createTextNode(p.text));
      }
    }
    node.replaceWith(frag);
  }
}
