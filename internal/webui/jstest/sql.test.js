// What a query's result carries back into the conversation.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { asFailure, asMessage, fold } from '../static/js/sql.js';

const bytes = msgs => msgs.reduce((n, m) => n + new TextEncoder().encode(m.text).length, 0);

function result(n) {
  const rows = Array.from({ length: 50 }, (_, i) => [`${'値'.repeat(n)}${i}`]);
  return asMessage('SELECT x FROM `p.d.t`', { fields: ['x'], rows, total: 50, bytes: 1024 });
}

test('a conversation that fits is sent as it is', () => {
  const msgs = [{ role: 'user', text: '売上は？' }, { role: 'agent', text: 'SQL を提案します' }, { role: 'user', text: result(1), result: true }];
  assert.deepEqual(fold(msgs), msgs);
});

test('the oldest results fold first, and the newest stays whole', () => {
  const msgs = [{ role: 'user', text: '売上は？' }];
  for (let i = 0; i < 6; i++) {
    msgs.push({ role: 'agent', text: `提案 ${i}` }, { role: 'user', text: result(200), result: true });
  }
  assert.ok(bytes(msgs) > 64 << 10, 'the fixture has to overflow to test anything');
  const out = fold(msgs);
  assert.ok(bytes(out) <= 64 << 10);
  assert.match(out[2].text, /省きました/);
  assert.match(out[2].text, /SELECT x FROM/, 'a folded result keeps the SQL that produced it');
  assert.equal(out.at(-1).text, msgs.at(-1).text);
  // Only as many fold as it takes.
  assert.equal(out.filter(m => /省きました/.test(m.text)).length < 5, true);
  // What the page shows is untouched.
  assert.doesNotMatch(msgs[2].text, /省きました/);
});

test('a person\'s own words are never folded', () => {
  const long = 'あ'.repeat(30000);
  const msgs = [{ role: 'user', text: long }, { role: 'agent', text: 'はい' }, { role: 'user', text: long }];
  assert.deepEqual(fold(msgs), msgs);
});

test('a query that failed goes back with its SQL and what BigQuery said', () => {
  const text = asFailure('SELECT y FROM `p.d.t`', new Error('Unrecognized name: y'));
  assert.match(text, /SELECT y FROM `p\.d\.t`/);
  assert.match(text, /Unrecognized name: y/);
});

test('a failure is never folded: it is short, and the agent corrects from it', () => {
  const msgs = [{ role: 'user', text: '売上は？' }, { role: 'agent', text: '提案' }, { role: 'user', text: asFailure('SELECT 1', new Error('x')) }];
  assert.deepEqual(fold(msgs, 1), msgs);
});
