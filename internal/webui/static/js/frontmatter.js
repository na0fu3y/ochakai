// The frontmatter as fields: which keys get an editor, what each one
// looks like, and how what a person typed becomes the value that goes
// back into the document.
//
// Pure, like documents.js and for the same reason — this decides what a
// writer's frontmatter becomes, so it is held to examples. It renders
// markup from values and reads values back out of plain objects the
// editor collected from the DOM; the DOM itself is views/editor.js.
//
// It parses no YAML and writes none. Both directions go through
// POST /api/v1/frontmatter (design doc 0130): the server reads the
// document a producer wrote and renders the block a person will read in
// a diff, because a subset parser in the page is the thing design doc
// 0130 §3.1 refused — its failure mode is a form that silently drops a
// key.

import { esc } from './escape.js';
import { KNOWN_TYPES, STATUSES } from './vocab.js';

// The runtimes SPEC §10.2 names as examples. A suggestion list rather
// than a vocabulary: the key takes any string, and ochakai stores what
// the writer wrote (design doc 0036 §3.4).
const RUNTIMES = ['bigquery', 'postgres', 'dbt', 'python', 'looker'];

// FIELDS is every frontmatter key this form edits, in the order OKF
// SPEC introduces them — base (§4.1), provenance (§5.1), lifecycle
// (§5.4-§5.5), Attested Computation (§10.2) — which is the order the
// bundle writer emits them in too, so a document written here reads like
// one written by hand.
//
// What is not here is what nobody writes: `generated` and `verified` are
// this instance's observations and the write path ignores them (design
// doc 0009), and `okf_version` belongs to the bundle root, which is
// generated. A producer's own key is not here either — it has no field,
// and it is not lost: the document keeps it, and the form says so.
export const FIELDS = [
  { key: 'type', kind: 'combo', label: '型', options: KNOWN_TYPES, required: true },
  { key: 'title', kind: 'text', label: 'タイトル' },
  { key: 'description', kind: 'prose', label: '説明', hint: '一文で。検索結果と一覧に出ます。' },
  {
    key: 'resource', kind: 'text', label: '実体の場所',
    placeholder: 'bigquery://project/dataset/table',
    hint: 'この知識が指している物の URI。テーブル、ダッシュボード、外部ページなど。',
  },
  { key: 'tags', kind: 'list', label: 'タグ', placeholder: 'sales' },
  {
    key: 'sources', kind: 'rows', label: '出典', addLabel: '出典を足す',
    hint: 'この知識が何から導かれたかです。',
    columns: [
      { name: 'resource', label: '場所', placeholder: 'bigquery://project/dataset/table' },
      { name: 'title', label: '見出し' },
      { name: 'id', label: '識別子' },
      { name: 'author', label: '作成者', placeholder: 'human:tanaka@example.co.jp' },
      { name: 'usage_count', label: '参照回数', kind: 'number' },
      { name: 'last_modified', label: '最終更新', kind: 'date' },
    ],
  },
  {
    key: 'usage_window', kind: 'object', label: '参照回数を数えた期間', onlyIfPresent: true,
    hint: '上の参照回数が、いつからいつまでを数えたものかです。',
    columns: [
      { name: 'from', label: '開始', kind: 'date' },
      { name: 'to', label: '終了', kind: 'date' },
    ],
  },
  { key: 'status', kind: 'enum', label: '状態', options: STATUSES },
  { key: 'stale_after', kind: 'date', label: '再確認日' },
  {
    key: 'runtime', kind: 'combo', label: '実行環境', options: RUNTIMES,
    types: ['Attested Computation'],
    hint: '計算を約束する型では必須です(SPEC §10.2)。ochakai は記録するだけで、実行はしません。',
  },
  {
    key: 'parameters', kind: 'rows', label: 'パラメータ', addLabel: 'パラメータを足す',
    types: ['Attested Computation'],
    hint: 'エージェントが埋める穴です。',
    columns: [
      { name: 'name', label: '名前', placeholder: 'year' },
      { name: 'type', label: '型', placeholder: 'integer' },
      { name: 'required', label: '必須', kind: 'bool' },
    ],
  },
  {
    key: 'computation', kind: 'text', label: '計算の置き場所',
    types: ['Attested Computation'],
    hint: '空のままなら、本文の <code># Computation</code> のコードブロックが計算です。',
  },
  {
    key: 'executor', kind: 'object', label: '実行のしかた',
    types: ['Attested Computation'],
    columns: [
      { name: 'resource', label: '手順・コードの場所' },
      { name: 'receipt', label: '実行が返すべき項目', kind: 'list' },
    ],
  },
  {
    key: 'attester', kind: 'object', label: '検証のしかた',
    types: ['Attested Computation'],
    hint: '実行が正しく行われたかを機械的に確かめるコードの場所です。',
    columns: [{ name: 'resource', label: 'コードの場所' }],
  },
];

export const FIELD_KEYS = new Set(FIELDS.map(f => f.key));

// otherKeys names the frontmatter keys the form has no editor for, in
// the document's own order. They are the reason this page can be a form
// at all: a producer's key is kept exactly where its writer put it (SPEC
// §4.1), and saying so is what keeps "the form is the document" true
// rather than merely hoped for.
export function otherKeys(keys) {
  return (keys || []).filter(k => !FIELD_KEYS.has(k));
}

// ---- values -----------------------------------------------------------

// valueOf turns what the editor collected for one field into the value
// that goes into the document, or undefined for "this key is not in the
// document" — which is not the same as an empty one. ochakai does not
// write a key its writer left out (design doc 0046 §3.9), so a cleared
// field removes the key rather than storing "".
export function valueOf(field, raw) {
  switch (field.kind) {
    case 'list':
      return listValue(raw);
    case 'rows': {
      const rows = (raw || []).map(r => objectValue(field.columns, r)).filter(Boolean);
      return rows.length ? rows : undefined;
    }
    case 'object':
      return objectValue(field.columns, raw);
    default:
      return scalarValue({ kind: field.kind }, raw);
  }
}

function listValue(raw) {
  const out = (raw || []).map(v => String(v ?? '').trim()).filter(v => v !== '');
  return out.length ? out : undefined;
}

// objectValue drops every column left empty and the object itself when
// nothing is left — a `usage_window` with neither end is a key that says
// nothing, and writing one would be inventing a claim.
function objectValue(columns, raw) {
  const out = {};
  for (const col of columns) {
    const v = col.kind === 'list' ? listValue((raw || {})[col.name]) : scalarValue(col, (raw || {})[col.name]);
    if (v !== undefined) out[col.name] = v;
  }
  return Object.keys(out).length ? out : undefined;
}

function scalarValue(col, raw) {
  if (col.kind === 'bool') {
    // SPEC §10.2: absent and false say the same thing, so only true is
    // written. A `required: false` line would be a key the writer did
    // not ask for.
    return raw === true ? true : undefined;
  }
  const s = String(raw ?? '').trim();
  if (s === '') return undefined;
  if (col.kind === 'number') {
    const n = Number(s);
    // Not a number is kept as the text it was: the server stores what
    // the writer wrote, and a form that silently turned "many" into NaN
    // would be deciding for them.
    return Number.isFinite(n) ? n : s;
  }
  return s;
}

// ---- markup -----------------------------------------------------------

// visibleFields picks the fields worth drawing for this document. A key
// the document carries always gets its editor — hiding one would make it
// uneditable without saying so. A field tied to types (the Attested
// Computation contract, SPEC §10.2) appears only when the type asks for
// it, and one marked onlyIfPresent stays away until a document brings
// the key: every field at once was a form nobody could see whole.
export function visibleFields(values) {
  const v = values || {};
  return FIELDS.filter(f => {
    if (v[f.key] !== undefined) return true;
    if (f.types) return f.types.includes(v.type);
    return !f.onlyIfPresent;
  });
}

// fieldsMarkup renders the visible fields from the frontmatter's values.
// The values come from the server's read of the document, so what is
// drawn here is the document — not a second store the page keeps beside
// it.
export function fieldsMarkup(values) {
  const v = values || {};
  return visibleFields(v).map(f => fieldMarkup(f, v[f.key])).join('');
}

export function fieldMarkup(field, value) {
  const body = {
    list: listMarkup, rows: rowsMarkup, object: objectMarkup,
  }[field.kind];
  if (body) return groupMarkup(field, body(field, value));
  return `
      <div class="field" data-fm-field="${field.key}">
        ${labelMarkup(field, `f-${field.key}`)}
        ${inputMarkup(field, `f-${field.key}`, field.key, '', value)}
        ${hintMarkup(field)}
      </div>`;
}

function groupMarkup(field, inner) {
  return `
      <div class="fieldset" data-fm-field="${field.key}">
        <div class="legend">${esc(field.label)} <code>${field.key}</code>${field.required ? ' <span class="req">必須</span>' : ''}</div>
        ${inner}
        ${hintMarkup(field)}
      </div>`;
}

function labelMarkup(field, id) {
  return `<label class="top" for="${id}">${esc(field.label)} <code>${field.key}</code>`
    + `${field.required ? ' <span class="req">必須</span>' : ''}</label>`;
}

function hintMarkup(field) {
  // The hint is authored copy, not a value, so its markup is deliberate:
  // it is the one string here that is not escaped.
  return field.hint ? `<div class="hint">${field.hint}</div>` : '';
}

// inputMarkup is one control. name and col address it: the editor reads
// the form back by walking [data-fm-key], so every control says which
// key it belongs to, which row it is in, and which column of that row.
function inputMarkup(field, id, key, path, value, col) {
  const spec = col || field;
  const at = `data-fm-key="${key}"${path ? ` data-fm-path="${path}"` : ''}`;
  const place = spec.placeholder ? ` placeholder="${esc(spec.placeholder)}"` : '';
  switch (spec.kind) {
    case 'prose':
      return `<textarea id="${id}" ${at} rows="2"${place}>${esc(value ?? '')}</textarea>`;
    case 'bool':
      // A bare checkbox under the column's own label, like every other
      // control here: a checkbox carrying its own text sat a line above
      // the boxes beside it, and lining it up by hand is a number that
      // goes wrong the first time a font changes.
      return `<input type="checkbox" class="check" id="${id}" ${at}${value === true ? ' checked' : ''}>`;
    case 'date':
      // A date input takes YYYY-MM-DD and hands one back, which is what
      // OKF writes (SPEC §5.5). A value in any other spelling would be
      // dropped by the control, so it falls back to a text box rather
      // than being erased by being shown.
      return isDate(value) || !value
        ? `<input type="date" id="${id}" ${at} value="${esc(value ?? '')}">`
        : `<input type="text" id="${id}" ${at} class="mono" value="${esc(value)}">`;
    case 'number':
      return `<input type="number" id="${id}" ${at} value="${esc(value ?? '')}"${place}>`;
    case 'enum':
      return `<select id="${id}" ${at}>`
        + `<option value=""${value ? '' : ' selected'}>—</option>`
        + spec.options.map(o => `<option value="${esc(o)}"${o === value ? ' selected' : ''}>${esc(o)}</option>`).join('')
        + `</select>`;
    case 'combo':
      // A list of suggestions, not a vocabulary: the key takes any
      // string, and a <select> would make the page refuse what the
      // format allows.
      return `<input type="text" id="${id}" ${at} list="fm-list-${key}" value="${esc(value ?? '')}"${place}>`
        + `<datalist id="fm-list-${key}">${spec.options.map(o => `<option value="${esc(o)}"></option>`).join('')}</datalist>`;
    default:
      return `<input type="text" id="${id}" ${at} value="${esc(value ?? '')}"${place}>`;
  }
}

const isDate = v => typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v);

function listMarkup(field, value) {
  const items = Array.isArray(value) ? value : [];
  const rows = items.map((v, i) => listItemMarkup(field.key, '', i, v, field)).join('');
  return `<div class="fm-list" data-fm-list="${field.key}">${rows}</div>`
    + `<button type="button" class="btn small fm-add" data-fm-add="${field.key}">＋</button>`;
}

// listItemMarkup is one string of a list of strings, with the button
// that takes it out. Exported so the editor can add a row without
// redrawing the form under a cursor that is in it.
export function listItemMarkup(key, path, i, value, field) {
  const place = field && field.placeholder ? ` placeholder="${esc(field.placeholder)}"` : '';
  return `<div class="fm-item">`
    + `<input type="text" data-fm-key="${key}"${path ? ` data-fm-path="${path}"` : ''} data-fm-i="${i}" value="${esc(value ?? '')}"${place}>`
    + `<button type="button" class="btn small fm-del" title="この行を消します" aria-label="この行を消す">×</button>`
    + `</div>`;
}

function objectMarkup(field, value) {
  const v = value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  return `<div class="row-trio">` + field.columns.map(col => {
    const id = `f-${field.key}-${col.name}`;
    if (col.kind === 'list') {
      const items = Array.isArray(v[col.name]) ? v[col.name] : [];
      return `<div class="field"><label class="top">${esc(col.label)} <code>${col.name}</code></label>`
        + `<div class="fm-list" data-fm-list="${field.key}" data-fm-list-path="${col.name}">`
        + items.map((s, i) => listItemMarkup(field.key, col.name, i, s)).join('')
        + `</div><button type="button" class="btn small fm-add" data-fm-add="${field.key}" data-fm-add-path="${col.name}">＋</button></div>`;
    }
    return `<div class="field"><label class="top" for="${id}">${esc(col.label)} <code>${col.name}</code></label>`
      + inputMarkup(field, id, field.key, col.name, v[col.name], col)
      + (col.hint ? `<div class="hint">${col.hint}</div>` : '')
      + `</div>`;
  }).join('') + `</div>`;
}

function rowsMarkup(field, value) {
  const rows = Array.isArray(value) ? value : [];
  return `<div class="rows" data-fm-rows="${field.key}">${rows.map((r, i) => rowMarkup(field, i, r)).join('')}</div>`
    + `<button type="button" class="btn small fm-add" data-fm-add="${field.key}">${esc(field.addLabel || '行を足す')}</button>`;
}

// rowMarkup is one repeated object — one source, one parameter. Exported
// for the same reason listItemMarkup is.
export function rowMarkup(field, i, row) {
  const r = row && typeof row === 'object' ? row : {};
  const cells = field.columns.map(col => {
    const id = `f-${field.key}-${i}-${col.name}`;
    return `<div class="field">`
      + `<label class="top" for="${id}">${esc(col.label)} <code>${col.name}</code></label>`
      + inputMarkup(field, id, field.key, col.name, r[col.name], { ...col, kind: col.kind || 'text' })
      + (col.hint ? `<div class="hint">${col.hint}</div>` : '')
      + `</div>`;
  }).join('');
  return `<div class="row-card" data-fm-i="${i}">`
    + `<div class="row-head"><span class="n">${i + 1}</span>`
    + `<button type="button" class="btn small fm-del" title="この行を消します" aria-label="この行を消す">×</button></div>`
    + `<div class="row-trio">${cells}</div></div>`;
}
