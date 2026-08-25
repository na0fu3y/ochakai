// Which links point at nothing — and, just as much, which failures are
// not evidence that they do.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { checkTargets } from '../static/js/links.js';

const notFound = () => Object.assign(new Error('not found'), { code: 'not_found' });

test('a missing concept is dead and a present one is found', async () => {
  const { found, dead } = await checkTargets(['a', 'b'], id => {
    if (id === 'b') throw notFound();
    return Promise.resolve({ id });
  });
  assert.deepEqual([...found], ['a']);
  assert.deepEqual([...dead], ['b']);
});

test('a failure that is not a missing concept answers neither way', async () => {
  // A 500, a refused read, a dropped connection: none of these say the
  // concept is gone, and a red link claims exactly that — but none of
  // them says it is there either, and a caller that remembered them as
  // present would stop asking about a concept it never saw.
  const { found, dead } = await checkTargets(['a', 'b', 'c'], id => {
    if (id === 'a') throw Object.assign(new Error('boom'), { code: 'internal' });
    if (id === 'b') throw new Error('network');
    if (id === 'c') throw Object.assign(new Error('no'), { code: 'forbidden' });
    return Promise.resolve({});
  });
  assert.deepEqual([...dead], []);
  assert.deepEqual([...found], []);
});

test('each id is asked about once', async () => {
  const asked = [];
  await checkTargets(['a', 'b', 'a', '', 'b'], id => { asked.push(id); return Promise.resolve({}); });
  assert.deepEqual(asked.sort(), ['a', 'b']);
});

test('no more than the limit are in flight at once', async () => {
  let live = 0, peak = 0;
  const ids = Array.from({ length: 12 }, (_, i) => 'id' + i);
  await checkTargets(ids, () => {
    live++;
    peak = Math.max(peak, live);
    return new Promise(resolve => setTimeout(() => { live--; resolve({}); }, 1));
  }, 3);
  assert.ok(peak <= 3, `${peak} probes were in flight at once`);
});

test('nothing to check asks nothing', async () => {
  let asked = 0;
  const { found, dead } = await checkTargets([], () => { asked++; return Promise.resolve({}); });
  assert.equal(asked, 0);
  assert.equal(dead.size, 0);
  assert.equal(found.size, 0);
});

test('a pool of none would answer without asking, so there is no such pool', async () => {
  // Array.from({length: 0}) is the empty array, so a cap of zero used to
  // resolve instantly having probed nothing — an empty dead set, which
  // reads as "every link is fine".
  let asked = 0;
  const { dead } = await checkTargets(['a'], id => { asked++; throw notFound(); }, 0);
  assert.equal(asked, 1);
  assert.deepEqual([...dead], ['a']);
});
