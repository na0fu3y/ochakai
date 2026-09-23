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
// runs no SQL — where it proposes a query, the person runs it. The page
// does not preface the conversation with that: the proposal says who
// runs it where it is run, and an answer says what it read.
//
// What the agent said and what the page says are drawn apart: the
// answer is the card's body, and the page's own lines — what was read,
// the proposal's controls, the verdict — sit under a rule beneath it.

import { AGENT_CLIENT, AGENT_PROJECT, PROXY_RUNS, api, toast } from '../api.js';
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
    <div class="section-title">エージェント</div>
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
}

function draw(pending) {
  const last = turns.length - 1;
  const out = turns.map((t, i) => t.role === 'user'
    ? `<div class="ask-turn ask-user">${t.sqlResult ? md(t.text) : esc(t.text).replace(/\n/g, '<br>')}</div>`
    : `<div class="card ask-turn ask-agent"><div class="ask-said">${md(t.text)}</div>`
      + `<div class="ask-meta">${t.sql ? proposalHTML(t.sql, i === last && !pending) : ''}${readLine(t.read)}${verdictHTML(t, i)}</div></div>`).join('');
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

// The verdict of the person who asked (design doc 0142 §3), offered one
// way only: a person rarely stops to say an answer was right, so the
// page asks for nothing until one was wrong, and then for one sentence.
// It becomes a "failed" outcome on no concept — which concept misled
// the answer is the triage's question, not something to make the person
// pick from a list of ids — and verifies nothing. Hidden on a read-only
// deployment, which refuses reports.
function verdictHTML(t, i) {
  if (!t.turn) return '';
  if (t.verdict) return '<div class="hint">👎 記録済み</div>';
  return `<div class="ask-verdict write-only">
      <button type="button" class="btn small" data-verdict data-i="${i}" title="この答えは違う" aria-label="この答えは違う">👎</button>
      <div class="ask-verdict-form" id="verdict-${i}" hidden>
        <textarea id="note-${i}" rows="2" placeholder="何が違っていたか" aria-label="何が違っていたか"></textarea>
        <button type="button" class="btn primary small" data-judge data-i="${i}">記録する</button>
      </div>
    </div>`;
}

function openVerdict(e) {
  const i = Number(e.currentTarget.dataset.i), box = $('#verdict-' + i);
  box.hidden = !box.hidden;
  if (box.hidden) return;
  box.querySelector('[data-judge]').onclick = judge;
  $('#note-' + i).focus();
}

async function judge(e) {
  const i = Number(e.currentTarget.dataset.i);
  const t = turns[i];
  const body = { verdict: 'bad', note: $('#note-' + i).value.trim() };
  e.currentTarget.disabled = true;
  try {
    await api('/api/v1/agent/turns/' + encodeURIComponent(t.turn), { method: 'POST', body });
    t.verdict = 'bad';
    save();
    toast('記録しました');
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
  const runs = AGENT_CLIENT || PROXY_RUNS;
  const who = PROXY_RUNS
    ? 'ochakai ui を動かしているあなたの Google アカウントの権限で、SELECT であることを確かめてから実行します。'
    : 'あなたの Google アカウントの権限(BigQuery の読み取りだけ)で実行します。';
  const how = runs
    ? `${who}${AGENT_PROJECT ? `課金はプロジェクト ${AGENT_PROJECT} で、` : ''}一回の上限は ${fmtBytes(MAX_BYTES_BILLED)} です。`
    : 'このデプロイには、このページから実行するためのサインインが設定されていません。自分で実行して、結果を次のメッセージに貼ってください(手元の ochakai ui からなら、そのまま実行できます)。';
  return `
    <div class="ask-proposal">
      <div class="hint">エージェントはこの SQL の実行を提案しています。${esc(how)}</div>
      <textarea id="ask-sql" rows="${Math.min(14, sql.query.split('\n').length + 1)}" aria-label="提案された SQL">${esc(sql.query)}</textarea>
      ${runs ? `<div class="toolbar">
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
    const tok = PROXY_RUNS ? null : await signIn(AGENT_CLIENT);
    const res = await run(tok, project, query);
    turns.push({ role: 'user', text: asMessage(query, res), sqlResult: true });
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
