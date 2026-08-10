// The renderer, held to what it actually draws — including the things it
// does not, which the last test states rather than leaves to be
// discovered in a document somebody wrote.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { md, descHTML } from '../static/js/markdown.js';

test('headings, paragraphs and emphasis', () => {
  assert.equal(md('# 見出し'), '<h1>見出し</h1>');
  assert.equal(md('### 三段目'), '<h3>三段目</h3>');
  assert.equal(md('ふつうの段落'), '<p>ふつうの段落</p>');
  assert.equal(md('**強い** と *弱い*'), '<p><strong>強い</strong> と <em>弱い</em></p>');
});

test('a paragraph joins its lines, a blank line ends it', () => {
  assert.equal(md('一行目\n二行目\n\n次の段落'), '<p>一行目 二行目</p><p>次の段落</p>');
});

test('lists close when the kind changes and when the text stops', () => {
  assert.equal(md('- a\n- b'), '<ul><li>a</li><li>b</li></ul>');
  assert.equal(md('1. a\n2. b'), '<ol><li>a</li><li>b</li></ol>');
  assert.equal(md('- a\n1. b'), '<ul><li>a</li></ul><ol><li>b</li></ol>');
  assert.equal(md('- a\n\nあと'), '<ul><li>a</li></ul><p>あと</p>');
});

test('a fenced block is code, and its contents are not markup', () => {
  assert.equal(md('```\nSELECT 1\n```'), '<pre><code>SELECT 1\n</code></pre>');
  // Unterminated: the block still closes rather than swallowing the page.
  assert.match(md('```\nSELECT 1'), /<\/code><\/pre>$/);
  assert.match(md('```\n<b>x</b>\n```'), /&lt;b&gt;x&lt;\/b&gt;/);
});

test('markup shown inside a code span stays an example', () => {
  // The rule the server's link extraction follows, so what reads as a
  // link here is exactly what became an edge there (design doc 0024).
  assert.equal(md('`**not bold**`'), '<p><code>**not bold**</code></p>');
});

test('a GFM table becomes a table, with its alignment', () => {
  const html = md('| a | b |\n|:--|--:|\n| 1 | 2 |');
  assert.match(html, /<table><thead><tr><th style="text-align:left">a<\/th>/);
  assert.match(html, /<th style="text-align:right">b<\/th>/);
  assert.match(html, /<tbody><tr><td[^>]*>1<\/td><td[^>]*>2<\/td><\/tr><\/tbody>/);
});

test('a lone --- is prose, not a table delimiter', () => {
  assert.doesNotMatch(md('あ\n---\nい'), /<table>/);
});

test('everything interpolated is escaped', () => {
  // 31 innerHTML sinks read what this returns, so this is the boundary.
  assert.equal(md('<script>alert(1)</script>'),
    '<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>');
  assert.match(md('# <img onerror=x>'), /&lt;img onerror=x&gt;/);
  assert.match(md('| <b> |\n|---|---|\n| y |'), /&lt;b&gt;/);
});

test('an http link leaves the app; a bundle reference needs a resolver', () => {
  assert.equal(md('[x](https://example.com)'),
    '<p><a href="https://example.com" target="_blank" rel="noopener">x</a></p>');
  // Nothing resolves it, so it stays the text somebody wrote rather than
  // becoming a dead link.
  assert.equal(md('[x](/metrics/revenue.md)'), '<p>[x](/metrics/revenue.md)</p>');
  assert.equal(md('[x](/metrics/revenue.md)', null, () => '#/k/metrics/revenue'),
    '<p><a href="#/k/metrics/revenue">x</a></p>');
});

test('a link to a file resolves through the file resolver', () => {
  // Since design doc 0013 a bundle file may be any type, and a plain link
  // is the only notation that can reference a non-image one — the files
  // tab prints exactly this form for authors to paste. Consulting only
  // the entry resolver, which answers null for anything but a *.md, left
  // those references drawn as raw markdown.
  const asFile = ref => '/api/v1/bundle/m/' + ref;
  assert.equal(md('[csv](orders.csv)', asFile, () => null),
    '<p><a href="/api/v1/bundle/m/orders.csv" target="_blank" rel="noopener">csv</a></p>');
  // An entry wins over a file when both would resolve.
  assert.equal(md('[x](y.md)', asFile, () => '#/k/y'), '<p><a href="#/k/y">x</a></p>');
});

test('an image renders only when its reference resolves', () => {
  assert.equal(md('![alt](chart.png)'), '<p>![alt](chart.png)</p>');
  assert.equal(md('![alt](chart.png)', r => '/api/v1/bundle/m/' + r),
    '<p><img src="/api/v1/bundle/m/chart.png" alt="alt" loading="lazy"></p>');
});

test('a description is rendered like a body, keeping its line structure', () => {
  const html = descHTML('# 見出し\n\n本文', 'small');
  assert.equal(html, '<div class="desc md small"><h1>見出し</h1><p>本文</p></div>');
});

test('a description draws no bundle links', () => {
  // A reference in a description is not an edge the server derived from
  // the body, so drawing it as a link would show one that does not exist.
  assert.match(descHTML('[x](/metrics/revenue.md)'), /\[x\]\(\/metrics\/revenue\.md\)/);
});

test('headings go to six levels', () => {
  assert.equal(md('#### 四段目'), '<h4>四段目</h4>');
  assert.equal(md('###### 六段目'), '<h6>六段目</h6>');
  // Seven is not a heading in markdown either.
  assert.doesNotMatch(md('####### 七'), /<h7>|<h6>/);
});

test('a block quote is a quote', () => {
  assert.equal(md('> 引用'), '<blockquote><p>引用</p></blockquote>');
  // Consecutive lines are one quoted paragraph, as they are in prose.
  assert.equal(md('> 一行目\n> 二行目'), '<blockquote><p>一行目 二行目</p></blockquote>');
  assert.equal(md('> 引用\n\nあと'), '<blockquote><p>引用</p></blockquote><p>あと</p>');
  // The quote's own contents are still markdown, and still escaped.
  assert.match(md('> **強い** <b>'), /<strong>強い<\/strong> &lt;b&gt;/);
});

test('a nested list is a tree, not two lists in a row', () => {
  // The child list opens inside the item above it, so the markup says
  // what the writer said: these steps are under that step.
  assert.equal(md('- a\n  - b'), '<ul><li>a<ul><li>b</li></ul></li></ul>');
  assert.equal(md('- a\n  - b\n- c'), '<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>');
  assert.equal(md('1. a\n   - b'), '<ol><li>a<ul><li>b</li></ul></li></ol>');
  // Three deep, and back out in one step.
  assert.equal(md('- a\n  - b\n    - c\n- d'),
    '<ul><li>a<ul><li>b<ul><li>c</li></ul></li></ul></li><li>d</li></ul>');
});

// The notation the product tells writers to use. `sources[].id` exists
// so a footnote in the body can cite one of the concept's sources, and
// examples/demo is written that way — so this was the one gap where the
// server recommended something the bundled UI drew as characters.
test('a footnote is a footnote', () => {
  const html = md('主張。[^src]\n\n[^src]: 出典の名前');
  assert.match(html, /<sup class="fnref"><a id="fnref-src" href="#fn-src">1<\/a><\/sup>/);
  assert.match(html, /<section class="footnotes"><ol><li id="fn-src">出典の名前/);
  assert.match(html, /<a class="fnback" href="#fnref-src"/);
  // The definition line is not also drawn where it stands.
  assert.doesNotMatch(html, /<p>\[\^src\]/);
  assert.doesNotMatch(html, /\[\^src\]<\/p>/);
});

test('footnotes are numbered in the order the prose refers to them', () => {
  const html = md('一[^b] 二[^a]\n\n[^a]: あ\n[^b]: い');
  assert.match(html, /href="#fn-b">1</);
  assert.match(html, /href="#fn-a">2</);
  assert.match(html, /<li id="fn-b">い .*<li id="fn-a">あ /);
});

test('a footnote nothing defines stays the writer\'s text', () => {
  // The same rule the link and image notations follow: what cannot be
  // resolved is left as it was written, not drawn as a broken marker.
  assert.equal(md('主張。[^missing]'), '<p>主張。[^missing]</p>');
  // And a definition nothing refers to puts no number in the margin.
  assert.equal(md('本文。\n\n[^unused]: 誰も参照しない'), '<p>本文。</p>');
});

test('a footnote label cannot escape its own anchor', () => {
  // The label reaches an id and an href, so it is slugified rather than
  // trusted — the body is somebody else's text.
  const html = md('x[^a"b]\n\n[^a"b]: 出典');
  assert.doesNotMatch(html, /id="fn-a"b"/);
  assert.match(html, /id="fn-a-*b"|id="fn-a-quot-b"/);
});

// What it still does not draw, so the next person does not have to
// discover it in a document somebody wrote.
test('the notations this renderer does not know stay as text', () => {
  assert.doesNotMatch(md('term\n: definition'), /<dl>/);      // definition lists
  assert.doesNotMatch(md('- [ ] todo'), /<input/);             // task lists
  assert.doesNotMatch(md('~~gone~~'), /<del>/);                // strikethrough
  assert.doesNotMatch(md('---'), /<hr/);                       // thematic breaks
});
