// The writes the page makes on somebody's behalf, held to the calls they
// turn into. `documents.test.js` covers the edit itself; this covers the
// module that carries it to the server — which is the half a pure test
// cannot reach, and the half that shipped broken from v0.21.0 to
// v0.27.4: applyStatus called conceptURL without importing it, so every
// status change threw a ReferenceError, the PUT never left the browser,
// and the page put the old value back. Nothing caught it, because the
// only checks on this module read its source rather than run it.
//
// So it is run. The page's three globals are stubbed just enough for the
// module graph to load — a document that answers querySelector, a
// location for the origin, and a fetch that records what it was asked
// for — and the assertions are about the requests, not the DOM.

import { test } from 'node:test';
import assert from 'node:assert/strict';

globalThis.location = { protocol: 'http:', origin: 'http://ui.test' };
const stubEl = () => ({
  textContent: '', hidden: false, title: '', innerHTML: '',
  addEventListener() {}, querySelectorAll: () => [],
  classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
});
globalThis.document = {
  querySelector: stubEl, querySelectorAll: () => [], addEventListener() {}, body: stubEl(),
};

const calls = [];
let stored = '';
globalThis.fetch = async (url, init = {}) => {
  const method = init.method || 'GET';
  calls.push({ url, method, body: init.body, headers: init.headers || {} });
  if (method === 'PUT') {
    stored = init.body;
    return new Response(JSON.stringify({ id: 'metrics/revenue' }), { status: 200 });
  }
  if (url.endsWith('/api/v1/stats')) {
    return new Response(JSON.stringify({ queues: { drafts: 1, failed: 0, stale_after: 0 } }), { status: 200 });
  }
  if (method === 'POST') return new Response(JSON.stringify({}), { status: 200 });
  return new Response(JSON.stringify({ id: 'metrics/revenue', document: stored }), { status: 200 });
};

const { applyStatus, verifyEntry } = await import('../static/js/actions.js');

const DOC = `---
type: Metric
title: "Revenue"
description: "売上"
status: draft
sources:
  - id: handbook
    resource: https://example.com/handbook
---

本文。ここは投影が持っていない。
`;

test('a status change reads the document and writes the same one back, one line changed', async () => {
  calls.length = 0;
  stored = DOC;
  await applyStatus('metrics/revenue', 'stable');

  assert.deepEqual(calls.map(c => c.method), ['GET', 'PUT']);
  // The document's own address, which is what makes this a full
  // replacement of what the writer wrote rather than a patch of the
  // projection the page happens to hold.
  for (const c of calls) {
    assert.equal(c.url, 'http://ui.test/api/v1/bundle/metrics/revenue.md');
  }
  assert.equal(calls[1].headers['Content-Type'], 'text/markdown');
  assert.match(stored, /^status: stable$/m);
  // Everything the projection does not carry survived the round trip —
  // the body, the sources, the description (design doc 0035 §4 is the
  // same bug with fewer fields).
  assert.match(stored, /本文。ここは投影が持っていない。/);
  assert.match(stored, /resource: https:\/\/example\.com\/handbook/);
  assert.match(stored, /^description: "売上"$/m);
});

test('a refused write reaches the caller, which is what puts the badge back', async () => {
  const ok = globalThis.fetch;
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ code: 'read_only', error: '読み取り専用です' }), { status: 403 });
  // Restored even when the assertion below throws: a stub left in place
  // fails the next test for the wrong reason, and the log then names a
  // test that is fine.
  try {
    await assert.rejects(() => applyStatus('metrics/revenue', 'stable'), /読み取り専用です/);
  } finally {
    globalThis.fetch = ok;
  }
});

test('verifying is a ruling on the review face, not an edit of the document', async () => {
  calls.length = 0;
  await verifyEntry('metrics/revenue');
  const ruling = calls.find(c => c.method === 'POST');
  assert.equal(ruling.url, 'http://ui.test/api/v1/review/metrics/revenue');
  assert.deepEqual(JSON.parse(ruling.body), { ruling: 'verified' });
  assert.equal(calls.filter(c => c.method === 'PUT').length, 0);
});
