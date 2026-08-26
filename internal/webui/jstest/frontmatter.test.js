// The fields the frontmatter is edited through. Held to examples for the
// reason documents.test.js is: what this returns becomes a key in a
// document somebody wrote.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  FIELDS, fieldsMarkup, listItemMarkup, otherKeys, rowMarkup, valueOf,
} from '../static/js/frontmatter.js';

const field = key => FIELDS.find(f => f.key === key);

test('every key OKF asks a writer for has a field, and no key it does not', () => {
  const keys = FIELDS.map(f => f.key);
  // SPEC's recommended keys (§4.1), provenance (§5.1), lifecycle
  // (§5.4-§5.5) and the Attested Computation contract (§10.2).
  for (const k of [
    'type', 'title', 'description', 'resource', 'tags',
    'sources', 'usage_window', 'status', 'stale_after',
    'runtime', 'parameters', 'computation', 'executor', 'attester',
  ]) {
    assert.ok(keys.includes(k), `${k} has no field`);
  }
  // What this instance observes is not a field: provenance comes from
  // the caller's identity, and the write path ignores these (design doc
  // 0009). Offering them would be offering a box nothing reads.
  for (const k of ['generated', 'verified', 'created_by', 'rejected_by', 'okf_version']) {
    assert.ok(!keys.includes(k), `${k} is editable, and it is not the writer's`);
  }
});

test('a cleared field removes the key rather than storing an empty one', () => {
  // ochakai does not write a key its writer left out (design doc 0046
  // §3.9), so "" has to mean absent — otherwise clearing a title would
  // store one that is empty and nothing would say so.
  assert.equal(valueOf(field('title'), ''), undefined);
  assert.equal(valueOf(field('title'), '   '), undefined);
  assert.equal(valueOf(field('tags'), ['', ' ']), undefined);
  assert.equal(valueOf(field('usage_window'), { from: '', to: '' }), undefined);
  assert.equal(valueOf(field('sources'), [{ resource: '', title: '' }]), undefined);
});

test('a value that is there comes back as the shape the document takes', () => {
  assert.equal(valueOf(field('title'), ' Revenue '), 'Revenue');
  assert.deepEqual(valueOf(field('tags'), ['sales', '', ' daily ']), ['sales', 'daily']);
  assert.deepEqual(valueOf(field('usage_window'), { from: '2026-01-01', to: '' }), { from: '2026-01-01' });
  assert.deepEqual(
    valueOf(field('sources'), [{ resource: 'bigquery://p/d/t', title: 'Orders', id: '' }]),
    [{ resource: 'bigquery://p/d/t', title: 'Orders' }],
  );
});

test('a count is a number, and something that is not one is kept as written', () => {
  assert.deepEqual(valueOf(field('sources'), [{ resource: 'x', usage_count: '12' }]),
    [{ resource: 'x', usage_count: 12 }]);
  // The server stores what the writer wrote; a form that turned this
  // into NaN would be deciding for them.
  assert.deepEqual(valueOf(field('sources'), [{ resource: 'x', usage_count: 'たくさん' }]),
    [{ resource: 'x', usage_count: 'たくさん' }]);
});

test('required is written only when it is true', () => {
  // SPEC §10.2: absent and false say the same thing, so `required:
  // false` would be a line the writer did not ask for.
  assert.deepEqual(valueOf(field('parameters'), [{ name: 'year', type: 'integer', required: true }]),
    [{ name: 'year', type: 'integer', required: true }]);
  assert.deepEqual(valueOf(field('parameters'), [{ name: 'year', type: 'integer', required: false }]),
    [{ name: 'year', type: 'integer' }]);
  // A row with nothing but an unticked box is not a parameter.
  assert.equal(valueOf(field('parameters'), [{ name: '', type: '', required: false }]), undefined);
});

test('a list inside an object is a list', () => {
  assert.deepEqual(
    valueOf(field('executor'), { resource: 'runbooks/revenue.md', receipt: ['rows', '', 'bytes'] }),
    { resource: 'runbooks/revenue.md', receipt: ['rows', 'bytes'] },
  );
});

test('the keys the form has no editor for are named, not hidden', () => {
  // A producer's key survives the form (SPEC §4.1), and a writer has no
  // way to know that unless the page says so.
  assert.deepEqual(otherKeys(['type', 'threshold', 'title', 'model']), ['threshold', 'model']);
  assert.deepEqual(otherKeys(['type', 'title']), []);
  assert.deepEqual(otherKeys(undefined), []);
});

test('values reach the markup escaped', () => {
  const html = fieldsMarkup({ title: '"><script>alert(1)</script>', tags: ['<b>'] });
  assert.ok(!html.includes('<script>'), html.slice(0, 400));
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(!html.includes('value="<b>"'));
});

test('a date the control cannot hold is shown as text rather than erased', () => {
  // A date input takes YYYY-MM-DD and silently drops anything else. A
  // producer who wrote a fuller timestamp must still see it.
  const ok = fieldsMarkup({ stale_after: '2026-12-31' });
  assert.match(ok, /type="date"[^>]*value="2026-12-31"/);
  const odd = fieldsMarkup({ stale_after: '2026-12-31T09:00:00Z' });
  assert.match(odd, /type="text"[^>]*value="2026-12-31T09:00:00Z"/);
});

test('a field appears when its type asks for it or its key is present', () => {
  // The Attested Computation contract (SPEC §10.2) is noise on every
  // other type, and usage_window earns its place only when a document
  // brings it. A key the document carries always gets its editor —
  // hiding one would make it uneditable without saying so.
  const base = fieldsMarkup({ type: 'Insight' });
  assert.ok(!base.includes('data-fm-key="runtime"'), 'runtime drawn for a type with no contract');
  assert.ok(!base.includes('data-fm-key="usage_window"'), 'usage_window drawn with no key to edit');
  const ac = fieldsMarkup({ type: 'Attested Computation' });
  assert.ok(ac.includes('data-fm-key="runtime"'), 'the contract is missing from its own type');
  const carried = fieldsMarkup({ type: 'Insight', runtime: 'bigquery', usage_window: { from: '2026-01-01' } });
  assert.ok(carried.includes('data-fm-key="runtime"'), 'a carried key lost its editor');
  assert.ok(carried.includes('data-fm-key="usage_window"'), 'a carried usage_window lost its editor');
});

test('every control says which key and column it belongs to', () => {
  // The editor reads the form back by walking these markers; a control
  // without them is a value that never reaches the document. Rendered
  // from a document that draws every field: the Attested Computation
  // contract by its type, usage_window by being present.
  const html = fieldsMarkup({ type: 'Attested Computation', usage_window: { from: '2026-01-01' } });
  for (const f of FIELDS) {
    assert.ok(html.includes(`data-fm-key="${f.key}"`) || html.includes(`data-fm-rows="${f.key}"`)
      || html.includes(`data-fm-list="${f.key}"`), `${f.key} has no marker`);
  }
  const row = rowMarkup(field('sources'), 0, { resource: 'x' });
  for (const col of field('sources').columns) {
    assert.ok(row.includes(`data-fm-path="${col.name}"`), `${col.name} has no marker`);
  }
  assert.match(listItemMarkup('tags', '', 2, 'sales'), /data-fm-key="tags"[^>]*data-fm-i="2"/);
});

test('a type is a suggestion list, not a menu', () => {
  // SPEC §4.1 makes type any string; ochakai stores what the writer
  // wrote. A <select> would make the page refuse what the format allows.
  const html = fieldsMarkup({ type: 'Runbook' });
  assert.match(html, /<input type="text"[^>]*list="fm-list-type"[^>]*value="Runbook"/);
  assert.match(html, /<datalist id="fm-list-type">/);
  // status is the opposite case: SPEC §5.4 gives it three values.
  assert.match(html, /<select[^>]*data-fm-key="status"/);
});
