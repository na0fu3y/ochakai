// The access policy's round trip: the rules the server sends, the
// document an operator edits, and the rules that go back (design doc
// 0109 §5). Every bug here is a bug in a boundary somebody is drawing,
// and the two directions are held together because a document that does
// not parse back to what it rendered from is a policy nobody can review.
//
// Outside static/, so it is not embedded and not served.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  parsePolicyDocument, policyDocument, validPrincipal,
} from '../static/js/policy.js';

const RULES = [
  { prefix: '', principal: 'human:na0@example.co.jp', may_write: true, granted_at: '2026-08-17T00:00:00Z', granted_by: 'human:na0@example.co.jp' },
  { prefix: 'sales', principal: '*', may_write: false },
  { prefix: 'sales/sample', principal: 'human:tanaka@example.co.jp', may_write: true },
];

test('the document parses back to the rules it was rendered from', () => {
  const back = parsePolicyDocument(policyDocument(RULES));
  assert.deepEqual(back, [
    { prefix: '', principal: 'human:na0@example.co.jp', may_write: true },
    { prefix: 'sales', principal: '*', may_write: false },
    { prefix: 'sales/sample', principal: 'human:tanaka@example.co.jp', may_write: true },
  ]);
});

// The server records these and ignores them on write. A document that
// carried them would invite an edit that silently does nothing — the
// worst answer a boundary editor can give.
test('the document leaves out what the server records', () => {
  const text = policyDocument(RULES);
  assert.doesNotMatch(text, /granted_at/);
  assert.doesNotMatch(text, /granted_by/);
});

test('an empty policy is a document, not an empty box', () => {
  assert.deepEqual(parsePolicyDocument(policyDocument([])), []);
  assert.match(policyDocument([]), /"rules": \[\]/);
});

test('a prefix is trimmed the way the server trims it', () => {
  const [rule] = parsePolicyDocument('{"rules":[{"prefix":" /sales/orders/ ","principal":"*","may_write":false}]}');
  assert.equal(rule.prefix, 'sales/orders');
});

// "sales/" and "sales" are one grant once the server has them, so the
// duplicate has to be caught against the row as it will be kept.
test('two rows for one grant are refused, however they are spelled', () => {
  assert.throws(() => parsePolicyDocument(
    '{"rules":[{"prefix":"sales","principal":"*"},{"prefix":"sales/","principal":"*"}]}'),
  /二行/);
});

test('a nested prefix is not a duplicate of the one above it', () => {
  const rules = parsePolicyDocument(
    '{"rules":[{"prefix":"sales","principal":"*"},{"prefix":"sales/sample","principal":"*"}]}');
  assert.equal(rules.length, 2);
});

test('may_write defaults to false and refuses anything but a boolean', () => {
  const [rule] = parsePolicyDocument('{"rules":[{"prefix":"sales","principal":"*"}]}');
  assert.equal(rule.may_write, false);
  assert.throws(() => parsePolicyDocument('{"rules":[{"prefix":"s","principal":"*","may_write":"yes"}]}'),
    /may_write/);
});

// Which row is wrong is the whole reason this check exists in the page
// at all: the server validates it again, but the textarea is the thing
// with rows in it.
test('a bad row is named by its position', () => {
  assert.throws(() => parsePolicyDocument(
    '{"rules":[{"prefix":"a","principal":"*"},{"prefix":"b","principal":"tanaka@example.co.jp"}]}'),
  /^Error: 2 番目の principal/);
});

test('the shapes that are not a policy say so', () => {
  assert.throws(() => parsePolicyDocument('{'), /JSON として読めません/);
  assert.throws(() => parsePolicyDocument('[]'), /rules/);
  assert.throws(() => parsePolicyDocument('{"rules":{}}'), /rules/);
  assert.throws(() => parsePolicyDocument('{"rules":[null]}'), /1 番目/);
  assert.throws(() => parsePolicyDocument('{"rules":[{"prefix":2,"principal":"*"}]}'), /prefix/);
});

test('a principal is the wildcard or an actor the ledger can spell', () => {
  for (const ok of ['*', 'human:na0@example.co.jp', 'process:app@x.iam.gserviceaccount.com']) {
    assert.equal(validPrincipal(ok), true, ok);
  }
  for (const no of ['', 'na0@example.co.jp', 'human:', ':name', 'agent:x', 'human:a b', '**']) {
    assert.equal(validPrincipal(no), false, no);
  }
});
