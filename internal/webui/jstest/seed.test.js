// The page's projection of a schema listing, held to the file the Go
// side writes (design doc 0148 §3).

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { parseSource, project, schemaQuery } from '../static/js/seed.js';

const read = name => JSON.parse(readFileSync(new URL(`../../../cmd/ochakai/testdata/${name}`, import.meta.url), 'utf8'));

test('the page projects a listing byte for byte as ochakai seed does', () => {
  const golden = read('seed-golden.json');
  const got = project(read('seed-columns.json'), { project: golden.project, prefix: golden.prefix });
  assert.deepEqual(got.map(c => ({ id: c.id, document: c.document })), golden.concepts);
});

test('a source is a dataset, or one table in it', () => {
  assert.deepEqual(parseSource('bigquery-public-data.thelook_ecommerce'),
    { project: 'bigquery-public-data', dataset: 'thelook_ecommerce', table: '' });
  assert.deepEqual(parseSource(' `p-1.ga.events_*` '), { project: 'p-1', dataset: 'ga', table: 'events_*' });
  assert.deepEqual(parseSource('example.com:proj.ds.t'), { project: 'example.com:proj', dataset: 'ds', table: 't' });
  for (const bad of ['thelook', 'p.d.t.x', "p.d.t' OR 1=1 --", 'p.d-x', 'P.d', 'p.d.*']) {
    assert.throws(() => parseSource(bad), /project\.dataset/, bad);
  }
});

test('the query reads one dataset, narrowed to a table or a shard family', () => {
  const all = schemaQuery(parseSource('p-1.ga'));
  assert.match(all, /FROM `p-1\.ga\.INFORMATION_SCHEMA\.COLUMNS` c/);
  assert.doesNotMatch(all, /WHERE/);
  assert.match(schemaQuery(parseSource('p-1.ga.orders')), /WHERE c\.table_name = 'orders'/);
  assert.match(schemaQuery(parseSource('p-1.ga.events_*')), /WHERE STARTS_WITH\(c\.table_name, 'events_'\)/);
});
