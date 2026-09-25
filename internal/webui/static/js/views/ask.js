// Asking the deployment's own agent (design doc 0142), for the person who
// has no agent of their own.
//
// The page holds the conversation and sends all of it on every turn: the
// server keeps none (design doc 0145 §0), so there is no session to lose
// and nothing to clean up. It is kept in sessionStorage so that opening a
// concept the answer cited and coming back does not end the conversation;
// closing the tab does, which is the same lifetime a chat window has.
//
// The agent reads and answers, and may write drafts. It rules on nothing,
// replaces nothing and runs no SQL — where it proposes a query, the
// person runs it; where it writes a draft, a person rules on it. The page
// does not preface the conversation with that: the proposal says who
// runs it where it is run, and an answer says what it read.
//
// A person may agree, once per conversation, that the page runs what the
// agent proposes without asking each time (ROADMAP, stage 1). The query
// still runs as them, read-only and under the byte cap; what changes is
// only who clicks. A query that fails goes back to the agent as the
// person's next message, so it can correct itself instead of stopping.
//
// What the agent said and what the page says are drawn apart: the
// answer is the card's body, and the page's own lines — what was read,
// the proposal's controls, the verdict — sit under a rule beneath it.

import { AGENT_CLIENT, AGENT_PROJECT, PROXY_RUNS, api, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { entryHash } from '../format.js';
import { md } from '../markdown.js';
import { chartHTML, NUMERIC_TYPES } from '../chart.js';
import { asFailure, asMessage, fmtBytes, fold, hasToken, MAX_BYTES_BILLED, run, signIn } from '../sql.js';

// The server refuses a conversation longer than this (internal/agent).
// Said here so the page can offer a fresh start before the refusal, not
// after it.
const MAX_TURNS = 40;
const KEY = 'ochakai.ask';
// The billing project a person runs proposals in, where the operator
// named none — remembered per browser, since it is then theirs.
const PROJECT_KEY = 'ochakai.bq-project';
// Whether the person agreed to automatic runs in this conversation. Kept
// beside the conversation, and ended with it.
const AUTO_KEY = 'ochakai.ask.auto';
// How many queries the page runs by itself before a person has said
// anything again. A proposal past this waits for a click: an agent that
// needs more rounds than this is more likely looping than converging.
const MAX_AUTO_RUNS = 6;

let turns = load();
let auto = loadAuto();
let ticking = 0; // the interval counting a pending answer's seconds

function load() {
  try {
    const v = JSON.parse(sessionStorage.getItem(KEY) || '[]');
    return Array.isArray(v) ? v : [];
  } catch { return []; }
}

function loadAuto() {
  try { return sessionStorage.getItem(AUTO_KEY) === '1'; } catch { return false; }
}

function setAuto(on) {
  auto = on;
  try { sessionStorage.setItem(AUTO_KEY, on ? '1' : ''); } catch { /* holds for this page only */ }
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
        <span id="ask-auto-state"></span>
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
  $('#ask-new').addEventListener('click', () => { turns = []; save(); setAuto(false); draw(); $('#ask-text').focus(); });
  draw();
}

// draw redraws the conversation. pending is 'read' while the agent is
// answering and 'run' while the page is running its query.
function draw(pending) {
  if (!$('#ask-turns')) return; // the person has moved to another view
  const last = turns.length - 1;
  const out = turns.map((t, i) => t.role === 'user'
    ? (t.res || t.sqlFailed ? resultHTML(t, i)
      : `<div class="ask-turn ask-user">${t.sqlResult ? md(t.text) : esc(t.text).replace(/\n/g, '<br>')}</div>`)
    : `<div class="card ask-turn ask-agent"><div class="ask-said">${md(t.text)}</div>`
      + `<div class="ask-meta">${t.sql ? proposalHTML(t.sql, i === last && !pending) : ''}${readLine(t.read)}${draftLine(t.drafts)}${verdictHTML(t, i)}</div></div>`).join('');
  $('#ask-turns').innerHTML = out + (pending
    ? `<div class="empty" id="ask-pending">${pending === 'run' ? '提案された SQL を実行しています…' : 'エージェントが読んでいます…'}</div>`
    : (turns.length ? '' : '<div class="empty">まだ何も訊いていません。</div>'));
  clearInterval(ticking);
  if (pending === 'read') tick(Date.now());
  $('#ask-auto-state').innerHTML = auto
    ? '<span class="hint">この会話では SQL を自動で実行しています</span> <button type="button" id="ask-auto-off" class="btn small">やめる</button>'
    : '';
  $('#ask-auto-off')?.addEventListener('click', () => { setAuto(false); draw(); });
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
  const held = auto ? heldBecause() : '';
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
        ${auto ? '' : '<label class="check"><input type="checkbox" id="ask-auto"> この会話では、以後の SQL も確かめずに実行する</label>'}
        <button type="button" id="ask-run" class="btn primary">実行して結果を返す</button>
      </div>` : ''}
      ${held ? `<div class="hint">${esc(held)}</div>` : ''}
    </div>`;
}

// autoRuns counts the queries run since the person last wrote something
// of their own.
function autoRuns() {
  let n = 0;
  for (let i = turns.length - 1; i >= 0; i--) {
    const t = turns[i];
    if (t.role !== 'user') continue;
    if (!t.sqlResult && !t.sqlFailed) break;
    n++;
  }
  return n;
}

function project() {
  if (AGENT_PROJECT) return AGENT_PROJECT;
  try { return localStorage.getItem(PROJECT_KEY) || ''; } catch { return ''; }
}

// heldBecause says why a proposal waits for a click although the person
// agreed to automatic runs, or '' when the page may run it by itself.
function heldBecause() {
  if (!AGENT_CLIENT && !PROXY_RUNS) return 'このデプロイでは、このページから実行できません。';
  if (!project()) return '課金するプロジェクトを入れて、一度実行してください。';
  if (autoRuns() >= MAX_AUTO_RUNS) return `続けて ${MAX_AUTO_RUNS} 回実行したので止まっています。続けるなら実行するか、何か書いてください。`;
  if (!PROXY_RUNS && !hasToken()) return 'Google のサインインが切れました。実行するとサインインし直します。';
  return '';
}

// autoRun runs the newest proposal where the person agreed to it and
// nothing holds it back.
async function autoRun() {
  const t = turns.at(-1);
  if (!auto || !t || t.role !== 'agent' || !t.sql || heldBecause()) return;
  await execute(t.sql.query, project());
}

// execute runs one query and hands what came back to the agent. A query
// BigQuery refused goes back too, as the person's next message: the
// agent reads the error and corrects the SQL. Only a sign-in that failed
// stops here — that is the page's problem, not the query's.
async function execute(query, proj) {
  const tok = PROXY_RUNS ? null : await signIn(AGENT_CLIENT);
  draw('run');
  try {
    const res = await run(tok, proj, query);
    turns.push({ role: 'user', text: asMessage(query, res), sqlResult: true, res: { query, ...res } });
  } catch (e) {
    turns.push({ role: 'user', text: asFailure(query, e), sqlFailed: true, error: e.message, query });
  }
  save();
  await answer();
}

// resultHTML draws what a query returned, or why it did not run. It is
// the page's line, not the person's words, so it is not drawn as theirs.
function resultHTML(t, i) {
  if (t.sqlFailed) {
    return `<div class="ask-turn ask-result"><div class="hint">実行できませんでした(エージェントに返しました)</div><pre><code>${esc(t.error)}</code></pre></div>`;
  }
  const r = t.res;
  const shown = r.rows.length < r.total ? `、先頭 ${r.rows.length} 行` : '';
  // Numbers line up on their last digit, the way a person compares them.
  const num = (r.types || []).map(t => NUMERIC_TYPES.has(t));
  const td = (tag, c, i) => `<${tag}${num[i] ? ' class="num"' : ''}>${esc(c)}</${tag}>`;
  const head = r.fields.map((f, i) => td('th', f, i)).join('');
  const body = r.rows.map(row => `<tr>${row.map((c, i) => td('td', c, i)).join('')}</tr>`).join('');
  return `<div class="ask-turn ask-result">
      <div class="hint">実行結果(${esc(fmtBytes(r.bytes))} 読み取り、全 ${r.total} 行${shown})</div>
      ${chartHTML(r)}
      <div class="ask-table"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>
    </div>`;
}

async function runProposal() {
  const query = $('#ask-sql').value.trim();
  const proj = AGENT_PROJECT || $('#ask-project').value.trim();
  if (!query || !proj) { toast('SQL と課金するプロジェクトが要ります'); return; }
  if (!AGENT_PROJECT) {
    try { localStorage.setItem(PROJECT_KEY, proj); } catch { /* remembered for this run only */ }
  }
  if ($('#ask-auto')?.checked) setAuto(true);
  const btn = $('#ask-run');
  btn.disabled = true;
  btn.textContent = '実行しています…';
  try {
    await execute(query, proj);
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

// draftLine names what the agent wrote, where a person rules on it: each
// is a draft nobody has confirmed, and the review queue is where it waits.
function draftLine(ids) {
  if (!ids || !ids.length) return '';
  const links = ids.map(id => `<a href="${esc(entryHash({ id }))}"><code>${esc(id)}</code></a>`).join(' ');
  return `<div class="hint">書いた下書き(<a href="#/review">レビュー</a>で裁定を待っています): ${links}</div>`;
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
  draw('read');
  try {
    const ans = await api('/api/v1/agent', {
      method: 'POST',
      // A proposal travels back as part of the agent's own message, so
      // the model reads what it asked for beside what came back. Older
      // results are folded where the whole would not fit; the page keeps
      // showing them in full.
      body: {
        messages: fold(turns.map(t => ({
          role: t.role,
          text: t.sql ? `${t.text}\n\n\`\`\`sql\n${t.sql.query}\n\`\`\`` : t.text,
          result: !!t.sqlResult,
        }))).map(({ role, text }) => ({ role, text })),
      },
    });
    turns.push({ role: 'agent', text: ans.text, read: ans.read || [], drafts: ans.drafts || [], sql: ans.sql || null, turn: ans.turn || '' });
    save();
    draw();
    // Not awaited: the answer is in, and a sign-in that fails while the
    // page runs the next query by itself is said where it happened.
    autoRun().catch(e => { toast('実行できませんでした: ' + e.message, 8000); draw(); });
    return true;
  } catch (e) {
    turns.pop();
    save();
    toast('答えられませんでした: ' + e.message, 6000);
    draw();
    return false;
  }
}
