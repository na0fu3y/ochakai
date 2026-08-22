// The language of what a reader sees. Nothing else in this repository
// reads the page's copy: the tests beside this one hold behaviour, and
// the browser smoke asserts DOM markers and words out of the corpus. A
// page half-translated into Japanese passed every one of them, which is
// how it shipped that way once.
//
// The rules are declared in CONTRIBUTING.md ("Web UI の文言") rather
// than here, for the reason RECORD-LINES is: the vocabulary a writer
// consults and the vocabulary the check enforces have to be one list,
// or the check becomes a second opinion nobody reads.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import { copyFragments, copyOf, visible, walk } from './copy.mjs';

const STATIC = fileURLToPath(new URL('../static', import.meta.url));
const CONTRIBUTING = fileURLToPath(new URL('../../../CONTRIBUTING.md', import.meta.url));
const files = walk(STATIC);
const rel = f => f.slice(STATIC.length + 1);

test('there is copy to check at all', () => {
  // The extraction is the whole of this file's leverage, and it is
  // regex-shaped: a page reorganised into a shape it cannot read would
  // turn every assertion below green by finding nothing.
  assert.ok(files.length >= 20, `only ${files.length} files under static/`);
  const total = files.reduce((n, f) => n + copyOf(f).length, 0);
  assert.ok(total > 8000, `only ${total} characters of copy found; the extractor has stopped seeing the page`);
});

// The terms that stay English inside Japanese copy, declared in
// CONTRIBUTING.md as one line-wrapped list ending at a blank line.
function englishTerms() {
  const md = readFileSync(CONTRIBUTING, 'utf8');
  const m = md.match(/^ *COPY-ENGLISH-TERMS:([\s\S]*?)\n *\n/m);
  assert.ok(m, `${CONTRIBUTING} declares no COPY-ENGLISH-TERMS: this check now guards nothing`);
  return new Set(m[1].trim().split(/\s+/).map(t => t.toLowerCase()));
}

// Two or more English words in a row, tolerating the comma or period
// between them — "reported wrong, not verified" is one run, and a rule
// that let punctuation break it would pass exactly the half-translated
// sentence this file exists to catch.
const ENGLISH_RUN = /[A-Za-z][A-Za-z'-]*(?:[,.]? +[A-Za-z][A-Za-z'-]*)+/g;

test('no untranslated English phrase reaches a reader', () => {
  const allowed = englishTerms();
  const found = [];
  for (const f of files) {
    for (const run of copyOf(f).match(ENGLISH_RUN) || []) {
      // A run survives only if every word in it is a declared term. That
      // is what separates a product name from a sentence: English prose
      // carries ordinary words, and one of them is never on the list.
      const words = run.split(/[,.]? +/).map(w => w.toLowerCase());
      if (!words.every(w => allowed.has(w))) found.push(`${rel(f)}: ${run}`);
    }
  }
  assert.deepEqual(found, [],
    'English left in the UI copy. Translate it, or add the term to COPY-ENGLISH-TERMS in CONTRIBUTING.md if it is a name the reader types.');
});

test('no plural branch survives — Japanese has no plural', () => {
  // `n === 1 ? 'draft' : 'drafts'` is a reliable mark of copy nobody
  // translated: the branch cannot survive a translation, because the
  // language it is branching for does not inflect.
  const TERNARY = /\?\s*(['"])([A-Za-z][A-Za-z ]*)\1\s*:\s*(['"])([A-Za-z][A-Za-z ]*)\3/g;
  const found = [];
  for (const f of files) {
    const src = readFileSync(f, 'utf8');
    for (const m of src.matchAll(TERNARY)) {
      const [a, b] = [m[2], m[4]];
      if (b === a + 's' || a === b + 's') found.push(`${rel(f)}: ${m[0]}`);
    }
  }
  assert.deepEqual(found, [], 'a plural branch is untranslated copy');
});

test('a question mark after Japanese is full width', () => {
  // Mixed widths are the one punctuation rule this page needs of its
  // own: the repository writes half-width parentheses everywhere
  // (CONTRIBUTING.md), and nothing in it had ever written a question
  // mark, so the UI was the only place with a rule to pick.
  const found = [];
  for (const f of files) {
    for (const fragment of copyFragments(f)) {
      for (const m of visible(fragment).matchAll(/[ぁ-んァ-ヶ一-龥][?!]/g)) found.push(`${rel(f)}: …${m[0]}`);
    }
  }
  assert.deepEqual(found, [], 'use ？ or ！ after Japanese, not the half-width form');
});

test('a Japanese paragraph is one line, because a break becomes a space', () => {
  // A newline in the source between two Japanese characters is rendered
  // as a space. Browsers do not apply the segment-break removal that
  // would have saved this — measured, not assumed — and a half-width
  // space mid-sentence is conspicuous in a language that has none. The
  // page carried 61 of them, three in the first paragraph of the home
  // page, and no test could see one.
  //
  // Only the break is checked. A literal space next to a `${...}` cannot
  // be judged from here: `ナレッジ ${n} 件` is correct and
  // `${fmtDate(d)} に` was not, and nothing static tells them apart.
  const INLINE = /<\/?(?:strong|em|code|a|b|i|span)\b[^>]*>/gi;
  const found = [];
  for (const f of files) {
    for (const fragment of copyFragments(f)) {
      const text = fragment.replace(INLINE, '');
      for (const m of text.matchAll(/([ぁ-んァ-ヶ一-龥、。])[ \t]*\n[ \t]*([ぁ-んァ-ヶ一-龥「（])/g)) {
        found.push(`${rel(f)}: ${m[1]}⏎${m[2]}`);
      }
    }
  }
  assert.deepEqual(found, [], 'join the line: the break renders as a space inside the sentence');
});

test('the reader is told ナレッジ, never concept', () => {
  // One word for one thing (CONTRIBUTING.md's 用語表). `concept` is the
  // wire's word and stays in ids, tool names and the API; the person
  // reading the page is shown ナレッジ. The lookaround spares
  // put_concept and search_concepts, which are names, not prose.
  const found = [];
  for (const f of files) {
    for (const m of copyOf(f).matchAll(/(?<![\w_-])concepts?(?![\w_-])/gi)) found.push(`${rel(f)}: ${m[0]}`);
  }
  assert.deepEqual(found, [], 'UI copy says ナレッジ; concept is the wire\'s word');
});
