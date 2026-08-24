// The module that rewrites what somebody wrote. Held to examples,
// because every bug here is a bug in a document a person is editing.
//
// Outside static/, so it is not embedded and not served: `//go:embed
// static` takes the page, and the tests stay in the repository.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  templateDocument, withFrontmatterKey, withStatus,
} from '../static/js/documents.js';

const FM = `---
type: Metric
title: "Revenue"
status: draft
---

本文。
`;

test('a key the document already names is replaced in place', () => {
  const out = withFrontmatterKey(FM, 'status', 'stable');
  assert.match(out, /^status: stable$/m);
  assert.doesNotMatch(out, /status: draft/);
  // Everything else survives, body included.
  assert.match(out, /^type: Metric$/m);
  assert.match(out, /本文。/);
});

test('a key the document does not name is appended inside the frontmatter', () => {
  const out = withFrontmatterKey(FM, 'runtime', 'bigquery');
  const [, front] = out.match(/^---\r?\n([\s\S]*?)---\r?\n/);
  assert.match(front, /^runtime: bigquery$/m);
});

test('a document with no frontmatter is returned unchanged', () => {
  // Nothing to edit and nothing to invent: appending a bare `key: value`
  // to a body would put a line in the document that is not frontmatter.
  const body = 'ただの本文\n';
  assert.equal(withFrontmatterKey(body, 'status', 'stable'), body);
});

test('CRLF frontmatter is still frontmatter', () => {
  const crlf = '---\r\ntype: Metric\r\n---\r\n\r\nbody\r\n';
  assert.match(withFrontmatterKey(crlf, 'status', 'draft'), /^status: draft$/m);
});

test('the body is never searched for the key', () => {
  // A body line that looks like frontmatter is prose, and replacing it
  // would edit what somebody wrote rather than what they declared.
  const doc = '---\ntype: Metric\nstatus: draft\n---\n\nstatus: この行は本文です。\n';
  const out = withStatus(doc, 'stable');
  assert.match(out, /^status: この行は本文です。$/m);
  assert.equal(out.match(/^status: stable$/m).length, 1);
});

test('a new document carries the type it was asked for', () => {
  const doc = templateDocument('Insight', 'なぜ売上が落ちたか');
  assert.match(doc, /^type: Insight$/m);
  assert.match(doc, /^status: draft$/m);
  // The title is JSON-quoted, so a colon in it cannot break the block.
  assert.match(templateDocument('Insight', 'Q1: 売上'), /^title: "Q1: 売上"$/m);
});

test('a new Attested Computation carries what the server refuses it without', () => {
  const doc = templateDocument('Attested Computation', '');
  assert.match(doc, /^runtime: bigquery$/m);
  // A seed only ever opens an empty document, so it is here too.
  assert.match(doc, /^parameters:$/m);
});

test('a seed shows the shape of the type it opens', () => {
  // Illustrative, and only ever in an empty document: a resource spelled
  // out so a fresh template shows what the type is for.
  assert.match(templateDocument('Reference', ''), /^resource: https:\/\//m);
  assert.match(templateDocument('BigQuery Table', ''), /^resource: bigquery:\/\//m);
  // A type with nothing required and nothing to seed opens bare.
  const insight = templateDocument('Insight', '');
  assert.doesNotMatch(insight, /^resource:/m);
  assert.doesNotMatch(insight, /^runtime:/m);
});
