// What a revision changed, as the history tab tells it.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { diffHTML, diffLines, diffStats } from '../static/js/diff.js';

const ops = (a, b) => diffLines(a, b).map(o => o.op + ':' + o.text);

test('an edit in the middle leaves the rest alone', () => {
  assert.deepEqual(ops('a\nb\nc', 'a\nx\nc'), ['same:a', 'del:b', 'add:x', 'same:c']);
});

test('an added and a removed line are told apart', () => {
  assert.deepEqual(ops('a\nb', 'a\nb\nc'), ['same:a', 'same:b', 'add:c']);
  assert.deepEqual(ops('a\nb\nc', 'a\nc'), ['same:a', 'del:b', 'same:c']);
});

test('the first revision is all additions', () => {
  // The oldest revision has nothing before it; its diff is the document,
  // with no phantom deletion for the empty side.
  assert.deepEqual(ops('', 'a\nb'), ['add:a', 'add:b']);
  assert.deepEqual(diffStats(diffLines('', 'a\nb')), { added: 2, removed: 0 });
});

test('identical documents are no change at all', () => {
  assert.deepEqual(ops('a\nb', 'a\nb'), ['same:a', 'same:b']);
  assert.equal(diffHTML('a\nb', 'a\nb'), '');
});

test('a moved block is an alignment, not a rewrite', () => {
  // The LCS keeps the longest run: b/c stay, a moves.
  assert.deepEqual(ops('a\nb\nc', 'b\nc\na'), ['del:a', 'same:b', 'same:c', 'add:a']);
});

test('stats count the changed lines only', () => {
  assert.deepEqual(diffStats(diffLines('a\nb\nc', 'a\nx\ny\nc')), { added: 2, removed: 1 });
});

test('the rendering keeps context and elides the rest', () => {
  const before = ['head', '1', '2', '3', '4', '5', '6', '7', '8', 'tail'].join('\n');
  const after = ['head', '1', '2', '3', '4', '5', '6', '7', '8', 'TAIL'].join('\n');
  const html = diffHTML(before, after, 2);
  // The change and its two context lines are there; the head is elided
  // with a count, so a reader knows how much sits between.
  assert.match(html, /<span class="ln del">- tail<\/span>/);
  assert.match(html, /<span class="ln add">\+ TAIL<\/span>/);
  assert.match(html, /<span class="ln same"> {2}8<\/span>/);
  assert.match(html, /<span class="ln skip">⋯ 7 行<\/span>/);
  assert.ok(!html.includes('head'));
});

test('what a writer typed arrives escaped, never as markup', () => {
  const html = diffHTML('x', '<img src=x onerror=alert(1)>');
  assert.ok(!html.includes('<img'));
  assert.match(html, /&lt;img/);
});

test('a rewrite past the alignment budget is stated as a replacement', () => {
  const before = Array.from({ length: 600 }, (_, i) => 'a' + i).join('\n');
  const after = Array.from({ length: 600 }, (_, i) => 'b' + i).join('\n');
  const d = diffLines(before, after);
  assert.equal(diffStats(d).added, 600);
  assert.equal(diffStats(d).removed, 600);
});
