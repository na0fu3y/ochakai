// The outline of a rendered body: ids on the headings the renderer drew,
// and the table of contents built from them. Documentation services show
// long pages this way — the shape first, the prose under it — and a
// knowledge base whose bodies are runbooks and metric definitions reads
// the same way.
//
// It reads the renderer's own output rather than the markdown, because
// the renderer already decided what is a heading and what is a `#` in a
// code fence; deciding twice is how the two drift. The shape it matches
// is exactly what md() emits — `<hN>` with no attributes — so nothing a
// writer typed can match it: a `<h2>` in their prose arrives escaped.

import { esc } from './escape.js';

// The entities esc() writes, undone for the anchor only: a heading
// "A & B" should anchor as A-B, not A-amp-B. The visible text keeps the
// escaped form, because it is already HTML.
const unescape = s => s
  .replace(/&lt;/g, '<').replace(/&gt;/g, '>')
  .replace(/&quot;/g, '"').replace(/&#39;/g, "'")
  .replace(/&amp;/g, '&');

// An anchor a reader can see in the URL: the heading's own words, with
// whitespace as hyphens and markup punctuation dropped. Japanese stays
// itself — an id may hold any character but spaces, and 「検証の順番」
// is a better anchor than a transliteration nobody typed.
export function headingAnchor(text) {
  return unescape(text)
    .trim()
    .replace(/\s+/g, '-')
    .replace(/[<>&"'#%?]/g, '')
    .toLowerCase();
}

// headingAnchors gives every heading in a rendered body an id and
// returns the outline beside the marked-up html. Ids are deduplicated
// the way filenames are (-2, -3, …), so two sections with one name stay
// two anchors; an empty heading gets no id and stays out of the outline.
export function headingAnchors(html) {
  const taken = new Map();
  const headings = [];
  const out = String(html ?? '').replace(/<h([1-6])>([\s\S]*?)<\/h\1>/g, (m, level, inner) => {
    const text = inner.replace(/<[^>]*>/g, '').trim();
    const base = headingAnchor(text);
    if (!base) return m;
    const n = (taken.get(base) || 0) + 1;
    taken.set(base, n);
    const id = n === 1 ? base : `${base}-${n}`;
    headings.push({ level: Number(level), text, id });
    return `<h${level} id="${esc(id)}">${inner}</h${level}>`;
  });
  return { html: out, headings };
}

// How many headings make an outline worth its height: below this a
// reader sees the whole shape by scrolling once, and the box would only
// push the prose down.
export const TOC_MIN = 3;

// tocHTML draws the outline as a disclosure above the body — h1 to h3,
// because past that a document is showing structure to the section it is
// in, not to the page. Nothing to draw returns nothing: the box is for
// documents long enough to need a map.
export function tocHTML(headings) {
  const items = (headings || []).filter(h => h.level <= 3);
  if (items.length < TOC_MIN) return '';
  const top = Math.min(...items.map(h => h.level));
  return '<details class="toc" open><summary>目次</summary><ol>'
    + items.map(h => `<li class="lv${h.level - top}"><a href="#${esc(h.id)}">${h.text}</a></li>`).join('')
    + '</ol></details>';
}
