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

import { api, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { entryHash } from '../format.js';
import { md } from '../markdown.js';

// The server refuses a conversation longer than this (internal/agent).
// Said here so the page can offer a fresh start before the refusal, not
// after it.
const MAX_TURNS = 40;
const KEY = 'ochakai.ask';

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
  const out = turns.map(t => t.role === 'user'
    ? `<div class="ask-turn ask-user">${esc(t.text).replace(/\n/g, '<br>')}</div>`
    : `<div class="card ask-turn ask-agent">${md(t.text)}${readLine(t.read)}</div>`).join('');
  $('#ask-turns').innerHTML = out + (pending
    ? '<div class="empty">エージェントが読んでいます…</div>'
    : (turns.length ? '' : '<div class="empty">まだ何も訊いていません。</div>'));
  const left = MAX_TURNS - turns.length;
  $('#ask-count').textContent = left <= 6 ? `この会話はあと ${Math.max(0, Math.floor(left / 2))} 往復まで` : '';
  $('#ask-send').disabled = !!pending || left < 1;
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
  draw(true);
  try {
    const ans = await api('/api/v1/agent', {
      method: 'POST',
      body: { messages: turns.map(({ role, text }) => ({ role, text })) },
    });
    turns.push({ role: 'agent', text: ans.text, read: ans.read || [] });
    save();
  } catch (e) {
    // The question goes back into the box rather than staying in the
    // conversation: left there, the next send would carry it as history
    // the agent never answered, and the question would read as asked
    // twice.
    turns.pop();
    box.value = text;
    toast('答えられませんでした: ' + e.message, 6000);
  }
  draw();
}
