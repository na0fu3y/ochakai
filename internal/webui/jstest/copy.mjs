// The strings a reader actually sees, pulled out of static/ — the half
// of the page no other test looks at.
//
// A module of its own because the extraction is the hard part and the
// assertions on top of it are one line each: what a reader sees is the
// contents of a string literal, never the code around it, and inside a
// literal it is the text between the tags plus the attributes that are
// themselves copy.

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

export function walk(dir, out = []) {
  for (const name of readdirSync(dir).sort()) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (name.endsWith('.js') || name.endsWith('.html')) out.push(p);
  }
  return out;
}

// Where a `/` opens a regular expression rather than dividing: after an
// operator, a comma, an opening bracket, or at the start of something.
// The distinction has to be made — `/`([^`]+)`/g` in markdown.js holds
// two backticks, and a scan that took them for a template literal read
// the rest of that module inside out.
const BEFORE_REGEX = new Set([...'(,=:[!&|?{};+-*%~^<>', undefined]);
const KEYWORD_BEFORE_REGEX = /(?:^|[^\w$])(?:return|typeof|case|in|of|do|else)$/;

// jsLiterals returns every string literal in a module, with each
// `${...}` expression replaced by a space: a template literal is copy
// interleaved with code, and only the copy half is a reader's.
//
// Comments are skipped rather than returned. They are English by house
// rule (CLAUDE.md), so leaving them in would make every check below
// fail on prose that is meant to be English.
export function jsLiterals(src) {
  const out = [];
  let i = 0;
  let prev; // last significant character — what decides `/` below
  let prevEnd = 0; // and where it sat, for the keyword lookbehind
  const significant = (ch, at) => { prev = ch; prevEnd = at + 1; };
  while (i < src.length) {
    const c = src[i];
    if (c === '/' && src[i + 1] === '/') {
      while (i < src.length && src[i] !== '\n') i++;
      continue;
    }
    if (c === '/' && src[i + 1] === '*') {
      const end = src.indexOf('*/', i + 2);
      i = end === -1 ? src.length : end + 2;
      continue;
    }
    if (c === '/' && (BEFORE_REGEX.has(prev) || KEYWORD_BEFORE_REGEX.test(src.slice(0, prevEnd)))) {
      i++;
      let inClass = false;
      while (i < src.length) {
        if (src[i] === '\\') { i += 2; continue; }
        if (src[i] === '[') inClass = true;
        else if (src[i] === ']') inClass = false;
        else if (src[i] === '/' && !inClass) break;
        else if (src[i] === '\n') break; // unterminated: not a regex after all
        i++;
      }
      i++;
      while (i < src.length && /[a-z]/.test(src[i])) i++; // flags
      significant('/', i - 1);
      continue;
    }
    if (c === "'" || c === '"' || c === '`') {
      const quote = c;
      let buf = '';
      i++;
      while (i < src.length && src[i] !== quote) {
        if (src[i] === '\\') { i += 2; continue; }
        if (quote === '`' && src[i] === '$' && src[i + 1] === '{') {
          let depth = 1;
          i += 2;
          const start = i;
          while (i < src.length && depth > 0) {
            if (src[i] === '{') depth++;
            else if (src[i] === '}') depth--;
            i++;
          }
          // The expression is code, so it leaves a space in the text
          // around it — but a ternary picking between two sentences is
          // copy living inside code, and that is where the tooltips this
          // page half-translated once were hiding. Read it as a module
          // of its own rather than dropping it.
          out.push(...jsLiterals(src.slice(start, Math.max(start, i - 1))));
          buf += ' ';
          continue;
        }
        buf += src[i++];
      }
      i++;
      out.push(buf);
      significant(quote, i - 1);
      continue;
    }
    if (!/\s/.test(c)) significant(c, i);
    i++;
  }
  return out;
}

// The attributes that carry copy rather than markup. An aria-label is
// read out loud, so it is as much a reader's string as the text beside
// it — and half-translated attributes are exactly what the first pass
// over this page left behind.
const COPY_ATTRS = /(?:title|aria-label|placeholder|alt)="([^"]*)"/g;

export function visible(fragment) {
  const parts = [];
  for (const m of fragment.matchAll(COPY_ATTRS)) parts.push(m[1]);
  parts.push(fragment.replace(/<[^>]*>/g, ' '));
  return parts.join('   ');
}

const JAPANESE = /[ぁ-んァ-ヶ一-龥々〆〜「」、。]/;
const MARKUP = /<[a-z][^>]*>/i;
// The three calls that put a bare sentence in front of a reader without
// any markup around it. A string reaching one of them is copy however it
// is spelled — which is the case that matters, because a toast left in
// English carries neither a Japanese character nor a tag to be caught by.
const ANNOUNCED = /\b(?:toast|confirm|prompt|alert)\(\s*(['"`])((?:\\.|(?!\1)[\s\S])*?)\1/g;

// copyFragments is the subset of a module's literals that a reader sees.
// Most string literals in this page are not copy — they are selectors,
// event names, header values, and the vocabularies the server stores —
// and a check that read them all would drown the one it exists for.
export function copyFragments(path) {
  const src = readFileSync(path, 'utf8');
  if (path.endsWith('.html')) return [src.replace(/<!--[\s\S]*?-->/g, ' ')];
  const out = jsLiterals(src).filter(s => JAPANESE.test(s) || MARKUP.test(s));
  for (const m of src.matchAll(ANNOUNCED)) out.push(m[2]);
  return out;
}

export function copyOf(path) {
  return copyFragments(path).map(visible).join('   ');
}
