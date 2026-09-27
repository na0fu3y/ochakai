// The failed reports a reader sees before trusting a concept.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { failedNoteHTML, failedSince } from '../static/js/reports.js';

const by = { kind: 'human', name: 'tanaka@example.co.jp' };
const reports = [
  { outcome: 'failed', at: '2026-09-27T10:00:00Z', by, note: '2024年の数字が合わない' },
  { outcome: 'worked', at: '2026-09-26T10:00:00Z', by, note: '合った' },
  { outcome: 'failed', at: '2026-09-20T10:00:00Z', by, note: '古い失敗' },
];

test('only failures after the last verification are open', () => {
  assert.deepEqual(failedSince(reports, '2026-09-25T00:00:00Z').map(r => r.note), ['2024年の数字が合わない']);
  assert.deepEqual(failedSince(reports, '2026-09-28T00:00:00Z'), []);
});

test('with no verification every failure is open', () => {
  assert.equal(failedSince(reports, undefined).length, 2);
  assert.equal(failedSince(undefined, undefined).length, 0);
});

test('the note leads, escaped, with how many more there are', () => {
  const html = failedNoteHTML(failedSince(reports, null), '失敗の報告: ');
  assert.match(html, /「2024年の数字が合わない」/);
  assert.match(html, /ほか 1 件/);
  assert.equal(failedNoteHTML([], 'x'), '');
  assert.match(failedNoteHTML([{ ...reports[0], note: '<b>' }], ''), /&lt;b&gt;/);
});
