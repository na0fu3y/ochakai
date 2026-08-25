// The values a reader sees, and the paths a click follows.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  actorStr, conceptURL, crumbTrail, daysSince, dirHash, displayTitle,
  entryHash, fmtAge, fmtSize, headingHash, isVerified, lastVerification,
  parseKPath, trustOf,
} from '../static/js/format.js';
import { esc } from '../static/js/escape.js';

test('an id keeps its slashes and encodes everything else', () => {
  // The id is a path (design doc 0016), so its separators are real; a
  // segment with a space or a hash is not.
  assert.equal(entryHash({ id: 'metrics/revenue' }), '#/k/metrics/revenue');
  assert.equal(entryHash({ id: 'a b/c#d' }), '#/k/a%20b/c%23d');
  assert.equal(conceptURL('metrics/revenue'), '/api/v1/bundle/metrics/revenue.md');
});

test('a heading link carries the concept and the section together', () => {
  assert.equal(headingHash('metrics/revenue', '定義'), '#/k/metrics/revenue?h=%E5%AE%9A%E7%BE%A9');
  // And comes back apart the same way.
  assert.deepEqual(parseKPath('metrics/revenue?h=%E5%AE%9A%E7%BE%A9'),
    { id: 'metrics/revenue', heading: '定義' });
});

test('a route with no heading is the concept alone', () => {
  assert.deepEqual(parseKPath('metrics/revenue'), { id: 'metrics/revenue', heading: '' });
  assert.deepEqual(parseKPath(''), { id: '', heading: '' });
});

test('the id is decoded segment by segment, after the heading is cut off', () => {
  // An encoded slash inside a segment is part of the segment, not a
  // separator — the same rule idPath writes with.
  assert.deepEqual(parseKPath('a%20b/c%23d?h=x'), { id: 'a b/c#d', heading: 'x' });
  // A "?h=" a writer put in a segment travels encoded, so the first one
  // in the raw path really is the route's own.
  const raw = 'a/' + encodeURIComponent('q?h=1');
  assert.deepEqual(parseKPath(raw), { id: 'a/q?h=1', heading: '' });
});

test('a malformed address is a bad address, not an exception', () => {
  // A truncated or hand-typed escape used to throw URIError out of the
  // router, which left the previous concept on screen under the new
  // address. The raw text finds nothing, which is what happened.
  assert.deepEqual(parseKPath('metrics/revenue?h=%zz'), { id: 'metrics/revenue', heading: '%zz' });
  assert.deepEqual(parseKPath('metrics/%E3%81'), { id: 'metrics/%E3%81', heading: '' });
});

test('a directory hash tolerates a trailing slash', () => {
  assert.equal(dirHash('insights/q1'), '#/dir/insights/q1');
  assert.equal(dirHash('insights/q1/'), '#/dir/insights/q1');
  assert.equal(dirHash(''), '#/dir/');
});

test('a concept without a title is named by its last segment', () => {
  assert.equal(displayTitle({ id: 'metrics/revenue', title: 'Revenue' }), 'Revenue');
  assert.equal(displayTitle({ id: 'metrics/revenue' }), 'revenue');
  assert.equal(displayTitle({ id: 'metrics/revenue', title: '' }), 'revenue');
});

test('a breadcrumb links every directory and escapes every name', () => {
  assert.equal(crumbTrail([]), '<a href="#/" title="ルートの索引を開きます">/</a>');
  assert.match(crumbTrail(['a', 'b']), /<a href="#\/dir\/a">a\/<\/a><a href="#\/dir\/a\/b">b\/<\/a>/);
  // The leaf is the current page's own name, as text.
  assert.match(crumbTrail(['a'], '<x>'), /&lt;x&gt;$/);
});

test('an actor never loses who it was acting through', () => {
  // A person writing through an embedded application must not read like
  // one who wrote it themselves (design doc 0027).
  assert.equal(actorStr({ kind: 'human', name: 'a@b' }), '👤 a@b');
  assert.equal(actorStr({ kind: 'process', name: 'sa@p' }), '🤖 sa@p');
  assert.equal(actorStr({ kind: 'process', name: 'sa@p', via: 'a@b', producer: 'claude/1' }),
    '🤖 sa@p(a@b 経由・claude/1 使用)');
  assert.equal(actorStr({ kind: 'process', name: 'sa@p', via: 'a@b' }), '🤖 sa@p(a@b 経由)');
  assert.equal(actorStr({ kind: 'process', name: 'sa@p', producer: 'claude/1' }), '🤖 sa@p(claude/1 使用)');
  assert.equal(actorStr(null), '');
  assert.equal(actorStr({ kind: 'human' }), '');
});

test('trust defaults to unverified, and the newest verification wins', () => {
  assert.equal(trustOf({}), 'unverified');
  assert.equal(isVerified({}), false);
  assert.equal(isVerified({ trust: 'human-reviewed' }), true);
  assert.equal(lastVerification(null), null);
  assert.equal(lastVerification({ verified: [] }), null);
  // Stored oldest-first, so the last one is the newest (design doc 0043).
  assert.deepEqual(lastVerification({ verified: [{ at: '1' }, { at: '2' }] }), { at: '2' });
});

test('an age reads as a person would say it', () => {
  assert.equal(fmtAge(null), '');
  assert.equal(fmtAge(0), '今日');
  assert.equal(fmtAge(1), '昨日');
  assert.equal(fmtAge(9), '9 日前');
  assert.equal(daysSince(null), null);
  assert.equal(daysSince('not a date'), null);
  assert.equal(daysSince(new Date(Date.now() - 3 * 86400000).toISOString()), 3);
});

test('a size reads in the unit it deserves', () => {
  assert.equal(fmtSize(undefined), '');
  assert.equal(fmtSize(0), '0 B');
  assert.equal(fmtSize(1023), '1023 B');
  assert.equal(fmtSize(2048), '2 KB');
  assert.equal(fmtSize(3 * 1024 * 1024), '3.0 MB');
});

test('escaping covers every character that could close a tag or an attribute', () => {
  assert.equal(esc(`<&>"'`), '&lt;&amp;&gt;&quot;&#39;');
  assert.equal(esc(null), '');
  assert.equal(esc(0), '0');
});
