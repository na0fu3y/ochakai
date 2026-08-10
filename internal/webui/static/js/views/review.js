// The review queue: what agents drafted, what was reported wrong, what
// is past its expiry — and the loop's own numbers above them.

import { rejectEntry, verifyEntry } from '../actions.js';
import { api, toast } from '../api.js';
import { cardThumbs } from '../cards.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { actorStr, daysSince, displayTitle, entryHash, fmtAge } from '../format.js';
import { descHTML } from '../markdown.js';
import { queueStrip, refreshQueues } from '../queues.js';
import { refreshTree } from '../tree.js';
import { icon } from '../vocab.js';
import { explore } from './explore.js';

// A draft with zero search hits, older than this, is flagged stale — the
// inventory case (nobody's finding it; a candidate to drop). Sort=usage
// already sinks these to the bottom; the toggle isolates them.
export const STALE_DAYS = 14;
// loaded and cursor walk the draft queue a page at a time (design doc
// 0050 §2.1): the queue is a ledger, so it has an end rather than a cap.
export const review = { staleOnly: false, loaded: [], cursor: '' };

export function viewReview() {
  view.innerHTML = `
    <div class="section-title">draft のレビューキュー</div>
    <div class="read-only-note">この ochakai は read-only なので、レビューの操作は
    表示されません。キューそのものは本物です — 書き込めるデプロイのレビュアーが
    片付けていくのが、これと同じ列です。</div>
    <p style="color:var(--muted);max-width:48rem">エージェントが書き戻した draft を、求められている順に — 検索ヒット数と、
    その draft が実際に検索で浮いた回数で順位が付きます。正しいものは検証し、そうでないものは却下します
    (理由は status_note として残るので、エージェントは同じ提案を繰り返さなくなります)。一度も使われていない
    draft は下に沈みます。<em>stale</em> を切り替えると、その仕分けができます。</p>
    <p style="color:var(--muted);font-size:.9rem;max-width:48rem">検証済みのナレッジには、それ自身の二つのキューがある:
    <a href="#/search/reported-wrong">間違いと報告された</a>(失敗の報告に応えていない concept)と、
    <a href="#/search/verification-age">検証の古さ</a>(検証が古い順)です。検証すると、その concept は両方から消えます。
    A third, <a href="#/search/stale">stale</a>, lists concepts past the expiry their author declared — that one is cleared by
    editing the concept, not by verifying.</p>
    <div id="loop-stats"></div>
    <div class="toolbar">
      <label class="check"><input type="checkbox" id="r-stale" ${review.staleOnly ? 'checked' : ''}>
        stale only (0 hits, ≥ ${STALE_DAYS} days old)</label>
      <span class="grow"></span>
      <span id="queue-strip" style="display:flex;gap:.35rem">${queueStrip()}</span>
    </div>
    <div id="review-results"><div class="empty">…</div></div>`;
  $('#r-stale').addEventListener('change', () => { review.staleOnly = $('#r-stale').checked; runReview(); });
  refreshQueues();
  runReview();
  loadLoopStats();
}

// The loop as the instance sees it (design doc 0051), on the page where
// the person who runs it already is. Four numbers and the questions that
// came back empty — not a dashboard: what a curator can act on this
// morning, which is what the queues below are for.
//
// sparkline draws the review trend: eight weeks of verifications, oldest
// first (design doc 0095). Inline SVG rather than a chart library — this
// is a curation surface and not a BI tool (design doc 0067 §1), and one
// shape with no axes is the whole of what it has to say: is reviewing
// picking up, holding, or stopping.
//
// Absent when the server sent no trend, which is a base nobody has ruled
// on yet — eight zeroes would read as "review stopped".
function sparkline(weeks) {
  if (!weeks || !weeks.length) return '';
  const peak = Math.max(...weeks.map(w => w.verifications), 1);
  const w = 13, gap = 3, h = 26;
  const bars = weeks.map((week, i) => {
    // A week with nothing still gets a mark, or a gap in the row reads
    // as missing data rather than as a week nobody reviewed anything.
    const tall = Math.max(2, Math.round((week.verifications / peak) * h));
    return `<rect class="spark-bar${week.verifications ? '' : ' zero'}"
      x="${i * (w + gap)}" y="${h - tall}" width="${w}" height="${tall}"
      ><title>${esc(week.from)} から1週間: ${week.verifications} 件</title></rect>`;
  }).join('');
  const total = weeks.reduce((n, week) => n + week.verifications, 0);
  return `<figure class="spark">
    <svg viewBox="0 0 ${weeks.length * (w + gap) - gap} ${h}" width="${weeks.length * (w + gap) - gap}"
         height="${h}" role="img" aria-label="直近 ${weeks.length} 週の検証数、古い順: ${
      weeks.map(week => week.verifications).join('、')}">${bars}</svg>
    <figcaption>直近 ${weeks.length} 週の検証 — 計 ${total} 件。左が古い</figcaption>
  </figure>`;
}

export async function loadLoopStats() {
  const out = $('#loop-stats');
  if (!out) return;
  let s;
  try { s = await api('/api/v1/stats'); } catch (e) {
    if (out !== $('#loop-stats')) return;
    out.innerHTML = `<div class="error-banner" role="alert">loop の数字を読み込めませんでした: ${esc(e.message)}</div>`;
    return;
  }
  if (out !== $('#loop-stats')) return;
  const trust = s.concepts?.trust || {};
  const confirmed = (trust['human-reviewed'] || 0) + (trust['machine-confirmed'] || 0);
  const gaps = s.misses?.recording ? (s.misses.queries || []) : [];
  // Shown only when it happened. The tiles above are the always-there
  // numbers; this is a fault in the measurement behind them, and a
  // permanent "0 lost" line would be one more thing to read past.
  const dropped = (s.dropped?.events ?? 0) + (s.dropped?.misses ?? 0);
  // Same rule, same reason: a fault in what search can see, drawn only
  // where it exists. A permanent "0 truncated" is one more line to read
  // past, and this one is worth stopping at — nothing else on any
  // surface says a concept is half in the index (design doc 0089).
  const truncated = s.embedding?.enabled ? (s.embedding.truncated ?? 0) : 0;
  out.innerHTML = `
    <div class="stat-tiles" style="max-width:52rem;margin:.2rem 0 1rem">
      <div class="tile"><div class="num">${s.concepts?.total ?? 0}</div><div class="lbl">concepts</div></div>
      <div class="tile"><div class="num">${confirmed}</div><div class="lbl">confirmed by somebody</div></div>
      <div class="tile"><div class="num">${s.review?.verifications ?? 0}</div><div class="lbl">verified in ${s.window_days} days</div></div>
      <div class="tile"><div class="num">${s.misses?.recording ? (s.misses.count ?? 0) : '–'}</div>
        <div class="lbl">searches that found nothing</div></div>
      <div class="tile"><div class="num">${s.outcomes?.concepts_reported ?? 0}/${s.outcomes?.concepts_used ?? 0}</div>
        <div class="lbl">used concepts reported back on</div></div>
    </div>` + sparkline(s.review?.weekly) + (dropped ? `
    <p style="max-width:48rem;margin:0 0 1.2rem;color:var(--muted);font-size:.9rem">
      ${dropped} observation${dropped === 1 ? '' : 's'} were recorded and then lost in these
      ${s.window_days} days, so the numbers above undercount by at least that much. Usage is
      buffered in memory and written in batches; an instance that went away took what it was
      holding with it.</p>` : '') + (truncated ? `
    <p style="max-width:48rem;margin:0 0 1.2rem;color:var(--muted);font-size:.9rem">
      ${truncated} 件の concept が、埋め込みモデルの入力窓に収まらず<strong>前半だけ</strong>
      ベクトル検索に載っています(全 ${s.embedding.vectors} 件中)。後半に書かれていることでは
      見つかりません — 語句そのものが含まれていれば字句検索は当てます。長すぎる concept を
      分けるか、より広い窓のモデルに移すかのどちらかです。</p>` : '') + (gaps.length ? `
    <details style="max-width:48rem;margin:0 0 1.2rem">
      <summary style="cursor:pointer;color:var(--muted)">Asked for, not found — what to write next</summary>
      <p style="color:var(--muted);font-size:.9rem;margin:.5rem 0">直近 ${s.window_days} 日の検索
      that returned nothing, most-asked first. Not a queue anything empties: it stops appearing when the answer exists.</p>
      ${gaps.map(g => `<div style="display:flex;gap:.6rem;align-items:baseline;padding:.25rem 0">
        <span class="badge">${g.count}×</span>
        <a href="#/search" class="mono" data-gap="${esc(g.query)}">${esc(g.query)}</a></div>`).join('')}
    </details>` : '');
  // A gap is a question; clicking it asks it again, which is how a
  // curator sees what is there before writing what is not.
  out.querySelectorAll('[data-gap]').forEach(a => a.addEventListener('click', () => {
    explore.q = a.dataset.gap;
    explore.failedFeed = explore.ageFeed = explore.expiredFeed = false;
  }));
}

export function isStaleDraft(h) {
  const age = daysSince(h.created_at);
  return ((h.usage && h.usage.search_hits) || 0) === 0 && age !== null && age >= STALE_DAYS;
}

// append reads the next page of the queue and keeps the cards already on
// screen; every other call starts it over.
export async function runReview(append = false) {
  const out = $('#review-results');
  if (!out) return;
  const my = runReview._seq = (runReview._seq || 0) + 1;
  const limit = 100;
  let url = '/api/v1/search?sort=usage&status=draft&limit=' + limit;
  if (append && review.cursor) url += '&cursor=' + encodeURIComponent(review.cursor);
  else { review.loaded = []; review.cursor = ''; }
  try {
    const page = await api(url);
    if (my !== runReview._seq || out !== $('#review-results')) return; // stale response
    const hits = review.loaded = review.loaded.concat(page.hits || []);
    review.cursor = page.cursor || '';
    let list = hits;
    if (review.staleOnly) list = list.filter(isStaleDraft);
    if (!list.length) {
      out.innerHTML = `<div class="empty">${review.staleOnly ? '放置された draft はありません。' : 'レビュー待ちの draft はありません。'}</div>`;
      return;
    }
    out.innerHTML = list.map(reviewCard).join('')
      + (review.cursor
        ? `<div class="truncation-note">Showing ${list.length} drafts ·
             <a href="#" id="review-more">load more</a></div>`
        : '');
    $('#review-more')?.addEventListener('click', e => { e.preventDefault(); runReview(true); });
    wireReviewActions(out);
  } catch (e) {
    if (my !== runReview._seq || out !== $('#review-results')) return;
    out.innerHTML = `<div class="error-banner" role="alert">レビューキューを読み込めませんでした: ${esc(e.message)}</div>`;
  }
}

export function reviewCard(h) {
  const u = h.usage || {};
  const tags = (h.tags || []).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
  const who = actorStr(h.created_by);
  const age = daysSince(h.created_at);
  // The recent count leads, because it is what put this card where it
  // is: the feeds rank on the window and break ties on the lifetime
  // total (design doc 0090). Showing only the total made the order look
  // wrong — a concept read twenty times last year sitting under one read
  // three times last week.
  const r = u.recent || {};
  const usageBits = (u.search_hits || u.fetches)
    ? `🔍 ${u.search_hits || 0} hits · fetched ${u.fetches || 0} (${r.fetches || 0} in ${r.days || 0}d)`
    : 'no usage yet';
  const outcomeBits = (u.worked || u.failed)
    ? `${u.failed ? '⚠️ ' : ''}worked ${u.worked || 0} · failed ${u.failed || 0}`
    : '';
  const meta = [
    who ? esc(who) : '',
    age !== null ? 'created ' + esc(fmtAge(age)) : '',
    usageBits,
    outcomeBits,
  ].filter(Boolean).join(' · ');
  return `<article class="card" data-id="${esc(h.id)}" data-href="${entryHash(h)}">
    <div class="head">
      <span class="type-ico" title="${esc(h.type)}">${icon(h.type)}</span>
      <a class="title" href="${entryHash(h)}" title="ochakai://${esc(h.id)}">${esc(displayTitle(h))}</a>
      <span class="badge draft">draft</span>
      ${isStaleDraft(h) ? '<span class="badge" title="検索ヒット 0 で動きもない — 落とす候補">🕸️ stale</span>' : ''}
      ${tags}
      <span class="actions write-only" style="margin-left:auto;display:flex;gap:.45rem">
        <button class="btn small primary" data-act="verify">✓ 検証</button>
        <button class="btn small danger" data-act="reject">却下</button>
      </span>
    </div>
    <div class="meta">${meta}</div>
    ${h.description ? descHTML(h.description) : ''}
    ${cardThumbs(h)}
  </article>`;
}

export function wireReviewActions(container) {
  container.querySelectorAll('[data-act]').forEach(btn => btn.addEventListener('click', async () => {
    const card = btn.closest('[data-id]');
    const id = card.dataset.id;
    // Both are rulings against the entry as it stands, so neither needs
    // the entry's current content — there is no full-replacement PUT here
    // to clobber a body edited since the queue loaded.
    try {
      if (btn.dataset.act === 'verify') {
        await verifyEntry(id);
        toast('Verified.');
      } else {
        const note = prompt('なぜ受け入れないのか?(裁定として残るので、エージェントは同じ提案を繰り返さなくなる)', '');
        if (note === null) return;
        await rejectEntry(id, note);
        toast('Rejected.');
      }
      refreshTree(); // the tree shows status badges (and hides rejected)
      runReview();
    } catch (e) { toast('Failed: ' + e.message); }
  }));
}
