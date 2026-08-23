import assert from 'node:assert/strict';
import test from 'node:test';

import { MAX_DESCENT, descendSingleRoad, isCorridor } from '../static/js/descend.js';

// A level as the browse endpoint answers one.
const level = (dirs = [], concepts = [], files = []) =>
  ({ dirs: dirs.map(name => ({ name, count: 1 })), concepts, files });

// A tree given as a map of prefix -> level, which is what the walk reads.
const loader = tree => async p => {
  if (!(p in tree)) throw new Error(`no level at ${p}`);
  return tree[p];
};

test('a corridor is one directory and nothing else', () => {
  assert.equal(isCorridor(level(['teams'])), true);
  assert.equal(isCorridor(level(['teams', 'glossary'])), false, 'two doors is a choice');
  assert.equal(isCorridor(level(['teams'], [{ id: 'note' }])), false, 'a concept here is a room');
  assert.equal(isCorridor(level(['teams'], [], [{ path: 'a.csv' }])), false, 'so is a file');
  assert.equal(isCorridor(level()), false, 'an empty level goes nowhere');
});

test('the walk stops where there is something to choose between', async () => {
  const tree = {
    'teams/': level(['growth']),
    'teams/growth/': level(['metrics', 'queries']),
  };
  const { level: at, prefix } = await descendSingleRoad(level(['teams']), '', loader(tree));
  assert.equal(prefix, 'teams/growth/');
  assert.deepEqual(at.dirs.map(d => d.name), ['metrics', 'queries']);
});

test('a level holding knowledge of its own is where the walk ends', async () => {
  const tree = { 'teams/': level(['growth'], [{ id: 'teams/charter' }]) };
  const { prefix } = await descendSingleRoad(level(['teams']), '', loader(tree));
  assert.equal(prefix, 'teams/', 'descending past teams/ would have hidden its concept');
});

test('a base with nothing to walk stays at the root', async () => {
  const { prefix } = await descendSingleRoad(level(['a', 'b']), '', loader({}));
  assert.equal(prefix, '');
  const empty = await descendSingleRoad(level(), '', loader({}));
  assert.equal(empty.prefix, '');
});

test('a level that cannot be read ends the walk where it stands', async () => {
  // The corridor says there is a door; opening it fails. The page must
  // still render what it already has, at the deepest level it read.
  const { level: at, prefix } = await descendSingleRoad(level(['teams']), '', loader({}));
  assert.equal(prefix, '');
  assert.deepEqual(at.dirs.map(d => d.name), ['teams']);
});

test('the walk is bounded', async () => {
  // A corridor with no end: every level answers with one more door.
  const endless = async p => level([String(p.split('/').length)]);
  const { prefix } = await descendSingleRoad(level(['1']), '', endless);
  assert.equal(prefix.split('/').filter(Boolean).length, MAX_DESCENT);
});
