// Asking the deployment's own agent (design doc 0142), for the person who
// has no agent of their own.
//
// The page holds the conversation and sends all of it on every turn: the
// server keeps none (design doc 0143 §0), so there is no session to lose
// and nothing to clean up. It is kept in sessionStorage so that opening a
// concept the answer cited and coming back does not end the conversation;
// closing the tab does, which is the same lifetime a chat window has.
//
// The agent reads and answers. It writes nothing, rules on nothing and
// runs no SQL — where it proposes a query, the person runs it — and the
// page says so where the question is typed rather than leaving the
// reader to find out from an answer.

import { AGENT_CLIENT, AGENT_PROJECT, api, postureSettled, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { entryHash } from '../format.js';
import { md } from '../markdown.js';
import { asMessage, fmtBytes, MAX_BYTES_BILLED, run, signIn } from '../sql.js';

// The server refuses a conversation longer than this (internal/agent).
// Said here so the page can offer a fresh start before the refusal, not
// after it.
const MAX_TURNS = 40;
const KEY = 'ochakai.ask';
// The billing project a person runs proposals in, where the operator
// named none — remembered per browser, since it is then theirs.
const PROJECT_KEY = 'ochakai.bq-project';

let turns = load();
let ticking = 0; // the interval counting a pending answer's seconds

function load() {
  try {
    const v = JSON.parse(sessionStorage.getItem(KEY) || '[]');
    return Array.isArray(v) ? v : [];
  } catch { return []; }
}

function save() {
  try { sessionStorage.setItem(KEY, JSON.stringify(turns)); } catch { /* the conversation still works; it just will not survive a reload */ }
}

export function viewAsk() {
  view.innerHTML = `
    <div class="section-title">エージェントに訊く</div>
    <div class="hint" style="margin-bottom:.6rem">エージェントはナレッジを読んで答え、引いたナレッジが人に確かめられたものかを答えの中で言います。ナレッジの書き込みと裁定はしません — 書き足すべきことを見つけたら、その本文を答えに載せます。<span class="agent-sql-only">データが要る問いには SQL を提案します。走るのは、あなたが「実行して結果を返す」を押したときだけで、あなたの Google アカウントの権限で走ります。</span><span class="agent-no-sql">SQL は書いて見せるだけで、実行はしません。</span></div>
    <div id="ask-turns"></div>
    <div class="ask-form">
      <textarea id="ask-text" rows="3" placeholder="例: 先月の売上はどう数えればいい？(⌘/Ctrl + Enter で送る)" aria-label="エージェントへの質問"></textarea>
      <div class="toolbar">
        <button type="button" id="ask-send" class="btn primary">送る</button>
        <button type="button" id="ask-new" class="btn">新しい会話</button>
        <span class="grow"></span>
        <span class="hint" id="ask-count"></span>
      </div>
    </div>`;
  // Not a form submit: asking writes nothing, so it stays available on a
  // read-only deployment, and the page's submit buttons are the ones the
  // read-only rule hides.
  $('#ask-send').addEventListener('click', send);
  $('#ask-text').addEventListener('keydown', e => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); send(); }
  });
  $('#ask-new').addEventListener('click', () => { turns = []; save(); draw(); $('#ask-text').focus(); });
  draw();
  // A reload restores the conversation from sessionStorage and draws it
  // before /api/v1/stats answers (main.js fires that request but does
  // not wait for it), so an open proposal's branch on AGENT_CLIENT —
  // the run button, the billing-project field — can be drawn on the
  // stale empty default and then never revisited. Redraw once the real
  // value has landed; harmless where there was nothing to fix.
  postureSettled.then(() => { if ($('#ask-turns') && !$('#ask-pending')) draw(); });
}

function draw(pending) {
  const last = turns.length - 1;
  const out = turns.map((t, i) => t.role === 'user'
    ? `<div class="ask-turn ask-user">${t.sqlResult ? md(t.text) : esc(t.text).replace(/\n/g, '<br>')}</div>`
    : `<div class="card ask-turn ask-agent">${md(t.text)}${t.sql ? proposalHTML(t.sql, i === last && !pending) : ''}${readLine(t.read)}${verdictHTML(t, i)}</div>`).join('');
  $('#ask-turns').innerHTML = out + (pending
    ? '<div class="empty" id="ask-pending">エージェントが読んでいます…</div>'
    : (turns.length ? '' : '<div class="empty">まだ何も訊いていません。</div>'));
  clearInterval(ticking);
  if (pending) tick(Date.now());
  const left = MAX_TURNS - turns.length;
  $('#ask-count').textContent = left <= 6 ? `この会話はあと ${Math.max(0, Math.floor(left / 2))} 往復まで` : '';
  $('#ask-send').disabled = !!pending || left < 1;
  $('#ask-run')?.addEventListener('click', runProposal);
  document.querySelectorAll('[data-verdict]').forEach(b => b.addEventListener('click', openVerdict));
}

// tick says how long a pending answer has taken. The server answers in
// one piece, often after half a minute of reading, and a line that never
// changes for that long reads as a page that has stopped.
function tick(start) {
  ticking = setInterval(() => {
    const el = $('#ask-pending');
    if (!el) { clearInterval(ticking); return; }
    const s = Math.round((Date.now() - start) / 1000);
    el.textContent = `エージェントが読んでいます…(${s} 秒)`
      + (s >= 15 ? ' ナレッジを何件か読んでから答えるので、一分ほどかかることがあります。' : '');
  }, 1000);
}

// The verdict of the person who asked (design doc 0142 §3). It becomes
// their own outcome reports — "worked" for what a good answer read,
// "failed" only for what they say misled a bad one — and verifies
// nothing. Hidden on a read-only deployment, which refuses reports.
function verdictHTML(t, i) {
  if (!t.turn) return '';
  if (t.verdict) {
    return `<div class="hint">判定: ${t.verdict === 'good' ? '合っている' : '違う'}(記録済み)</div>`;
  }
  return `<div class="ask-verdict write-only" data-turn="${i}">
      <span class="hint">この答えは</span>
      <button type="button" class="btn small" data-verdict="good" data-i="${i}">合っている</button>
      <button type="button" class="btn small" data-verdict="bad" data-i="${i}">違う</button>
      <div class="ask-verdict-form" id="verdict-${i}" hidden></div>
    </div>`;
}

function openVerdict(e) {
  const i = Number(e.currentTarget.dataset.i), verdict = e.currentTarget.dataset.verdict;
  const t = turns[i], box = $('#verdict-' + i);
  const read = t.read || [];
  box.innerHTML = verdict === 'good'
    ? `<label class="check"><input type="checkbox" id="keep-${i}"> この問いを比較に使う(棚卸しが、これからもこの答えに届くかを確かめる)</label>`
    : `<div class="hint">どのナレッジが答えを誤らせましたか。分からなければ選ばずに記録してください — そのときはどのナレッジにも失敗を報告しません。</div>`
      + read.map(id => `<label class="check"><input type="checkbox" data-blame="${esc(id)}"> <code>${esc(id)}</code></label>`).join('<br>');
  box.innerHTML += `
      <input type="text" id="note-${i}" placeholder="一言(任意)— 何が合っていた / 違っていたか" style="width:100%;margin:.3rem 0">
      <button type="button" class="btn primary small" data-judge="${verdict}" data-i="${i}">記録する</button>`;
  box.hidden = false;
  box.querySelector('[data-judge]').addEventListener('click', judge);
}

async function judge(e) {
  const i = Number(e.currentTarget.dataset.i), verdict = e.currentTarget.dataset.judge;
  const t = turns[i], box = $('#verdict-' + i);
  const body = { verdict, note: $('#note-' + i).value.trim() };
  if (verdict === 'good') body.keep = $('#keep-' + i).checked;
  else body.blame = [...box.querySelectorAll('[data-blame]:checked')].map(c => c.dataset.blame);
  e.currentTarget.disabled = true;
  try {
    const res = await api('/api/v1/agent/turns/' + encodeURIComponent(t.turn), { method: 'POST', body });
    t.verdict = verdict;
    save();
    toast(res.reported.length ? `記録しました(${res.reported.length} 件のナレッジに報告)` : '記録しました');
    draw();
  } catch (err) {
    e.currentTarget.disabled = false;
    toast('記録できませんでした: ' + err.message, 6000);
  }
}

// A proposal is the agent asking the person to run something. Only the
// newest one can be run — an older one has already been answered or
// passed over — and the person can edit it first: the message that goes
// back says which SQL actually ran.
function proposalHTML(sql, open) {
  if (!open) return `<pre><code>${esc(sql.query)}</code></pre>`;
  let project = '';
  try { project = localStorage.getItem(PROJECT_KEY) || ''; } catch { /* asked each time */ }
  const how = AGENT_CLIENT
    ? `あなたの Google アカウントの権限(BigQuery の読み取りだけ)で実行します。${AGENT_PROJECT ? `課金はプロジェクト ${AGENT_PROJECT} で、` : ''}一回の上限は ${fmtBytes(MAX_BYTES_BILLED)} です。`
    : 'このデプロイには実行のためのサインインが設定されていません。自分で実行して、結果を次のメッセージに貼ってください。';
  return `
    <div class="ask-proposal">
      <div class="hint">エージェントはこの SQL の実行を提案しています。${esc(how)}</div>
      <textarea id="ask-sql" rows="${Math.min(14, sql.query.split('\n').length + 1)}" aria-label="提案された SQL">${esc(sql.query)}</textarea>
      ${AGENT_CLIENT ? `<div class="toolbar">
        ${AGENT_PROJECT ? '' : `<label class="check">課金するプロジェクト <input type="text" id="ask-project" value="${esc(project)}" placeholder="my-project" style="width:12rem"></label>`}
        <button type="button" id="ask-run" class="btn primary">実行して結果を返す</button>
      </div>` : ''}
    </div>`;
}

async function runProposal() {
  const query = $('#ask-sql').value.trim();
  const project = AGENT_PROJECT || $('#ask-project').value.trim();
  if (!query || !project) { toast('SQL と課金するプロジェクトが要ります'); return; }
  if (!AGENT_PROJECT) {
    try { localStorage.setItem(PROJECT_KEY, project); } catch { /* remembered for this run only */ }
  }
  const btn = $('#ask-run');
  btn.disabled = true;
  btn.textContent = '実行しています…';
  try {
    const tok = await signIn(AGENT_CLIENT);
    const res = await run(tok, project, query);
    turns.push({ role: 'user', text: asMessage(project, query, res), sqlResult: true });
    save();
    await answer();
  } catch (e) {
    toast('実行できませんでした: ' + e.message, 8000);
    btn.disabled = false;
    btn.textContent = '実行して結果を返す';
  }
}

function readLine(ids) {
  if (!ids || !ids.length) return '';
  const links = ids.map(id => `<a href="${esc(entryHash({ id }))}"><code>${esc(id)}</code></a>`).join(' ');
  return `<div class="hint">読んだナレッジ: ${links}</div>`;
}

async function send() {
  const box = $('#ask-text');
  const text = box.value.trim();
  if (!text || $('#ask-send').disabled) return;
  turns.push({ role: 'user', text });
  box.value = '';
  if (!await answer()) box.value = text;
}

// answer asks the agent for the next turn of the conversation as it
// stands. On failure the last message comes off again: left there, the
// next send would carry it as history the agent never answered.
async function answer() {
  draw(true);
  try {
    const ans = await api('/api/v1/agent', {
      method: 'POST',
      // A proposal travels back as part of the agent's own message, so
      // the model reads what it asked for beside what came back.
      body: { messages: turns.map(t => ({ role: t.role, text: t.sql ? `${t.text}\n\n\`\`\`sql\n${t.sql.query}\n\`\`\`` : t.text })) },
    });
    turns.push({ role: 'agent', text: ans.text, read: ans.read || [], sql: ans.sql || null, turn: ans.turn || '' });
    save();
    draw();
    return true;
  } catch (e) {
    turns.pop();
    save();
    toast('答えられませんでした: ' + e.message, 6000);
    draw();
    return false;
  }
}
