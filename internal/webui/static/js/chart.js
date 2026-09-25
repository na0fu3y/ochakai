// Drawing a query's result as a chart (design doc 0145 §5.4).
//
// The page decides from the result's shape alone whether a chart says
// anything, and which one: no model chooses an axis, so the same rows
// draw the same chart for everybody, and a chart the shape does not
// support is simply not drawn — the table under it is always there.
//
// - The first column labels the rows; every other column is a number.
// - A date or time label draws columns placed on a time axis; a text
//   label draws horizontal bars in the order the query returned them.
//   Never a line: a line joins its points, and joining is a reading the
//   page would be choosing for the person. A column stands for its own
//   row only, so a period with no row, or a NULL, is visibly empty.
// - One to four series share one axis. Series whose scales differ by
//   more than SCALE_RATIO are drawn as one small chart each instead:
//   a second y-axis would let one of them lie.
//
// Nothing here is kept beyond the conversation: a chart is part of an
// answer, not a board to pin or share.

export const NUMERIC_TYPES = new Set(['INTEGER', 'INT64', 'FLOAT', 'FLOAT64', 'NUMERIC', 'BIGNUMERIC']);
const NUMERIC = NUMERIC_TYPES;
const TIME = new Set(['DATE', 'DATETIME', 'TIMESTAMP']);
const MAX_SERIES = 4;
const MAX_BARS = 20;
const SCALE_RATIO = 20;

const W = 640, H = 200;
const PAD = { top: 10, right: 16, bottom: 26, left: 56 };

// plan says what the result can be drawn as: null when it cannot, or
// { kind: 'line' | 'bar', label, series: [{ name, values }], rows }.
export function plan(res) {
  const types = res.types || [];
  if (!res.rows || res.rows.length < 2 || res.fields.length < 2) return null;
  const labelType = types[0];
  const kind = TIME.has(labelType) ? 'column' : labelType === 'STRING' ? 'bar' : null;
  if (!kind) return null;
  const rest = types.slice(1);
  if (rest.length > MAX_SERIES || !rest.every(t => NUMERIC.has(t))) return null;
  if (kind === 'bar' && res.rows.length > MAX_BARS) return null;
  let rows = res.rows.map(r => ({ label: r[0], values: r.slice(1).map(num) }));
  if (kind === 'column') {
    rows = rows.map(r => ({ ...r, t: Date.parse(r.label.replace(' UTC', 'Z')) })).filter(r => !Number.isNaN(r.t));
    if (rows.length < 2) return null;
    rows.sort((a, b) => a.t - b.t);
  }
  const series = res.fields.slice(1).map((name, i) => ({ name, values: rows.map(r => r.values[i]) }));
  if (series.every(s => s.values.every(v => v === null))) return null;
  return { kind, label: res.fields[0], series, rows, step: kind === 'column' ? step(rows) : 0 };
}

// step is the interval the rows are usually apart — a day, a week, a
// month — read as the median gap, so that one missing day does not
// redefine it. It is how wide one column's slot is.
function step(rows) {
  const gaps = rows.slice(1).map((r, i) => r.t - rows[i].t).filter(g => g > 0).sort((a, b) => a - b);
  return gaps.length ? gaps[Math.floor(gaps.length / 2)] : 0;
}

function num(v) {
  if (v === 'NULL' || v === '' || v === undefined) return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

// groups splits the series into the charts they are drawn on: one shared
// axis, unless one series would flatten another.
export function groups(series) {
  const peak = s => Math.max(0, ...s.values.filter(v => v !== null).map(Math.abs));
  const peaks = series.map(peak).filter(p => p > 0);
  if (series.length < 2 || !peaks.length || Math.max(...peaks) / Math.min(...peaks) <= SCALE_RATIO) return [series];
  return series.map(s => [s]);
}

// ticks returns round values spanning [lo, hi], about n of them.
export function ticks(lo, hi, n = 4) {
  if (lo === hi) hi = lo + 1;
  const raw = (hi - lo) / n;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map(m => m * mag).find(s => s >= raw);
  const out = [];
  const end = Math.ceil(hi / step) * step;
  for (let v = Math.floor(lo / step) * step; v <= end + step * 1e-9; v += step) out.push(Number(v.toPrecision(12)));
  return out;
}

export function fmtNum(v) {
  if (v === null) return '—';
  const a = Math.abs(v);
  if (a >= 1e12) return (v / 1e12).toFixed(1).replace(/\.0$/, '') + '兆';
  if (a >= 1e8) return (v / 1e8).toFixed(1).replace(/\.0$/, '') + '億';
  if (a >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万';
  return Number.isInteger(v) ? v.toLocaleString('ja-JP') : v.toLocaleString('ja-JP', { maximumFractionDigits: 2 });
}

const esc = s => String(s).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

// svg draws one chart of the plan's rows with the given series (a group
// from groups). The colour of a series is its column's position in the
// result, so a series keeps its colour whichever chart it lands on.
export function svg(p, group) {
  const slot = s => p.series.indexOf(s) + 1;
  const vals = group.flatMap(s => s.values).filter(v => v !== null);
  const lo = Math.min(0, ...vals), hi = Math.max(0, ...vals);
  const ts = ticks(lo, hi);
  const y0 = ts[0], y1 = ts.at(-1);
  return p.kind === 'column' ? columns(p, group, slot, ts, y0, y1) : bars(p, group, slot, ts, y0, y1);
}

function columns(p, group, slot, ts, y0, y1) {
  const iw = W - PAD.left - PAD.right, ih = H - PAD.top - PAD.bottom;
  const t0 = p.rows[0].t, t1 = p.rows.at(-1).t;
  // Each row owns a slot one step wide, centred on its time, so the
  // first and last columns fit and a missing period is an empty slot.
  const step = p.step || (t1 - t0) / Math.max(1, p.rows.length - 1) || 1;
  const a = t0 - step / 2, b = t1 + step / 2;
  const x = t => PAD.left + (t - a) / (b - a) * iw;
  const y = v => PAD.top + ih - (v - y0) / (y1 - y0) * ih;
  const slotW = iw * step / (b - a), gap = 1;
  const groupW = Math.max(1, slotW * 0.8);
  const cw = Math.max(1, (groupW - gap * (group.length - 1)) / group.length);
  const grid = ts.map(v => `<line class="grid${v === 0 ? ' zero' : ''}" x1="${PAD.left}" x2="${W - PAD.right}" y1="${y(v)}" y2="${y(v)}"/>`
    + `<text class="tick" x="${PAD.left - 6}" y="${y(v) + 4}" text-anchor="end">${esc(fmtNum(v))}</text>`).join('');
  const xt = [0, Math.floor((p.rows.length - 1) / 2), p.rows.length - 1]
    .filter((v, i, arr) => arr.indexOf(v) === i)
    .map((i, k, arr) => `<text class="tick" x="${x(p.rows[i].t)}" y="${H - 8}" text-anchor="${k === 0 ? 'start' : k === arr.length - 1 ? 'end' : 'middle'}">${esc(p.rows[i].label)}</text>`).join('');
  const marks = p.rows.map((r, i) => group.map((s, k) => {
    const v = s.values[i];
    if (v === null) return '';
    const left = x(r.t) - groupW / 2 + k * (cw + gap);
    return `<path class="bar s${slot(s)}" d="${columnPath(left, cw, y(0), y(v))}"><title>${esc(r.label)} · ${esc(s.name)}: ${esc(fmtNum(v))}</title></path>`;
  }).join('')).join('');
  return `<svg class="chart" viewBox="0 0 ${W} ${H}" role="img" aria-label="${esc(group.map(s => s.name).join('・'))} を ${esc(p.label)} ごとに">`
    + grid + marks + xt + '</svg>';
}

// columnPath is a column from the baseline to its value with only the
// data end rounded.
function columnPath(x, w, ya, yb) {
  const r = Math.min(3, w / 2, Math.abs(yb - ya));
  const dir = yb <= ya ? 1 : -1; // up for a positive value
  const e = yb + dir * r;
  return `M${x},${ya}V${e}Q${x},${yb} ${x + r},${yb}H${x + w - r}Q${x + w},${yb} ${x + w},${e}V${ya}Z`;
}

function bars(p, group, slot, ts, y0, y1) {
  const band = 22, gap = 2;
  const bh = Math.max(4, (band - 6 - gap * (group.length - 1)) / group.length);
  const left = 128, iw = W - left - PAD.right;
  const h = PAD.top + p.rows.length * band + PAD.bottom;
  const x = v => left + (v - y0) / (y1 - y0) * iw;
  const grid = ts.map(v => `<line class="grid${v === 0 ? ' zero' : ''}" x1="${x(v)}" x2="${x(v)}" y1="${PAD.top}" y2="${h - PAD.bottom}"/>`
    + `<text class="tick" x="${x(v)}" y="${h - 8}" text-anchor="middle">${esc(fmtNum(v))}</text>`).join('');
  const rows = p.rows.map((r, i) => {
    const top = PAD.top + i * band + 3;
    const label = `<text class="label" x="${left - 8}" y="${top + (band - 6) / 2 + 4}" text-anchor="end">${esc(clip(r.label, 16))}</text>`;
    const marks = group.map((s, k) => {
      const v = s.values[i];
      if (v === null) return '';
      const yy = top + k * (bh + gap);
      return `<path class="bar s${slot(s)}" d="${barPath(x(0), x(v), yy, bh)}"><title>${esc(r.label)} · ${esc(s.name)}: ${esc(fmtNum(v))}</title></path>`;
    }).join('');
    return label + marks;
  }).join('');
  return `<svg class="chart" viewBox="0 0 ${W} ${h}" role="img" aria-label="${esc(group.map(s => s.name).join('・'))} を ${esc(p.label)} ごとに">`
    + grid + rows + '</svg>';
}

// barPath is a bar from the baseline to its value with only the data end
// rounded, so the baseline stays a straight edge.
function barPath(xa, xb, y, h) {
  const r = Math.min(4, Math.abs(xb - xa), h / 2);
  const dir = xb >= xa ? 1 : -1;
  const e = xb - dir * r;
  return `M${xa},${y}H${e}Q${xb},${y} ${xb},${y + r}V${y + h - r}Q${xb},${y + h} ${e},${y + h}H${xa}Z`;
}

function clip(s, n) {
  return [...s].length > n ? [...s].slice(0, n - 1).join('') + '…' : s;
}

// chartHTML is the chart part of a result, or '' when the shape draws
// none. A legend names every series of two or more; one series is named
// by the caption.
export function chartHTML(res) {
  const p = plan(res);
  if (!p) return '';
  const cut = res.rows.length < res.total ? `(先頭 ${res.rows.length} 行)` : '';
  const legend = p.series.length > 1
    ? `<div class="chart-legend">${p.series.map((s, i) => `<span><i class="swatch s${i + 1}"></i>${esc(s.name)}</span>`).join('')}</div>`
    : `<div class="chart-legend"><span>${esc(p.series[0].name)}</span></div>`;
  const charts = groups(p.series).map(g => `<div class="chart-box">${groups(p.series).length > 1 ? `<div class="hint">${esc(g[0].name)}</div>` : ''}${svg(p, g)}</div>`).join('');
  return `<div class="ask-chart">${legend}${charts}${cut ? `<div class="hint">${cut}</div>` : ''}</div>`;
}
