import assert from 'node:assert/strict';
import test from 'node:test';

import { groupByType } from '../static/js/vocab.js';

const c = (id, type) => ({ id, type });
const shape = groups => groups.map(g => [g.type, g.concepts.map(x => x.id)]);

test('recommended types come in teaching order, free types after by name', () => {
  const got = groupByType([
    c('sales/team', 'Team'), c('sales/terms/arr', 'Glossary Term'),
    c('sales/revenue', 'Metric'), c('sales/about', 'Area'), c('sales/mrr', 'Metric'),
  ]);
  assert.deepEqual(shape(got), [
    ['Metric', ['sales/revenue', 'sales/mrr']],
    ['Glossary Term', ['sales/terms/arr']],
    ['Area', ['sales/about']],
    ['Team', ['sales/team']],
  ]);
});

test('types match case-insensitively; recommended ones take their recommended spelling', () => {
  const got = groupByType([c('a', 'metric'), c('b', 'Metric'), c('x', 'team'), c('y', 'TEAM')]);
  assert.deepEqual(shape(got), [['Metric', ['a', 'b']], ['team', ['x', 'y']]]);
});

test('one type is one group, and nothing is no groups', () => {
  assert.equal(groupByType([c('a', 'Skill'), c('b', 'skill')]).length, 1);
  assert.deepEqual(groupByType([]), []);
});
