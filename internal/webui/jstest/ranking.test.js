// How a ranking is divided for a reader: which hits are shown, which the
// fold hides, and how many of the hidden ones are still inside the answer
// an agent receives (design doc 0115).
//
// The division is the part of the search view that has to be exactly
// right and is invisible when it is wrong — a hit folded away still
// reaches every other caller, and the rank it holds is what the edge is
// drawn on. Outside static/, so it is not embedded and not served.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { divideRanking } from '../static/js/format.js';

// A lexical ranking: scores on the 0-3 scale, three of them under the
// weak floor.
const LEXICAL = [
  { id: 'a', score: 1.7 },
  { id: 'b', score: 0.9 },
  { id: 'c', score: 0.12 },
  { id: 'd', score: 0.3 },
  { id: 'e', score: 0.02 },
  { id: 'f', score: 0.01 },
];

test('a listing is not folded: no query, nothing weak about it', () => {
  const { shown, weak, weakInCut } = divideRanking(LEXICAL, { fold: false });
  assert.equal(shown.length, LEXICAL.length);
  assert.deepEqual(weak, []);
  assert.equal(weakInCut, 0);
});

test('the fold splits a lexical ranking at the weak floor', () => {
  const { shown, weak } = divideRanking(LEXICAL, { fold: true });
  assert.deepEqual(shown.map(r => r.hit.id), ['a', 'b', 'd']);
  assert.deepEqual(weak.map(r => r.hit.id), ['c', 'e', 'f']);
});

test('a hit keeps the rank the server gave it, on either side of the fold', () => {
  const { shown, weak } = divideRanking(LEXICAL, { fold: true });
  // d is fourth in the ranking and third on screen; the edge is drawn on
  // the first number, so a fold above must not renumber it.
  assert.deepEqual(shown.map(r => r.rank), [0, 1, 3]);
  assert.deepEqual(weak.map(r => r.rank), [2, 4, 5]);
});

test('hybrid scores are never folded: the whole ranking sits under the floor', () => {
  // Fused scores live on a ~1/60 scale (design doc 0110), where the weak
  // floor would hide every hit. The top score is what says which scale
  // this is.
  const rrf = [{ id: 'a', score: 0.049 }, { id: 'b', score: 0.032 }, { id: 'c', score: 0.016 }];
  const { shown, weak } = divideRanking(rrf, { fold: true });
  assert.equal(shown.length, 3);
  assert.deepEqual(weak, []);
});

test('a folded hit inside the answer is counted, because hiding it must not mean nobody sees it', () => {
  // The server answered with four hits; c and e are folded away, and c is
  // inside that answer while e is past it.
  const { weakInCut } = divideRanking(LEXICAL, { fold: true, cut: 4 });
  assert.equal(weakInCut, 1);
});

test('nothing is inside the answer when its size is unknown', () => {
  assert.equal(divideRanking(LEXICAL, { fold: true }).weakInCut, 0);
});

test('an empty ranking divides into nothing rather than throwing', () => {
  const { shown, weak, weakInCut } = divideRanking([], { fold: true, cut: 10 });
  assert.deepEqual(shown, []);
  assert.deepEqual(weak, []);
  assert.equal(weakInCut, 0);
});
