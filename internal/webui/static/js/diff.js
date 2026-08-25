// What changed between two revisions of a document, line by line. The
// history tab holds every revision whole (design doc 0046 §3.5), and a
// reviewer deciding whether to verify wants the three lines that moved,
// not two full documents to read side by side — which is how every wiki
// shows its history, and why this one does too.
//
// Pure text in, pure markup out: nothing here touches the network or the
// document, so a test can hold it.

import { esc } from './escape.js';

// Above this many changed lines a side, the quadratic middle would cost
// more than the answer is worth; the fallback below states the change as
// a replacement, which for a rewrite that big is also the truth.
const LCS_MAX = 500;

// diffLines compares two documents and returns every line once, in
// order, marked same / add / del. Common prefix and suffix are peeled
// first — most edits touch a few lines in the middle of an unchanged
// document — and the middle is aligned on a longest common subsequence.
export function diffLines(before, after) {
  // An empty document has no lines — not one empty line. It matters at
  // the oldest revision, whose diff is against nothing: the whole
  // document is additions, with no phantom deletion in front.
  const split = s => String(s ?? '') === '' ? [] : String(s).split('\n');
  const a = split(before);
  const b = split(after);
  let lo = 0;
  while (lo < a.length && lo < b.length && a[lo] === b[lo]) lo++;
  let aHi = a.length, bHi = b.length;
  while (aHi > lo && bHi > lo && a[aHi - 1] === b[bHi - 1]) { aHi--; bHi--; }
  const ops = [];
  for (let i = 0; i < lo; i++) ops.push({ op: 'same', text: a[i] });
  const mid = lcsOps(a.slice(lo, aHi), b.slice(lo, bHi));
  ops.push(...mid);
  for (let i = aHi; i < a.length; i++) ops.push({ op: 'same', text: a[i] });
  return ops;
}

// The aligned middle: deletions before additions, because a change reads
// as "this went, that came". Past LCS_MAX a side, the whole middle is
// one replacement.
function lcsOps(a, b) {
  if (!a.length || !b.length || a.length > LCS_MAX || b.length > LCS_MAX) {
    return [...a.map(text => ({ op: 'del', text })), ...b.map(text => ({ op: 'add', text }))];
  }
  // len[i][j]: the longest common subsequence of a[i:] and b[j:].
  const w = b.length + 1;
  const len = new Int32Array((a.length + 1) * w);
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      len[i * w + j] = a[i] === b[j]
        ? len[(i + 1) * w + j + 1] + 1
        : Math.max(len[(i + 1) * w + j], len[i * w + j + 1]);
    }
  }
  const ops = [];
  let i = 0, j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { ops.push({ op: 'same', text: a[i] }); i++; j++; }
    else if (len[(i + 1) * w + j] >= len[i * w + j + 1]) ops.push({ op: 'del', text: a[i++] });
    else ops.push({ op: 'add', text: b[j++] });
  }
  while (i < a.length) ops.push({ op: 'del', text: a[i++] });
  while (j < b.length) ops.push({ op: 'add', text: b[j++] });
  return ops;
}

// The two numbers a history row leads with.
export function diffStats(ops) {
  let added = 0, removed = 0;
  for (const o of ops) {
    if (o.op === 'add') added++;
    else if (o.op === 'del') removed++;
  }
  return { added, removed };
}

// diffHTML renders the changed hunks with `context` unchanged lines
// around each, eliding the rest — the whole document is one disclosure
// away, so what the diff owes the reader is only the change. Two
// unchanged documents return nothing, and the caller says so in words.
export function diffHTML(before, after, context = 2) {
  const ops = diffLines(before, after);
  if (!ops.some(o => o.op !== 'same')) return '';
  // keep[i]: this line is a change or sits within `context` of one.
  const keep = new Array(ops.length).fill(false);
  for (let i = 0; i < ops.length; i++) {
    if (ops[i].op === 'same') continue;
    for (let k = Math.max(0, i - context); k <= Math.min(ops.length - 1, i + context); k++) keep[k] = true;
  }
  const MARK = { same: ' ', add: '+', del: '-' };
  let out = '', skipped = 0;
  const flushSkip = () => {
    if (skipped) out += `<span class="ln skip">⋯ ${skipped} 行</span>`;
    skipped = 0;
  };
  for (let i = 0; i < ops.length; i++) {
    if (!keep[i]) { skipped++; continue; }
    flushSkip();
    out += `<span class="ln ${ops[i].op}">${MARK[ops[i].op]} ${esc(ops[i].text)}</span>`;
  }
  flushSkip();
  return `<pre class="diff mono">${out}</pre>`;
}
