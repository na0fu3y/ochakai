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
  joinPrincipal, parsePolicyDocument, policyDocument, splitPrincipal,
  validPrincipal, validateRules,
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

test('may_admin implies may_write, and the root refuses it', () => {
  // The page sends what the server would infer anyway, so a rule read
  // back matches the one that was typed (design doc 0124).
  const [rule] = parsePolicyDocument(
    '{"rules":[{"prefix":"teams/growth","principal":"human:lead@example.co.jp","may_admin":true}]}');
  assert.equal(rule.may_admin, true);
  assert.equal(rule.may_write, true, 'may_admin implies may_write');

  // Who may edit the whole policy is the one answer the policy cannot
  // carry, and the refusal names where it lives instead.
  assert.throws(
    () => parsePolicyDocument('{"rules":[{"prefix":"","principal":"human:x@example.co.jp","may_admin":true}]}'),
    /OCHAKAI_ADMINS/);
  assert.throws(
    () => parsePolicyDocument('{"rules":[{"prefix":"a","principal":"*","may_admin":"yes"}]}'),
    /may_admin/);
});

test('a policy that delegates nothing reads as it did before may_admin', () => {
  const doc = policyDocument([{ prefix: 'sales', principal: '*', may_write: false }]);
  assert.equal(doc.includes('may_admin'), false);
  const withAdmin = policyDocument([
    { prefix: 'sales', principal: 'human:lead@example.co.jp', may_write: true, may_admin: true }]);
  assert.equal(withAdmin.includes('"may_admin": true'), true);
});

// The row editor holds a principal as two controls and the wire carries
// it as one string, so the split is only safe if it puts back what it
// took apart — a grant that changed spelling on the way through the form
// would be a boundary nobody drew.
test('a principal survives being split into the two controls and back', () => {
  for (const p of ['*', 'human:na0@example.co.jp', 'process:app@x.iam.gserviceaccount.com']) {
    const { kind, name } = splitPrincipal(p);
    assert.equal(joinPrincipal(kind, name), p, p);
  }
});

// Nothing the server stored can look like this. It arrives as a name
// under the first kind rather than being dropped, so the row shows what
// is there — and the save still refuses it.
test('a principal the ledger cannot spell keeps its text and is still refused', () => {
  const { kind, name } = splitPrincipal('tanaka@example.co.jp');
  assert.equal(name, 'tanaka@example.co.jp');
  assert.equal(kind, 'human');
  assert.throws(() => validateRules([{ prefix: 'a', principal: 'tanaka@example.co.jp' }]), /principal/);
});

// The rows and the document go through one check, so neither shape can
// send what the other would have refused.
test('the rows the form builds are checked exactly as a pasted document is', () => {
  const rows = [{ prefix: ' /sales/ ', principal: joinPrincipal('human', ' tanaka@example.co.jp '), may_write: true }];
  assert.deepEqual(validateRules(rows), parsePolicyDocument(policyDocument(validateRules(rows))));
  assert.deepEqual(validateRules(rows), [
    { prefix: 'sales', principal: 'human:tanaka@example.co.jp', may_write: true }]);
});

// The form can point at the row it is about; a document can only say it.
// The position rides on the error so that both surfaces get their own
// half of the same sentence.
test('a refusal carries the row it is about', () => {
  const bad = [{ prefix: 'a', principal: '*' }, { prefix: 'b', principal: 'nobody' }];
  assert.throws(() => validateRules(bad), e => e.row === 2 && /2 番目/.test(e.message));
});
