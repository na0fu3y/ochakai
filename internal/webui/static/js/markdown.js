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
  let out = '', inCode = false, para = [], quote = [];
  // Open lists, outermost first, each with the indentation that opened
  // it. A list is a stack rather than a flag because a runbook writes
  // steps under steps, and a renderer that flattened them changed what
  // the writer said.
  const lists = [];
  // Footnote definitions, keyed by the label that refers to them, found
  // in a pass before any line is drawn — a reference may come before its
  // definition, and in practice always does.
  //
  // This notation is not decoration here. `sources[].id` exists so that a
  // footnote in the body can cite one of the concept's sources
  // (docs/architecture.md; the id keys the document's own footnotes,
  // api/openapi.yaml on `cites`), and examples/demo's own metrics/revenue
  // is written that way — so the bundle every reader imports first was
  // being drawn with `[^rev-policy]` sitting in the prose as characters.
  const notes = new Map();
  const cited = [];
  for (const line of lines) {
    const def = line.match(/^\[\^([^\]\s]+)\]:\s*(.*)$/);
    // Keyed by the escaped label, because the reference is matched
    // against text that has already been escaped: a label carrying a
    // quote would otherwise be one string here and another there, and
    // the footnote would quietly stop resolving.
    if (def) notes.set(esc(def[1]), def[2]);
  }
  // The anchor is the writer's own label rather than the number beside
  // it: two renders on one page (a description and a body) would
  // otherwise both claim #fn-1, and a label is what the writer can see
  // in the URL.
  const anchor = id => id.replace(/[^A-Za-z0-9_-]+/g, '-');
  const footnote = (m, id) => {
    if (!notes.has(id)) return m; // undefined: the writer's text, unchanged
    if (!cited.includes(id)) cited.push(id);
    return `<sup class="fnref"><a id="fnref-${anchor(id)}" href="#fn-${anchor(id)}">` +
      `${cited.indexOf(id) + 1}</a></sup>`;
  };
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
  // The footnote pass runs before the link pass: `[^x]` carries no
  // parentheses, so the link notation never matches it, but reading the
  // two in the other order would leave the choice to an accident.
  const inline = s => outsideCode(esc(s).replace(/`([^`]+)`/g, '<code>$1</code>'), t => t
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|\W)\*([^*\n]+)\*(?=\W|$)/g, '$1<em>$2</em>')
    .replace(/\[\^([^\]\s]+)\]/g, footnote)
    .replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, img)
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, link)
    );
  const flushPara = () => { if (para.length) { out += '<p>' + inline(para.join(' ')) + '</p>'; para = []; } };
  const flushQuote = () => {
    if (quote.length) { out += '<blockquote><p>' + inline(quote.join(' ')) + '</p></blockquote>'; quote = []; }
  };
  // Every open <li> is still open, so closing a level closes both.
  const flushList = () => { while (lists.length) out += `</li></${lists.pop().tag}>`; };
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
      flushPara(); flushQuote(); flushList();
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
    // A definition line was read before the loop started and is not
    // drawn where it stands: it belongs at the foot, with the others.
    if (/^\[\^([^\]\s]+)\]:/.test(line)) { flushPara(); flushQuote(); flushList(); continue; }
    // Six levels, because a document that reaches h4 is a document
    // somebody wrote that way, and drawing it as a paragraph loses the
    // structure rather than simplifying it.
    const h = line.match(/^(#{1,6})\s+(.*)/);
    if (h) {
      flushPara(); flushQuote(); flushList();
      out += `<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`;
      continue;
    }
    const q = line.match(/^>\s?(.*)/);
    if (q) {
      flushPara(); flushList();
      if (q[1].trim()) quote.push(q[1].trim());
      else flushQuote();
      continue;
    }
    flushQuote();
    const li = line.match(/^([ \t]*)([-*]|\d+\.)\s+(.*)/);
    if (li) {
      flushPara();
      const indent = li[1].replace(/\t/g, '  ').length;
      const want = /^\d/.test(li[2]) ? 'ol' : 'ul';
      // Deeper levels close first, each taking its open <li> with it.
      while (lists.length && lists[lists.length - 1].indent > indent) {
        out += `</li></${lists.pop().tag}>`;
      }
      const top = lists[lists.length - 1];
      if (!top || indent > top.indent) {
        // A nested list opens inside the item above it, which is still
        // open — that is what makes the markup a tree rather than two
        // lists in a row.
        out += `<${want}>`;
        lists.push({ tag: want, indent });
      } else {
        out += '</li>';
        if (top.tag !== want) {
          out += `</${top.tag}><${want}>`;
          lists[lists.length - 1] = { tag: want, indent };
        }
      }
      out += `<li>${inline(li[3])}`;
      continue;
    }
    if (!line.trim()) { flushPara(); flushList(); continue; }
    para.push(line.trim());
  }
  if (inCode) out += '</code></pre>';
  flushPara(); flushQuote(); flushList();
  // The foot, in the order the body referred to them. A definition
  // nothing referred to is left out: it names no claim, and printing it
  // would put a number in the margin that the prose never uses.
  if (cited.length) {
    out += '<section class="footnotes"><ol>' + cited.map(id =>
      `<li id="fn-${anchor(id)}">${inline(notes.get(id))} ` +
      `<a class="fnback" href="#fnref-${anchor(id)}" title="本文に戻る">↩</a></li>`).join('') +
      '</ol></section>';
  }
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
