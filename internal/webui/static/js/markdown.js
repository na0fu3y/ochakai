// The page's markdown renderer: the subset a concept body uses, drawn
// without a dependency. What it does not draw, it leaves as text.

import { esc } from './escape.js';

// resolveFile maps a reference from the body to a file URL (the
// detail view resolves entry-relative references against the entry's
// files); references it cannot resolve stay literal text rather than
// rendering broken images and dead links. resolveEntry does the same for
// links to other entries — the edges the server derives from this very
// text (design doc 0024) — returning a route or null.
export function md(src, resolveFile, resolveEntry) {
  const lines = String(src ?? '').split('\n');
  let out = '', inCode = false, inList = null, para = [];
  const img = (m, alt, ref) => {
    const url = /^https?:/.test(ref) ? ref : resolveFile && resolveFile(ref);
    return url ? `<img src="${url}" alt="${alt}" loading="lazy">` : m;
  };
  // An http(s) target leaves the app; anything else is a bundle reference,
  // and becomes a link when it names an entry or a file attached to this
  // one. Since design doc 0013 a bundle file may be any type, not only an
  // image, and a link is the only way to reference a non-image one — the
  // files tab tells authors to write exactly this — so the same
  // resolver serves both notations.
  const link = (m, text, ref) => {
    if (/^https?:/.test(ref)) return `<a href="${ref}" target="_blank" rel="noopener">${text}</a>`;
    const route = resolveEntry && resolveEntry(ref);
    if (route) return `<a href="${route}">${text}</a>`;
    const file = resolveFile && resolveFile(ref);
    return file ? `<a href="${file}" target="_blank" rel="noopener">${text}</a>` : m;
  };
  // Everything after the code-span pass is applied outside <code> only:
  // markup shown as an example is text, not markup. That is also the rule
  // the server's link extraction follows (design doc 0024), so what reads
  // as a link here is exactly what became an edge there.
  const outsideCode = (html, fn) =>
    html.split(/(<code>[\s\S]*?<\/code>)/).map((part, i) => i % 2 ? part : fn(part)).join('');
  const inline = s => outsideCode(esc(s).replace(/`([^`]+)`/g, '<code>$1</code>'), t => t
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|\W)\*([^*\n]+)\*(?=\W|$)/g, '$1<em>$2</em>')
    .replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, img)
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, link)
    );
  const flushPara = () => { if (para.length) { out += '<p>' + inline(para.join(' ')) + '</p>'; para = []; } };
  const flushList = () => { if (inList) { out += `</${inList}>`; inList = null; } };
  // GFM table: a header row followed by a `|---|---|` delimiter row (with
  // optional `:` alignment markers), then body rows until a blank line.
  // Requiring a delimiter with at least two columns keeps a lone `---`
  // (used as prose, not markup, elsewhere in this parser) from matching.
  const isTableSep = line => /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line);
  const splitRow = line => {
    const cells = [];
    let cur = '', esc = false;
    for (const ch of line.trim()) {
      if (esc) { cur += ch; esc = false; continue; }
      if (ch === '\\') { esc = true; continue; }
      if (ch === '|') { cells.push(cur); cur = ''; continue; }
      cur += ch;
    }
    cells.push(cur);
    if (cells.length > 1 && !cells[0].trim()) cells.shift();
    if (cells.length > 1 && !cells[cells.length - 1].trim()) cells.pop();
    return cells.map(c => c.trim());
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^```/.test(line)) {
      flushPara(); flushList();
      out += inCode ? '</code></pre>' : '<pre><code>';
      inCode = !inCode;
      continue;
    }
    if (inCode) { out += esc(line) + '\n'; continue; }
    if (line.includes('|') && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      flushPara(); flushList();
      const aligns = splitRow(lines[i + 1]).map(a => {
        const left = a.startsWith(':'), right = a.endsWith(':');
        return left && right ? 'center' : right ? 'right' : left ? 'left' : '';
      });
      const row = (cells, tag) => '<tr>' + cells.map((c, ci) =>
        `<${tag}${aligns[ci] ? ` style="text-align:${aligns[ci]}"` : ''}>${inline(c)}</${tag}>`).join('') + '</tr>';
      out += '<table><thead>' + row(splitRow(line), 'th') + '</thead><tbody>';
      i += 2;
      for (; i < lines.length && lines[i].includes('|') && lines[i].trim(); i++) out += row(splitRow(lines[i]), 'td');
      out += '</tbody></table>';
      i--;
      continue;
    }
    const h = line.match(/^(#{1,3})\s+(.*)/);
    if (h) { flushPara(); flushList(); out += `<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`; continue; }
    const li = line.match(/^\s*([-*]|\d+\.)\s+(.*)/);
    if (li) {
      flushPara();
      const want = /^\d/.test(li[1]) ? 'ol' : 'ul';
      if (inList !== want) { flushList(); out += `<${want}>`; inList = want; }
      out += '<li>' + inline(li[2]) + '</li>';
      continue;
    }
    if (!line.trim()) { flushPara(); flushList(); continue; }
    para.push(line.trim());
  }
  if (inCode) out += '</code></pre>';
  flushPara(); flushList();
  return out;
}

// A description is prose written inside a markdown document, so it is
// rendered the way the body is. It is stored with its line structure
// intact — OKF writes a multi-line one as a `description: |-` block —
// and putting that text in an element as-is loses exactly that, because
// HTML collapses the newlines: a description that opens with a heading
// came back as one run-on line.
//
// No resolvers: a reference from a description is not a link the server
// derives an edge from (design doc 0024 reads the body), so rendering
// one as a link here would draw an edge that does not exist. An http(s)
// target is a link, as it is anywhere else.
export function descHTML(text, cls) {
  return `<div class="desc md${cls ? ' ' + cls : ''}">${md(text)}</div>`;
}
