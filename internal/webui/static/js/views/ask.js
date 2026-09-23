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
// runs no SQL, and the page says so where the question is typed rather
// than leaving the reader to find out from an answer.

import { AGENT_CLIENT, api, toast } from '../api.js';
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
// The billing project a person runs proposals in, remembered per browser:
// it is theirs, not the deployment's.
const PROJECT_KEY = 'ochakai.bq-project';

let turns = load();

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
    <div class="hint" style="margin-bottom:.6rem">エージェントはナレッジを読んで答え、引いたナレッジが人に確かめられたものかを答えの中で言います。書き込み・裁定・SQL の実行はしません — 書き足すべきことを見つけたら、その本文を答えに載せます。</div>
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
    : `<div class="card ask-turn ask-agent">${md(t.text)}${t.sql ? proposalHTML(t.sql, i === last && !pending) : ''}${readLine(t.read)}</div>`).join('');
  $('#ask-turns').innerHTML = out + (pending
    ? '<div class="empty">エージェントが読んでいます…</div>'
    : (turns.length ? '' : '<div class="empty">まだ何も訊いていません。</div>'));
  const left = MAX_TURNS - turns.length;
  $('#ask-count').textContent = left <= 6 ? `この会話はあと ${Math.max(0, Math.floor(left / 2))} 往復まで` : '';
  $('#ask-send').disabled = !!pending || left < 1;
  $('#ask-run')?.addEventListener('click', runProposal);
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
    ? `あなたの Google アカウントの権限(BigQuery の読み取りだけ)で実行します。一回の上限は ${fmtBytes(MAX_BYTES_BILLED)} です。`
    : 'このデプロイには実行のためのサインインが設定されていません。自分で実行して、結果を次のメッセージに貼ってください。';
  return `
    <div class="ask-proposal">
      <div class="hint">エージェントはこの SQL の実行を提案しています。${esc(how)}</div>
      <textarea id="ask-sql" rows="${Math.min(14, sql.query.split('\n').length + 1)}" aria-label="提案された SQL">${esc(sql.query)}</textarea>
      ${AGENT_CLIENT ? `<div class="toolbar">
        <label class="check">課金するプロジェクト <input type="text" id="ask-project" value="${esc(project)}" placeholder="my-project" style="width:12rem"></label>
        <button type="button" id="ask-run" class="btn primary">実行して結果を返す</button>
      </div>` : ''}
    </div>`;
}

async function runProposal() {
  const query = $('#ask-sql').value.trim();
  const project = $('#ask-project').value.trim();
  if (!query || !project) { toast('SQL と課金するプロジェクトが要ります'); return; }
  try { localStorage.setItem(PROJECT_KEY, project); } catch { /* remembered for this run only */ }
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
    turns.push({ role: 'agent', text: ans.text, read: ans.read || [], sql: ans.sql || null });
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
