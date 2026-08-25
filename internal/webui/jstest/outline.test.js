// The outline: ids on rendered headings, and the table of contents
// drawn from them.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { headingAnchor, headingAnchors, TOC_MIN, tocHTML } from '../static/js/outline.js';
import { md } from '../static/js/markdown.js';

test('an anchor is the heading in the URL, Japanese included', () => {
  assert.equal(headingAnchor('検証の順番'), '検証の順番');
  assert.equal(headingAnchor('How To Read'), 'how-to-read');
  // esc() wrote entities; the anchor holds the character they meant,
  // minus the punctuation that cannot travel in a fragment.
  assert.equal(headingAnchor('A &amp; B'), 'a--b');
});

test('every heading in a rendered body gains an id', () => {
  const { html, headings } = headingAnchors(md('# 定義\n\n## 検証の順番\n\nprose'));
  assert.match(html, /<h1 id="定義">定義<\/h1>/);
  assert.match(html, /<h2 id="検証の順番">検証の順番<\/h2>/);
  assert.deepEqual(headings.map(h => [h.level, h.id]), [[1, '定義'], [2, '検証の順番']]);
});

test('two sections with one name stay two anchors', () => {
  const { headings } = headingAnchors('<h2>例</h2><h2>例</h2><h2>例</h2>');
  assert.deepEqual(headings.map(h => h.id), ['例', '例-2', '例-3']);
});

test('a heading a writer typed as markup cannot match', () => {
  // md() escapes prose, so a literal <h2> arrives as entities and the
  // outline sees no heading in it.
  const { headings } = headingAnchors(md('prose with <h2>fake</h2> inside'));
  assert.deepEqual(headings, []);
});

test('inline markup stays in the heading and out of the anchor', () => {
  const { html, headings } = headingAnchors(md('## `sql` の読み方'));
  assert.match(html, /<h2 id="sql-の読み方"><code>sql<\/code> の読み方<\/h2>/);
  assert.equal(headings[0].text, 'sql の読み方');
});

test('the outline waits for a document long enough to need one', () => {
  const two = [{ level: 2, text: 'a', id: 'a' }, { level: 2, text: 'b', id: 'b' }];
  assert.equal(tocHTML(two), '');
  assert.ok(two.length < TOC_MIN);
  const three = [...two, { level: 3, text: 'c', id: 'c' }];
  const html = tocHTML(three);
  assert.match(html, /<summary>目次<\/summary>/);
  // Levels indent relative to the shallowest heading present.
  assert.match(html, /<li class="lv0"><a href="#a">a<\/a><\/li>/);
  assert.match(html, /<li class="lv1"><a href="#c">c<\/a><\/li>/);
});

test('the outline stops at h3', () => {
  const headings = [
    { level: 2, text: 'a', id: 'a' }, { level: 2, text: 'b', id: 'b' },
    { level: 3, text: 'c', id: 'c' }, { level: 4, text: 'd', id: 'd' },
  ];
  const html = tocHTML(headings);
  assert.ok(!html.includes('#d'));
});
