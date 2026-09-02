// Search: the filter bar, the query, and the walk through the results.

import { api } from '../api.js';
import { hitCard } from '../cards.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { highlightIn, terms } from '../highlight.js';
import { KNOWN_TYPES, STATUSES, icon } from '../vocab.js';

// Filter state survives navigation within the session.
// loaded and cursor are the listing walk: the hits already on screen and
// where the next page resumes, both reset by any change that restarts the
// listing (design doc 0050 §2.1).
export const explore = { q: '', types: new Set(), statuses: new Set(), tag: '', prefix: '', ageFeed: false, expiredFeed: false, editedFeed: false, source: '', failedFeed: false, loaded: [], cursor: '', verified: false };

// If the viewport grows past the breakpoint while the filter disclosure is
// collapsed, its summary disappears — reopen it so filters stay reachable.
matchMedia('(min-width: 761px)').addEventListener('change', e => {
  const d = document.getElementById('filter-details');
  if (d && e.matches) d.open = true;
});

export function viewExplore() {
  const typeChips = KNOWN_TYPES.map(t => `
    <label class="chip"><input type="checkbox" data-type="${t}" aria-label="型 ${t}" ${explore.types.has(t) ? 'checked' : ''}>
      <span class="type-ico">${icon(t)}</span>${t}</label>`).join('');
  const extraTypes = [...explore.types].filter(t => !KNOWN_TYPES.includes(t)).map(t => `
    <label class="chip"><input type="checkbox" data-type="${esc(t)}" aria-label="型 ${esc(t)}" checked>
      <span class="type-ico">${icon(t)}</span>${esc(t)}</label>`).join('');
  const statusChips = STATUSES.map(s => `
    <label class="chip"><input type="checkbox" data-status="${s}" aria-label="ステータス ${s}" ${explore.statuses.has(s) ? 'checked' : ''}>${s}</label>`).join('')
    // The verification ledger, asked independently of the lifecycle value
    // (design doc 0043 §3.2). There is no rejected chip: a rejection is a
    // deletion (design doc 0135), so what a curator turned down is not in
    // any listing to filter for.
    + `
    <label class="chip" title="誰かが確認したナレッジだけに絞ります(machine-confirmed / human-reviewed)"><input type="checkbox" id="f-verified"
      aria-label="確認済みのみ" ${explore.verified ? 'checked' : ''}>✓ 確認済み</label>
`;
  // Filters collapse into a disclosure on narrow screens; keep it open on
  // wide ones (the summary is hidden there, so it could never be reopened).
  const wide = matchMedia('(min-width: 761px)').matches;

  view.innerHTML = `
  <section>
    <div class="searchbox">
      ${explore.ageFeed
        ? `<div class="feed-banner"><strong>検証の古さ</strong>のフィードです。最後に検証してから時間が経ったナレッジが上に来るので、古くなった検証済みクエリを見つけ直すのに使います。検索に戻るには「検証の古さ」のチェックを外してください。</div>`
        : explore.failedFeed
        ? `<div class="feed-banner"><strong>再検証のフィード</strong>です。失敗の報告(<code>report_outcome failed</code>)にまだ応えていないナレッジを、報告の多い順に並べています。中身を確かめて検証し直すと、このフィードから外れます。検索に戻るには「再検証」のチェックを外してください。</div>`
        : explore.source
        ? `<div class="feed-banner"><code>${esc(explore.source)}</code> を引用しているナレッジです。この資料から派生したものを、すべて表示しています。<a href="#/search">検索に戻る</a>。</div>`
        : explore.expiredFeed
        ? `<div class="feed-banner"><strong>期限切れのフィード</strong>です。書き手が宣言した <code>stale_after</code> を過ぎたナレッジを、超過の大きい順に並べています。<strong>このキューは検証しても解消しません。</strong>期限はサーバーが測った値ではなく書き手の宣言なので、内容を確かめたうえでナレッジを編集し、新しい期限を宣言する(または外す)必要があります。検索に戻るには「期限切れ」のチェックを外してください。</div>`
        : explore.editedFeed
        ? `<div class="feed-banner"><strong>編集後未検証のフィード</strong>です。検証は<strong>いまの内容</strong>に立ちます — 検証のあとに編集や移動が入ったナレッジは unverified に戻り、先頭からここに並びます(検証時刻の古い順)。内容を確かめて検証し直すと外れます。一度も検証されていないナレッジは検証時刻が空のまま末尾に続きます。検索に戻るには「編集後未検証」のチェックを外してください。</div>`
        : `<input type="text" id="q" placeholder="メトリクス・検証済みクエリ・知見・用語・テーブルを検索…"
                  value="${esc(explore.q)}" autocomplete="off">`}
      <a class="btn write-only" href="#/new" title="ナレッジを新規作成します">＋ ナレッジを作成</a>
    </div>
    <details id="filter-details" class="filterbar" ${wide ? 'open' : ''}>
      <summary>絞り込み</summary>
      <div class="fb-row">
        ${typeChips}${extraTypes}
        <input type="text" id="f-type-extra" placeholder="その他の型…" aria-label="その他の型" style="width:6.5rem">
        <span class="fb-sep"></span>
        ${statusChips}
        <span class="fb-sep"></span>
        <input type="text" id="f-tag" placeholder="タグ" aria-label="タグ" value="${esc(explore.tag)}" style="width:6rem">
        <input type="text" id="f-prefix" placeholder="パス" aria-label="パスで絞る"
               title="このパスの下にあるナレッジだけに絞ります(例: teams/growth)"
               value="${esc(explore.prefix)}" style="width:8rem">
        <span class="fb-sep"></span>
        <label class="chip" title="最後の検証が古い順に並べます"><input
          type="checkbox" id="f-age" aria-label="検証の古さのフィード" ${explore.ageFeed ? 'checked' : ''}>検証の古さ</label>
        <label class="chip" title="失敗の報告にまだ応えていないナレッジを並べます"><input
          type="checkbox" id="f-failed" aria-label="再検証のフィード" ${explore.failedFeed ? 'checked' : ''}>再検証</label>
        <label class="chip" title="stale_after を過ぎたナレッジを並べます(編集すると解消します)"><input
          type="checkbox" id="f-expired" aria-label="期限切れのフィード" ${explore.expiredFeed ? 'checked' : ''}>期限切れ</label>
        <label class="chip" title="検証のあとに編集・移動されたナレッジを並べます(検証し直すと解消します)"><input
          type="checkbox" id="f-edited" aria-label="編集後未検証のフィード" ${explore.editedFeed ? 'checked' : ''}>編集後未検証</label>
      </div>
    </details>
    <div id="results"><div class="empty">…</div></div>
  </section>`;

  const rerun = () => runSearch();
  $('#q')?.addEventListener('input', () => { explore.q = $('#q').value; debounce(rerun, 250); });
  // Autofocus is a desktop nicety; on touch screens it pops the keyboard
  // over the page before the user asked for it.
  if (matchMedia('(pointer: fine)').matches) $('#q')?.focus({ preventScroll: true });
  document.querySelectorAll('.fb-row input[data-type]').forEach(cb =>
    cb.addEventListener('change', () => { cb.checked ? explore.types.add(cb.dataset.type) : explore.types.delete(cb.dataset.type); rerun(); }));
  document.querySelectorAll('.fb-row input[data-status]').forEach(cb =>
    cb.addEventListener('change', () => { cb.checked ? explore.statuses.add(cb.dataset.status) : explore.statuses.delete(cb.dataset.status); rerun(); }));
  for (const [sel, key] of [['#f-verified', 'verified']]) {
    $(sel).addEventListener('change', () => { explore[key] = $(sel).checked; rerun(); });
  }
  $('#f-type-extra').addEventListener('keydown', e => {
    if (e.key === 'Enter' && e.target.value.trim()) { explore.types.add(e.target.value.trim()); viewExplore(); }
  });
  $('#f-tag').addEventListener('input', () => { explore.tag = $('#f-tag').value.trim(); debounce(rerun, 250); });
  $('#f-prefix').addEventListener('input', () => { explore.prefix = $('#f-prefix').value.replace(/^\/+|\/+$/g, '').trim(); debounce(rerun, 250); });
  // The three feeds are listing modes, not filters: each replaces the
  // query, so picking one clears the others.
  const feeds = [['#f-age', 'ageFeed'], ['#f-failed', 'failedFeed'], ['#f-expired', 'expiredFeed'], ['#f-edited', 'editedFeed']];
  for (const [sel, key] of feeds) {
    $(sel).addEventListener('change', () => {
      const on = $(sel).checked;
      for (const [, k] of feeds) explore[k] = false;
      explore[key] = on;
      viewExplore();
    });
  }

  runSearch();
}

export function debounce(fn, ms) {
  clearTimeout(debounce._t);
  debounce._t = setTimeout(fn, ms);
}

// Below this score a lexical hit is almost certainly noise: a real match
// contains the query whole and carries the server's 0.3 bonus for it,
// while an entry sharing one common fragment with a question scores a
// small fraction of the query's fragments (plus 0.05 if verified). Only
// meaningful in lexical mode — hybrid (RRF) scores live on a ~1/60 scale,
// detected below by the top score, where no useful floor exists.
export const WEAK_SCORE = 0.15;

// append reads the next page of a feed and keeps what is already on
// screen; every other call starts the listing over.
export async function runSearch(append = false) {
  const out = $('#results');
  if (!out) return;
  const p = new URLSearchParams();
  const isFeed = explore.ageFeed || explore.failedFeed || explore.expiredFeed || explore.editedFeed || !!explore.source;
  // The query as the server and the messages below see it: a box holding
  // only spaces is no query, and saying `「  」に一致するナレッジが
  // 見つかりません` describes something the user did not ask.
  const q = explore.q.trim();
  // source is a filter, not a mode: it rides along with whatever the
  // chain below picks (design doc 0037 §2.3).
  if (explore.source) p.set('source', explore.source);
  if (explore.ageFeed) p.set('sort', 'verified_at');
  else if (explore.failedFeed) p.set('sort', 'failed');
  else if (explore.expiredFeed) p.set('sort', 'stale_after');
  // The edited queue is a combination, not a sort (design doc 0138):
  // trust=unverified keeps only entries with no standing verification,
  // and the verification-age order puts the lapsed ones — confirmed
  // once, then edited — ahead of the never-verified tail.
  else if (explore.editedFeed) { p.set('sort', 'verified_at'); p.append('trust', 'unverified'); }
  else if (q) p.set('q', explore.q);
  // Nothing selects a mode: the landing view lists by demand rather than
  // by address. A request carrying no mode at all is the plain
  // enumeration, which this view could ask for and deliberately does not
  // — what somebody opens Explore for is the queue, and the tree beside
  // it is already the address order. A source filter alone is already a
  // listing, so it stays put.
  else if (!explore.source) p.set('sort', 'usage');
  for (const t of explore.types) p.append('type', t);
  for (const s of explore.statuses) p.append('status', s);
  // The filter asks SPEC §5.3's question — who confirmed this — rather
  // than a boolean the spec has no word for (design doc 0046 §3.10).
  // The edited feed already speaks in trust (unverified); adding the
  // confirmed tiers on top would OR the vocabulary back to everything.
  if (explore.verified && !explore.editedFeed) { p.append('trust', 'human-reviewed'); p.append('trust', 'machine-confirmed'); }
  if (explore.tag) p.append('tag', explore.tag);
  // A path scope is a filter like tag, so it rides along with whatever
  // mode the chain above picked (design doc 0041 §2.1). The UI takes one
  // scope where the API takes many: a person narrows to the directory
  // they are looking at, while "my scope and the shared one at once" is
  // the agent's question, asked in one call through REST or MCP.
  if (explore.prefix) p.append('prefix', explore.prefix);
  // Search caps at 50 server-side; a feed reads 100 at a time and walks
  // the rest with a cursor (design doc 0050 §2.1), so "load more" appends
  // a page instead of asking for a bigger one.
  const limit = isFeed ? 100 : 50;
  p.set('limit', String(limit));
  if (append && explore.cursor) p.set('cursor', explore.cursor);
  else { explore.loaded = []; explore.cursor = ''; }
  const my = runSearch._seq = (runSearch._seq || 0) + 1;
  // A scope narrows every result below it, so it is stated above them
  // rather than only in the filter bar — which collapses on narrow
  // screens, where an empty result would otherwise look like an empty
  // knowledge base.
  const scopeNote = explore.prefix
    ? `<div class="truncation-note"><code>/${esc(explore.prefix)}</code> とその下だけに絞っています ·
         <a href="#" id="scope-clear">全体を検索</a></div>`
    : '';
  const wireScope = () => $('#scope-clear')?.addEventListener('click', e => {
    e.preventDefault();
    explore.prefix = '';
    const box = $('#f-prefix');
    if (box) box.value = '';
    runSearch();
  });
  try {
    const page = await api('/api/v1/search?' + p);
    if (my !== runSearch._seq || out !== $('#results')) return; // stale response
    // The cursor is the server's word for "there is more"; its absence
    // ends the listing, and no count comes with either (0050 §2.3).
    const hits = explore.loaded = explore.loaded.concat(page.hits || []);
    explore.cursor = page.cursor || '';
    if (!hits.length) {
      out.innerHTML = scopeNote + `<div class="empty">${q ? '「' + esc(q) + '」に一致する' : ''}ナレッジが見つかりません。</div>`;
      wireScope();
      return;
    }
    // With a query, split off low-relevance hits so junk doesn't render as
    // if it matched — semantic and lexical search always return *something*.
    let strong = hits, weak = [];
    if (q && !isFeed && hits[0].score > 0.05) {
      strong = hits.filter(h => h.score >= WEAK_SCORE);
      weak = hits.filter(h => h.score < WEAK_SCORE);
    }
    let html = strong.map(hitCard).join('');
    if (!strong.length) {
      html = `<div class="empty">「${esc(q)}」に強く一致するナレッジはありません。</div>`;
    }
    // Nothing typed: this is the most-searched listing, not everything
    // there is. The feeds say what they are; this one said nothing.
    if (!isFeed && !q) {
      html = `<div class="truncation-note">検索語が未入力です。よく検索されるナレッジから順に表示しています。</div>` + html;
    }
    // The server says the ranking is worse than it normally gives
    // (design doc 0114). The page says it in its own words rather than
    // printing the server's: `degraded` is a value, and the sentence
    // belongs to whoever is reading it — here, a Japanese-speaking
    // curator wondering why a search they have run before came back thin.
    if (page.degraded) {
      html = `<div class="truncation-note">埋め込みが利用できなかったため、この結果は語の一致だけで並べています。意味が近くても語が異なるナレッジは含まれていません。しばらくしてから、もう一度お試しください。</div>` + html;
    }
    if (weak.length) {
      html += `<details class="weak-matches"><summary>弱い一致 ${weak.length} 件
        (ほとんど関係がなく、ノイズの可能性が高いものです)</summary>${weak.map(hitCard).join('')}</details>`;
    }
    if (explore.cursor) {
      html += `<div class="truncation-note">ナレッジ ${hits.length} 件を表示 ·
           <a href="#" id="feed-more">さらに読み込む</a></div>`;
    } else if (!isFeed && hits.length >= limit) {
      // A search does not page: 50 is the whole contract, and the way on
      // is a narrower question rather than a next page (0050 §2.2).
      html += `<div class="truncation-note">先頭 ${limit} 件を表示しています(サーバー側の上限)。絞り込むか、より具体的な問いで範囲を狭めてください。</div>`;
    }
    out.innerHTML = scopeNote + html;
    // The words that were typed, marked in the text they matched. It
    // runs on the rendered cards rather than on the strings above,
    // because a mark inserted into markup is a mark that can break it —
    // here it can only ever replace a text node. A feed answers no
    // query, so there is nothing to mark in one.
    if (q && !isFeed) {
      const ts = terms(q);
      out.querySelectorAll('.card .title, .card .desc, .card .snippet').forEach(el => highlightIn(el, ts));
    }
    wireScope();
    $('#feed-more')?.addEventListener('click', e => { e.preventDefault(); runSearch(true); });
  } catch (e) {
    if (my !== runSearch._seq || out !== $('#results')) return;
    out.innerHTML = `<div class="error-banner" role="alert">検索に失敗しました: ${esc(e.message)}</div>`;
  }
}
