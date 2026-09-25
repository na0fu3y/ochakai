// The page's copy of `ochakai seed` (cmd/ochakai/seed.go): a dataset's
// INFORMATION_SCHEMA rows, read by the person as themselves, turned into
// one draft `BigQuery Table` concept per table (design doc 0148).
//
// It is a copy, and a copy drifts the day one side learns a rule the
// other did not. Both are held to one file for that reason —
// cmd/ochakai/testdata/seed-golden.json, which the Go test writes and
// internal/webui/jstest/seed.test.js reads — so the drift fails CI
// instead of reaching somebody's base. Change the Go side, rerun
// `go test ./cmd/ochakai -run TestSeedGolden -update`, and this file
// has to follow.

export const TABLE_TYPE = 'BigQuery Table';

// A source is `project.dataset`, or `project.dataset.table` for one
// table — `events_*` naming a date-sharded one. Spelled narrowly on
// purpose: every part is pasted into the query's FROM and WHERE, so
// what cannot be a BigQuery name is refused here rather than escaped.
const PROJECT = /^[a-z][a-z0-9.:-]*[a-z0-9]$/;
const DATASET = /^[A-Za-z0-9_]+$/;
const TABLE = /^[A-Za-z0-9_$-]+\*?$/;

export function parseSource(s) {
  const parts = s.trim().replace(/^`|`$/g, '').split('.');
  // A domain-scoped project (example.com:proj) carries a dot of its own.
  if (parts.length > 2 && parts[0].includes(':') === false && parts[1].includes(':')) {
    parts.splice(0, 2, parts[0] + '.' + parts[1]);
  }
  const [project, dataset, table = ''] = parts;
  if (parts.length < 2 || parts.length > 3 || !PROJECT.test(project || '') || !DATASET.test(dataset || '')
      || (table && !TABLE.test(table))) {
    throw new Error('取り込み元は project.dataset か project.dataset.table の形で書いてください(例: bigquery-public-data.thelook_ecommerce)');
  }
  return { project, dataset, table };
}

// schemaQuery is the one query the page runs. COLUMN_FIELD_PATHS is
// joined for the column descriptions BigQuery keeps there — the only
// prose in a schema a person wrote — and only at the top level, where
// field_path is the column itself.
export function schemaQuery({ project, dataset, table }) {
  const at = view => `\`${project}.${dataset}.INFORMATION_SCHEMA.${view}\``;
  let where = '';
  if (table.endsWith('*')) where = `\nWHERE STARTS_WITH(c.table_name, '${table.slice(0, -1)}')`;
  else if (table) where = `\nWHERE c.table_name = '${table}'`;
  return `SELECT c.table_schema, c.table_name, c.column_name, c.data_type, c.is_nullable, f.description
FROM ${at('COLUMNS')} c
LEFT JOIN ${at('COLUMN_FIELD_PATHS')} f
  ON f.table_name = c.table_name AND f.column_name = c.column_name AND f.field_path = c.column_name${where}
ORDER BY c.table_name, c.ordinal_position`;
}

// Byte order, as Go's string comparison has it: the golden file is
// sorted that way, and localeCompare would disagree with it.
const byName = (a, b) => (a.schema !== b.schema ? cmp(a.schema, b.schema) : cmp(a.name, b.name));
const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);
const str = v => (v === null || v === undefined ? '' : String(v));

// gather groups columns by table, in the order the rows arrived.
export function gather(rows) {
  const index = new Map();
  const tables = [];
  for (const r of rows) {
    const c = {
      schema: str(r.table_schema), table: str(r.table_name), column: str(r.column_name),
      dataType: str(r.data_type), isNullable: str(r.is_nullable), comment: str(r.description),
    };
    if (!c.table || !c.column) continue;
    const key = c.schema + '.' + c.table;
    if (!index.has(key)) {
      index.set(key, tables.length);
      tables.push({ schema: c.schema, name: c.table, columns: [], shards: [] });
    }
    tables[index.get(key)].columns.push(c);
  }
  return tables.sort(byName);
}

const SHARD = /^(.*[^0-9])([0-9]{8})$/;

function isDate(d) {
  const y = +d.slice(0, 4), m = +d.slice(4, 6), day = +d.slice(6, 8);
  const t = new Date(Date.UTC(y, m - 1, day));
  return t.getUTCFullYear() === y && t.getUTCMonth() === m - 1 && t.getUTCDate() === day;
}

// foldShards folds each set of two or more date-sharded tables into one
// entry named by the stem, with the latest shard's columns — unless the
// stem is itself a table's name. seed.go's foldShards says why.
export function foldShards(tables) {
  const named = new Set(tables.map(t => t.schema + '\x00' + t.name));
  const groups = new Map();
  tables.forEach((t, i) => {
    const m = SHARD.exec(t.name);
    if (!m || !isDate(m[2])) return;
    const k = t.schema + '\x00' + m[1];
    if (!groups.has(k)) groups.set(k, { schema: t.schema, stem: m[1], members: [] });
    groups.get(k).members.push(i);
  });
  const folded = new Set();
  const out = [];
  for (const [k, g] of groups) {
    if (g.members.length < 2 || named.has(k)) continue;
    let latest = g.members[0];
    const suffixes = [];
    for (const i of g.members) {
      folded.add(i);
      suffixes.push(tables[i].name.slice(g.stem.length));
      if (tables[i].name > tables[latest].name) latest = i;
    }
    out.push({ schema: g.schema, name: g.stem, columns: tables[latest].columns, shards: suffixes.sort(cmp) });
  }
  tables.forEach((t, i) => { if (!folded.has(i)) out.push(t); });
  return out.sort(byName);
}

// seedID is the concept's address: prefix, dataset, table.
export function seedID(t, prefix) {
  return [prefix, t.schema, t.name]
    .map(s => s.replace(/^\/+|\/+$/g, '').normalize('NFC'))
    .filter(Boolean).join('/');
}

function title(t) {
  const name = t.name + (t.shards.length ? '*' : '');
  return t.schema ? t.schema + '.' + name : name;
}

// seedDocument renders one table as an OKF document, byte for byte what
// seed.go's seedDocument writes.
export function seedDocument(t, project) {
  let b = `---\ntype: ${TABLE_TYPE}\n`;
  if (project && t.name) b += `resource: bigquery://${project}.${title(t)}\n`;
  b += `title: ${title(t)}\nstatus: draft\n---\n\n`;
  b += 'Projected from the warehouse\'s own schema listing. **Nothing below was\n'
    + 'written by a person yet** — what this table is for, which column lies, and\n'
    + 'when the load is late are the reasons anybody will read this entry, and the\n'
    + 'schema does not know any of them.\n\n';
  const n = t.shards.length;
  if (n) {
    const s = t.name;
    b += `**Date-sharded**: ${n} tables, \`${s}${t.shards[0]}\` to \`${s}${t.shards[n - 1]}\`, folded into this one\n`
      + `entry. Query them as \`${s}*\`, narrowing with \`_TABLE_SUFFIX\`; the columns\n`
      + 'below are the latest shard\'s.\n\n';
  }
  b += '| Column | Type | Null | Note |\n|---|---|---|---|\n';
  for (const c of t.columns) {
    const nul = c.isNullable.toUpperCase() === 'YES' ? 'yes' : '';
    b += `| \`${c.column}\` | ${c.dataType} | ${nul} | ${c.comment.replaceAll('|', '\\|')} |\n`;
  }
  return b;
}

// project is the whole projection: rows in, {id, title, document,
// columns, shards} out, in the order the bundle would list them.
export function project(rows, { project: proj = '', prefix = 'tables' } = {}) {
  return foldShards(gather(rows)).map(t => ({
    id: seedID(t, prefix), title: title(t), document: seedDocument(t, proj),
    columns: t.columns.length, shards: t.shards.length,
  }));
}
