// The cards every listing is made of — a concept hit, a directory, a
// file. One implementation each, because search, browse, a directory
// index and the detail page all draw the same three things.

import { BASE } from './api.js';
import { esc } from './escape.js';
import { dirHash, displayTitle, entryHash, fmtSize, idPath } from './format.js';
import { descHTML } from './markdown.js';
import { icon } from './vocab.js';

// Image files as small previews on a result card. The metadata
// rides along on REST hits; the bytes are separate GETs the browser
// caches (ETag revalidation — repeat renders cost a 304, not a
// re-download).
export function cardThumbs(h) {
  const imgs = (h.files || []).filter(a => (a.media_type || '').startsWith('image/')).slice(0, 4);
  if (!imgs.length) return '';
  const src = a => BASE + '/api/v1/bundle/' + idPath(a.path);
  return `<div class="thumbs">${imgs.map(a =>
    `<img src="${src(a)}" alt="${esc(a.name)}" title="${esc(a.name)}" loading="lazy">`).join('')}</div>`;
}

// The card leads with what the entry says (title, status, description,
// SQL); the id, author, and dates live on the detail view — on a result
// list they were noise (the title link's URL still carries the id).
export function hitCard(h) {
  const tags = (h.tags || []).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
  const sql = h.attrs && h.attrs.sql ? `<pre>${esc(h.attrs.sql)}</pre>` : '';
  return `<article class="card" data-href="${entryHash(h)}">
    <div class="head">
      <span class="type-ico" title="${esc(h.type)}">${icon(h.type)}</span>
      <a class="title" href="${entryHash(h)}" title="ochakai://${esc(h.id)}">${esc(displayTitle(h))}</a>
      <span class="badge ${esc(h.status)}">${esc(h.status)}</span>
      ${tags}
    </div>
    ${h.description ? descHTML(h.description) : ''}
    ${h.snippet ? `<div class="desc snippet">${esc(h.snippet)}</div>` : ''}
    ${sql}
    ${cardThumbs(h)}
  </article>`;
}

// A directory rendered as a page: the web counterpart of the index.md
// the OKF export writes at every level (progressive disclosure) —
// subdirectories with their entry counts, then the entries at this
// level with title and description. The home page shows the root level
// below its intro, so "/" and "#/dir/<prefix>" read the same way.

export function dirCard(prefix, d) {
  const href = dirHash(prefix + d.name);
  const noun = d.count === 1 ? 'concept' : 'concepts';
  return `<article class="card" data-href="${href}">
    <div class="head">
      <span class="type-ico">📁</span>
      <a class="title mono" href="${href}">${esc(d.name)}/</a>
      <span class="count">${d.count} ${noun}</span>
    </div>
  </article>`;
}

// fileCard is one file object sitting in this directory — the third
// thing an index.md lists (design doc 0046 §3.7). It links to the
// object's own address, because that is where the bytes are; a file
// belongs to no entry unless one shows it.
export function fileCard(f) {
  const href = BASE + '/api/v1/bundle/' + idPath(f.path);
  return `<article class="card">
    <div class="head">
      <span class="type-ico">📄</span>
      <a class="title mono" href="${href}" target="_blank" rel="noopener">${esc(f.name)}</a>
      <span class="meta">${esc(f.media_type || '')} · ${fmtSize(f.size)}</span>
    </div>
  </article>`;
}
