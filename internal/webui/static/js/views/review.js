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
import { knownDirs, refreshTree } from '../tree.js';
import { icon } from '../vocab.js';
import { debounce, explore } from './explore.js';

// A draft with zero search hits, older than this, is flagged stale — the
// inventory case (nobody's finding it; a candidate to drop). Sort=usage
// already sinks these to the bottom; the toggle isolates them.
export const STALE_DAYS = 14;
// The windows the flow numbers can be asked about — `days` on the stats
// call, whose default is the 30 this page opens with. 180 is the
// server's ceiling and not a round number chosen here: the raw events
// behind the outcome and miss counts are pruned after it, so a longer
// window would answer with less than it was asked for (design doc 0069
// §3), and asking for one is a 400.
export const STATS_WINDOWS = [7, 30, 90, 180];
// loaded and cursor walk the draft queue a page at a time (design doc
// 0050 §2.1): the queue is a ledger, so it has an end rather than a cap.
// days and prefix are what the loop's numbers above it are asked about;
// both are parameters the stats call already takes.
export const review = { staleOnly: false, loaded: [], cursor: '', days: 30, prefix: '' };

export function viewReview() {
  view.innerHTML = `
    <div class="section-title">draft のレビューキュー</div>
    <div class="read-only-note">この ochakai は read-only のため、レビューの操作は表示されません。キューそのものは本物です。書き込めるデプロイのレビュアーが解消していくのは、これと同じ列です。</div>
    <p style="color:var(--muted);max-width:48rem">エージェントが書き戻した draft を、求められている順に並べています。順位を決めるのは検索に出た回数ではなく、実際に読み込まれた回数です。直近の期間を先に見て、同じなら通算で比べます。正しいものは検証し、そうでないものは却下してください(理由が <code>status_note</code> として残るため、エージェントは同じ提案を繰り返さなくなります)。一度も読まれていない draft は下に並ぶので、<em>放置されたものだけ</em>に切り替えると仕分けができます。</p>
    <p style="color:var(--muted);font-size:.9rem;max-width:48rem">検証済みのナレッジには、それ自身のキューが二つあります。<a href="#/search/reported-wrong">再検証</a>(失敗の報告にまだ応えていないナレッジ)と、<a href="#/search/verification-age">検証の古さ</a>(検証の古いものから順)です。検証し直すと、前者からは消え、後者では最後尾に回ります。三つ目の<a href="#/search/stale">期限切れ</a>は、書き手が宣言した期限を過ぎたナレッジを並べます。こちらは検証では解消せず、ナレッジを編集して期限を宣言し直すと消えます。</p>
    <div class="toolbar" style="margin:.2rem 0 .5rem">
      <label class="check" style="white-space:nowrap">期間
        <select id="loop-days" aria-label="集計期間">${STATS_WINDOWS.map(d =>
          `<option value="${d}"${d === review.days ? ' selected' : ''}>直近 ${d} 日</option>`).join('')}</select></label>
      <label class="check" style="white-space:nowrap">範囲
        <input type="text" id="loop-prefix" list="loop-dirs" placeholder="全体" aria-label="集計範囲のパス"
               title="このパスの下にあるナレッジだけを数えます(例: teams/growth)"
               value="${esc(review.prefix)}" style="width:9rem"></label>
      <datalist id="loop-dirs"></datalist>
    </div>
    <div id="loop-stats"></div>
    <div class="toolbar">
      <label class="check"><input type="checkbox" id="r-stale" ${review.staleOnly ? 'checked' : ''}>
        放置されたものだけ(検索ヒット 0 件・作成から ${STALE_DAYS} 日以上)</label>
      <span class="grow"></span>
      <span id="queue-strip" style="display:flex;gap:.35rem">${queueStrip()}</span>
    </div>
    <div id="review-results"><div class="empty">…</div></div>`;
  $('#r-stale').addEventListener('change', () => { review.staleOnly = $('#r-stale').checked; runReview(); });
  // The directories the tree has loaded, offered as completions — the
  // same list the move dialog completes against, and the reason the
  // scope is typed as a path rather than picked from a fixed menu:
  // which prefixes mean a team is the base's own business.
  $('#loop-dirs').innerHTML = knownDirs().map(p => `<option value="${esc(p)}">`).join('');
  $('#loop-days').addEventListener('change', e => { review.days = Number(e.target.value); loadLoopStats(); });
  $('#loop-prefix').addEventListener('input', e => {
    review.prefix = e.target.value.replace(/^\/+|\/+$/g, '').trim();
    debounce(loadLoopStats, 250);
  });
  refreshQueues();
  runReview();
  loadLoopStats();
}

// The loop as the instance sees it (design doc 0069 §5), on the page
// where the person who runs it already is. A handful of numbers and the
// questions that came back empty — not a dashboard: what a curator can
// act on this morning, which is what the queues below are for.
//
// They come in two kinds and are read differently, so they are drawn as
// two bands rather than one row: a *state* is how the base stands right
// now, a *flow* is what happened inside the window (design doc 0069 §5
// says which is which, field by field). Split, the window control
// visibly moves the second band and leaves the first alone; mixed, the
// reader had to know the schema to tell which numbers just changed.
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
      ><title>${esc(week.from)} からの1週間: ${week.verifications} 件</title></rect>`;
  }).join('');
  const total = weeks.reduce((n, week) => n + week.verifications, 0);
  return `<figure class="spark">
    <svg viewBox="0 0 ${weeks.length * (w + gap) - gap} ${h}" width="${weeks.length * (w + gap) - gap}"
         height="${h}" role="img" aria-label="直近 ${weeks.length} 週の検証数、古い順: ${
      weeks.map(week => week.verifications).join('、')}">${bars}</svg>
    <figcaption>直近 ${weeks.length} 週の検証(計 ${total} 件)。左が古い順です</figcaption>
  </figure>`;
}

export async function loadLoopStats() {
  const out = $('#loop-stats');
  if (!out) return;
  // The window and the scope are both parameters the stats call already
  // takes, so asking a narrower question adds nothing to the wire. days
  // is sent only when it is not the server's own default, which keeps
  // the plain "how is it going" call the same one it always was.
  const p = new URLSearchParams();
  if (review.days !== 30) p.set('days', review.days);
  if (review.prefix) p.append('prefix', review.prefix);
  const query = p.toString();
  // Rapid changes to either control leave earlier answers in flight; the
  // last one asked for is the one drawn, whichever returns first.
  const my = loadLoopStats._seq = (loadLoopStats._seq || 0) + 1;
  let s;
  try { s = await api('/api/v1/stats' + (query ? '?' + query : '')); } catch (e) {
    if (my !== loadLoopStats._seq || out !== $('#loop-stats')) return;
    out.innerHTML = `<div class="error-banner" role="alert">ループの数値を読み込めませんでした: ${esc(e.message)}</div>`;
    return;
  }
  if (my !== loadLoopStats._seq || out !== $('#loop-stats')) return;
  const trust = s.concepts?.trust || {};
  const confirmed = (trust['human-reviewed'] || 0) + (trust['machine-confirmed'] || 0);
  const gaps = (s.misses?.recording && !s.misses?.withheld) ? (s.misses.queries || []) : [];
  // Shown only when it happened. The tiles above are the always-there
  // numbers; this is a fault in the measurement behind them, and a
  // permanent "0 lost" line would be one more thing to read past.
  const dropped = (s.dropped?.events ?? 0) + (s.dropped?.misses ?? 0);
  // Same rule, same reason: a fault in what search can see, drawn only
  // where it exists. A permanent "0 truncated" is one more line to read
  // past, and this one is worth stopping at — nothing else on any
  // surface says a concept is half in the index (design doc 0089).
  const truncated = s.embedding?.enabled ? (s.embedding.truncated ?? 0) : 0;
  // Every number keyed by a concept honors the scope — the tallies, the
  // queues, the verifications, the outcomes, the shape. `misses` does
  // not and cannot: a search that found nothing found it nowhere, so
  // there is no id to scope it by (design doc 0069 §5.1). Wherever one
  // is drawn beside a scoped number, it says which it is.
  //
  // Which subtrees were counted is the server's answer, not this page's
  // request: a caller whose grants cover part of the bundle gets their
  // own numbers whether or not they picked a prefix here (design doc
  // 0123). `scope` absent means the whole bundle.
  const scope = Array.isArray(s.scope) ? s.scope : null;
  const scoped = scope !== null;
  const withheld = !!s.misses?.withheld;
  const scopeText = scoped
    ? (scope.length ? scope.map(p => `<code>/${esc(p)}</code>`).join('、') : 'あなたに見えている範囲')
    : '';
  out.innerHTML = (scoped ? `
    <p style="max-width:48rem;margin:0 0 .8rem;color:var(--muted);font-size:.9rem">${scopeText} の下だけを数えています。<strong>該当なしの検索だけは絞れません。</strong>何も返さなかった検索にはナレッジの id が無いため、どこで尋ねられたかを言えないからです。${withheld ? 'そのため、範囲を持つ利用者には出していません — 全体の数は管理者に尋ねてください。' : 'その数と一覧はインスタンス全体のものです。'}下のキューの帯と draft の一覧も絞っていません。</p>` : '') + `
    <div class="tile-band">いまの姿</div>
    <div class="stat-tiles band" style="max-width:52rem;margin:0 0 1rem">
      <div class="tile"><div class="num">${s.concepts?.total ?? 0}</div><div class="lbl">ナレッジ</div></div>
      <div class="tile"><div class="num">${confirmed}</div><div class="lbl">確認済み</div></div>
    </div>
    <div class="tile-band">直近 ${s.window_days} 日に起きたこと</div>
    <div class="stat-tiles band" style="max-width:52rem;margin:0 0 1rem">
      <div class="tile"><div class="num">${s.concepts?.created ?? 0}</div><div class="lbl">増えたナレッジ</div></div>
      <div class="tile"><div class="num">${s.review?.verifications ?? 0}</div><div class="lbl">検証</div></div>
      <div class="tile"><div class="num">${s.outcomes?.worked ?? 0} / ${s.outcomes?.failed ?? 0}</div>
        <div class="lbl">成功 / 失敗の報告</div></div>
      <div class="tile"><div class="num">${s.outcomes?.concepts_reported ?? 0}/${s.outcomes?.concepts_used ?? 0}</div>
        <div class="lbl">結果報告 / 利用</div></div>
      <div class="tile"><div class="num">${(withheld || !s.misses?.recording) ? '–' : (s.misses.count ?? 0)}</div>
        <div class="lbl">該当なしの検索${withheld ? '(非公開)' : (scoped ? '(全体)' : '')}</div></div>
    </div>` + sparkline(s.review?.weekly) + (dropped ? `
    <p style="max-width:48rem;margin:0 0 1.2rem;color:var(--muted);font-size:.9rem">
      この ${s.window_days} 日で、記録されたあとに失われた観測が ${dropped} 件あります。上の数値は、少なくともその分だけ実際より少なく出ています。利用状況はメモリに溜めてまとめて書き出すため、停止したインスタンスは抱えていた分を持ち去ります。</p>` : '') + (truncated ? `
    <p style="max-width:48rem;margin:0 0 1.2rem;color:var(--muted);font-size:.9rem">
      ${truncated} 件のナレッジが、埋め込みモデルの入力窓に収まらず<strong>前半だけ</strong>ベクトル検索に載っています(全 ${s.embedding.vectors} 件中)。後半に書かれた内容では見つかりません。語句そのものが含まれていれば字句検索では見つかります。長すぎるナレッジを分けるか、より広い窓のモデルに移すかのどちらかが必要です。</p>` : '') + (gaps.length ? `
    <details style="max-width:48rem;margin:0 0 1.2rem">
      <summary style="cursor:pointer;color:var(--muted)">該当なしの検索(${scoped ? '全体・' : ''}次に書くもの)</summary>
      <p style="color:var(--muted);font-size:.9rem;margin:.5rem 0">直近 ${s.window_days} 日で何も返さなかった検索を、多く尋ねられた順に並べています。誰かが解消するキューではありません。答えになるナレッジが書かれれば、この一覧からは自然に消えます。${scoped ? 'この一覧だけは範囲の指定を受けません。何も返さなかった検索は、どこで尋ねられたかを言えないからです。' : ''}</p>
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
        ? `<div class="truncation-note">draft ${list.length} 件を表示 ·
             <a href="#" id="review-more">さらに読み込む</a></div>`
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
    ? `🔍 ヒット ${u.search_hits || 0} · 取得 ${u.fetches || 0}(直近 ${r.days || 0} 日で ${r.fetches || 0})`
    : 'まだ使われていません';
  const outcomeBits = (u.worked || u.failed)
    ? `${u.failed ? '⚠️ ' : ''}成功 ${u.worked || 0} · 失敗 ${u.failed || 0}`
    : '';
  const meta = [
    who ? esc(who) : '',
    age !== null ? '作成 ' + esc(fmtAge(age)) : '',
    usageBits,
    outcomeBits,
  ].filter(Boolean).join(' · ');
  return `<article class="card" data-id="${esc(h.id)}" data-href="${entryHash(h)}">
    <div class="head">
      <span class="type-ico" title="${esc(h.type)}">${icon(h.type)}</span>
      <a class="title" href="${entryHash(h)}" title="ochakai://${esc(h.id)}">${esc(displayTitle(h))}</a>
      <span class="badge draft">draft</span>
      ${isStaleDraft(h) ? '<span class="badge" title="検索ヒット 0 件で動きもありません(削除の候補)">🕸️ 放置</span>' : ''}
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
        toast('検証しました。');
      } else {
        const note = prompt('却下の理由をご記入ください。裁定として残るため、エージェントは同じ提案を繰り返さなくなります。', '');
        if (note === null) return;
        await rejectEntry(id, note);
        toast('却下しました。');
      }
      refreshTree(); // the tree shows status badges (and hides rejected)
      runReview();
    } catch (e) {
      toast((btn.dataset.act === 'verify' ? '検証' : '却下') + 'できませんでした: ' + e.message);
    }
  }));
}
