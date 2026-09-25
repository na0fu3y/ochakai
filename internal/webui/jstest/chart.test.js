// When a result is drawn as a chart, and as which one.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { chartHTML, groups, plan, ticks } from '../static/js/chart.js';

const res = (fields, types, rows, total = rows.length) => ({ fields, types, rows, total, bytes: 0 });

test('a date and a number draw a line, oldest first', () => {
  const p = plan(res(['day', 'revenue'], ['DATE', 'INTEGER'], [['2026-09-02', '9800'], ['2026-09-01', '12300']]));
  assert.equal(p.kind, 'line');
  assert.deepEqual(p.rows.map(r => r.label), ['2026-09-01', '2026-09-02']);
  assert.deepEqual(p.series[0].values, [12300, 9800]);
});

test('a text label draws bars in the order the query returned', () => {
  const p = plan(res(['channel', 'n'], ['STRING', 'INT64'], [['web', '5'], ['app', '9']]));
  assert.equal(p.kind, 'bar');
  assert.deepEqual(p.rows.map(r => r.label), ['web', 'app']);
});

test('a shape a chart would misstate draws none', () => {
  // one row
  assert.equal(plan(res(['d', 'n'], ['DATE', 'INTEGER'], [['2026-09-01', '1']])), null);
  // a second label column: a chart would silently sum or overlap it
  assert.equal(plan(res(['d', 'ch', 'n'], ['DATE', 'STRING', 'INTEGER'], [['2026-09-01', 'a', '1'], ['2026-09-02', 'b', '2']])), null);
  // a numeric first column is not a label
  assert.equal(plan(res(['year', 'n'], ['INTEGER', 'INTEGER'], [['2025', '1'], ['2026', '2']])), null);
  // more series than colours
  assert.equal(plan(res(['d', 'a', 'b', 'c', 'e', 'f'], ['DATE', 'INTEGER', 'INTEGER', 'INTEGER', 'INTEGER', 'INTEGER'],
    [['2026-09-01', '1', '1', '1', '1', '1'], ['2026-09-02', '1', '1', '1', '1', '1']])), null);
  // too many bars to read
  const many = Array.from({ length: 21 }, (_, i) => [`k${i}`, String(i)]);
  assert.equal(plan(res(['k', 'n'], ['STRING', 'INTEGER'], many)), null);
  assert.equal(chartHTML(res(['k'], ['STRING'], [['a'], ['b']]), 0), '');
});

test('series on different scales get a chart each, never a second axis', () => {
  const s = [{ name: 'orders', values: [10, 12] }, { name: 'revenue', values: [120000, 130000] }];
  assert.equal(groups(s).length, 2);
  assert.equal(groups([{ name: 'a', values: [10, 12] }, { name: 'b', values: [15, 30] }]).length, 1);
});

test('ticks are round and cover the range', () => {
  assert.deepEqual(ticks(0, 12300), [0, 5000, 10000, 15000]);
  const t = ticks(-3, 7);
  assert.ok(t[0] <= -3 && t.at(-1) >= 7);
});

test('a chart of the first rows says it is only the first rows', () => {
  const html = chartHTML(res(['d', 'n'], ['DATE', 'INTEGER'], [['2026-09-01', '1'], ['2026-09-02', '2']], 80), 3);
  assert.match(html, /先頭 2 行/);
  assert.match(html, /data-key="3"/);
});
