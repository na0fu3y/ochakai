// Marking the query's words in the results: the string half, which is
// where the matching decisions live.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { segments, terms } from '../static/js/highlight.js';

test('a query is its words, longest first', () => {
  assert.deepEqual(terms('revenue monthly'), ['revenue', 'monthly']);
  // An ideographic space separates words as surely as an ASCII one.
  assert.deepEqual(terms('売上　定義'), ['売上', '定義']);
  assert.deepEqual(terms('  '), []);
  // Repeats are one term, and the longer of two sorts first.
  assert.deepEqual(terms('sql sql select'), ['select', 'sql']);
});

test('matching ignores case and needs no word boundary', () => {
  assert.deepEqual(segments('Monthly Revenue', terms('revenue')),
    [{ text: 'Monthly ', hit: false }, { text: 'Revenue', hit: true }]);
  // Japanese has no boundaries to find; a substring is the whole rule.
  assert.deepEqual(segments('月次の売上の定義', terms('売上')),
    [{ text: '月次の', hit: false }, { text: '売上', hit: true }, { text: 'の定義', hit: false }]);
});

test('every occurrence marks, not only the first', () => {
  const parts = segments('sql, more sql', terms('sql'));
  assert.equal(parts.filter(p => p.hit).length, 2);
  assert.equal(parts.map(p => p.text).join(''), 'sql, more sql');
});

test('overlapping terms merge into one mark', () => {
  // "revenue" and "venue" overlap; the reader sees one run, not a mark
  // inside a mark.
  const parts = segments('the revenue', terms('revenue venue'));
  assert.deepEqual(parts, [{ text: 'the ', hit: false }, { text: 'revenue', hit: true }]);
});

test('adjacent terms merge too', () => {
  assert.deepEqual(segments('abcd', terms('ab cd')), [{ text: 'abcd', hit: true }]);
});

test('a query with no match leaves the text in one piece', () => {
  assert.deepEqual(segments('nothing here', terms('sql')), [{ text: 'nothing here', hit: false }]);
  assert.deepEqual(segments('nothing here', []), [{ text: 'nothing here', hit: false }]);
  assert.deepEqual(segments('', terms('sql')), []);
});

test('a character that grows when lowercased does not shift the marks', () => {
  // U+0130 lowercases to two code units, so offsets found in the folded
  // text used to cut the original one character late: the mark landed on
  // "İs" + "tanbul" instead of on the word that matched, and enough of
  // them left a <mark> holding nothing at all.
  assert.deepEqual(segments('İstanbul', terms('stanbul')),
    [{ text: 'İ', hit: false }, { text: 'stanbul', hit: true }]);
  const parts = segments('İİİx', terms('x'));
  assert.deepEqual(parts, [{ text: 'İİİ', hit: false }, { text: 'x', hit: true }]);
  assert.ok(parts.every(p => p.text));
});

test('a regular expression in the query is text, not a pattern', () => {
  // Building a pattern out of user input is the bug this avoids: `.*`
  // matches itself here and nothing else.
  assert.deepEqual(segments('a.*b and ab', terms('.*')),
    [{ text: 'a', hit: false }, { text: '.*', hit: true }, { text: 'b and ab', hit: false }]);
});
