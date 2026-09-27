// What the failed reports said, where the reader decides whether to
// trust a concept. The notes lived only on the 利用状況 tab, so a
// concept reported wrong since it was last verified still led with
// "✓ human-reviewed", and the re-verify feed listed it with nothing but
// its description — the reviewer it summoned had to open it and find the
// third tab to learn what was wrong.

import { esc } from './escape.js';
import { actorStr, fmtDate } from './format.js';

// failedSince is the failed reports with a note that came after the
// verification at `since` (every one of them when there is none),
// newest first as usage returns them. Only noted reports come back from
// the server (openapi: Usage.reports), so this can miss a failure
// reported with no note; the re-verify feed is what counts those.
export function failedSince(reports, since) {
  const after = since ? Date.parse(since) : NaN;
  return (reports || []).filter(r => r.outcome === 'failed' &&
    (Number.isNaN(after) || Date.parse(r.at) > after));
}

// failedNoteHTML is the newest of those reports as one line, with how
// many more there are; '' when there are none.
export function failedNoteHTML(failed, lead) {
  if (!failed.length) return '';
  const [r] = failed;
  const more = failed.length > 1 ? ` ほか ${failed.length - 1} 件` : '';
  return `<div class="status-note failed-note">${esc(lead)}「${esc(r.note)}」` +
    `<span class="provenance">— ${esc(actorStr(r.by))}、${esc(fmtDate(r.at))}${more}</span></div>`;
}
