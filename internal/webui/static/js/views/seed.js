// Seeding from BigQuery (design doc 0148): the page reads one dataset's
// schema as the person — the same sign-in, the same read-only scope and
// the same billing cap as a query the agent proposed (0142 §4) — turns
// it into draft table concepts exactly as `ochakai seed` does, and writes
// each one with the ordinary PUT. The server runs nothing and learns
// nothing new: what reaches it is a draft, as if the person had typed it.

import { AGENT_CLIENT, AGENT_PROJECT, PROXY_RUNS, api, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { idPath } from '../format.js';
import { parseSource, project, schemaQuery } from '../seed.js';
import { MAX_BYTES_BILLED, PROJECT_KEY, fmtBytes, runAll, signIn } from '../sql.js';

// Rows one listing may hold. A dataset of 92 tables is about 2,000 rows;
// past this the person narrows to a table rather than seeding a
// warehouse's worth of drafts nobody will get round to reviewing.
const MAX_ROWS = 50_000;

let pending = null; // { source, concepts } read and not yet written

function billing() {
  if (AGENT_PROJECT) return AGENT_PROJECT;
  try { return localStorage.getItem(PROJECT_KEY) || ''; } catch { return ''; }
}

export function viewSeed() {
  pending = null;
  const runs = AGENT_CLIENT || PROXY_RUNS;
  view.innerHTML = `
    <div class="section-title">BigQuery から取り込む</div>
    <p style="color:var(--muted);max-width:44rem">データセットのスキーマ(<code>INFORMATION_SCHEMA</code>)を<strong>あなた自身の権限で</strong>読み、テーブル一つにつき <code>BigQuery Table</code> の <strong>draft</strong> を一つ作ります。列は表になり、テーブルが何のためにあるか・どの列が嘘をつくかは空のままです — それを書くのがレビューです。日付でシャードされたテーブル(<code>events_20260101</code>…)は一つにまとまります。<code>ochakai seed</code> と同じ結果です。</p>
    ${runs ? '' : `<div class="error-banner" role="alert">このデプロイでは、このページから BigQuery を読めません。運用者が <code>OCHAKAI_OAUTH_CLIENT_ID</code> を設定するか、手元で <code>ochakai ui</code> を使ってください。CLI なら <code>ochakai seed</code> が同じことをします。</div>`}
    <form id="seed-form" class="seed-form">
      <label>取り込み元 <input type="text" id="seed-source" required placeholder="bigquery-public-data.thelook_ecommerce" autocomplete="off" spellcheck="false"></label>
      <div class="hint">データセット(<code>project.dataset</code>)か、テーブル一つ(<code>project.dataset.table</code>)。末尾の <code>*</code> は名前の先頭で絞ります — 日付シャードなら <code>events_*</code>。</div>
      ${AGENT_PROJECT ? '' : `<label>課金するプロジェクト <input type="text" id="seed-billing" required placeholder="my-project" value="${esc(billing())}" spellcheck="false"></label>
      <div class="hint">クエリのジョブを作るあなたのプロジェクトです。パブリックデータを読むときも要ります。</div>`}
      <label>置き場所 <input type="text" id="seed-prefix" value="tables" spellcheck="false"></label>
      <div class="hint">ナレッジは <code>&lt;置き場所&gt;/&lt;データセット&gt;/&lt;テーブル&gt;</code> に作られます。</div>
      <div><button class="btn" type="submit" id="seed-read" ${runs ? '' : 'disabled'}>スキーマを読む</button></div>
    </form>
    <div id="seed-result"></div>`;
  $('#seed-form').addEventListener('submit', e => { e.preventDefault(); read(); });
}

async function read() {
  const out = $('#seed-result');
  let source;
  try { source = parseSource($('#seed-source').value); } catch (e) { toast(e.message, 6000); return; }
  const proj = AGENT_PROJECT || $('#seed-billing').value.trim();
  if (!proj) { toast('課金するプロジェクトが要ります'); return; }
  if (!AGENT_PROJECT) {
    try { localStorage.setItem(PROJECT_KEY, proj); } catch { /* remembered for this visit only */ }
  }
  const prefix = $('#seed-prefix').value.trim().replace(/^\/+|\/+$/g, '');
  const btn = $('#seed-read');
  btn.disabled = true;
  btn.textContent = '読んでいます…';
  try {
    const tok = PROXY_RUNS ? null : await signIn(AGENT_CLIENT);
    const res = await runAll(tok, proj, schemaQuery(source), MAX_ROWS);
    const concepts = project(res.rows, { project: source.project, prefix });
    if (!out.isConnected) return;
    if (!concepts.length) {
      out.innerHTML = '<div class="empty">テーブルが見つかりませんでした。名前と、あなたにそのデータセットを読む権限があるかを確かめてください。</div>';
      return;
    }
    pending = { source, concepts };
    drawPreview(res.bytes);
  } catch (e) {
    if (out.isConnected) out.innerHTML = `<div class="error-banner" role="alert">読めませんでした: ${esc(e.message)}</div>`;
  } finally {
    btn.disabled = false;
    btn.textContent = 'スキーマを読む';
  }
}

function drawPreview(bytes) {
  const { concepts } = pending;
  const folded = concepts.filter(c => c.shards).length;
  const rows = concepts.map(c => `<tr><td class="mono">${esc(c.id)}</td><td class="num">${c.columns}</td><td>${c.shards ? `${c.shards} 日分をまとめた` : ''}</td></tr>`).join('');
  $('#seed-result').innerHTML = `
    <div class="hint">${concepts.length} テーブル${folded ? `(うち ${folded} つはシャードをまとめたもの)` : ''} — ${esc(fmtBytes(bytes))} 読み取り、上限 ${esc(fmtBytes(MAX_BYTES_BILLED))}</div>
    <div class="ask-table"><table><thead><tr><th>作られるナレッジ</th><th class="num">列</th><th></th></tr></thead><tbody>${rows}</tbody></table></div>
    <p class="hint">既にある住所は上書きしません — 誰かが書き足した説明を、スキーマで消さないためです。</p>
    <div class="write-only"><button class="btn" id="seed-write">${concepts.length} 件を draft として取り込む</button></div>
    <div id="seed-progress"></div>`;
  $('#seed-write').addEventListener('click', write);
}

// write puts each draft with If-None-Match: *, one at a time, as
// `ochakai import` does. An occupied address is left alone and counted:
// a table somebody already described must not be reset to its schema.
async function write() {
  const { source, concepts } = pending;
  const btn = $('#seed-write');
  const progress = $('#seed-progress');
  btn.disabled = true;
  let created = 0, kept = 0;
  const failed = [];
  for (const [i, c] of concepts.entries()) {
    if (!progress.isConnected) return;
    progress.textContent = `${i + 1} / ${concepts.length}…`;
    try {
      await api(`/api/v1/bundle/${idPath(c.id)}.md`, { method: 'PUT', doc: c.document, onlyIfAbsent: true });
      created++;
    } catch (e) {
      if (e.code === 'already_exists') kept++;
      else failed.push({ id: c.id, message: e.message });
    }
  }
  const dir = concepts[0].id.split('/').slice(0, -1).join('/');
  progress.innerHTML = `
    <p>${created} 件を draft として作りました${kept ? `。${kept} 件は既にあったので触れていません` : ''}。</p>
    ${failed.length ? `<div class="error-banner" role="alert">${failed.length} 件は書けませんでした:<ul>${failed.map(f => `<li><span class="mono">${esc(f.id)}</span> — ${esc(f.message)}</li>`).join('')}</ul></div>` : ''}
    <p><a href="#/dir/${idPath(dir)}">${esc(dir)} を開く</a> ・ <a href="#/review">レビューキューで中身を書く</a></p>`;
  pending = { source, concepts: [] };
}
