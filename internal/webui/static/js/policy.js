// The access policy as rows (design doc 0109 §5): the grants an operator
// edits, and the validation they go through before being sent.
//
// A module of its own, importing nothing a browser has to provide, for
// the reason documents.js is one — the normalization is what a test can
// hold, and this is the half of the access view worth holding.

// The spellings a grant can name, mirroring domain.ValidPrincipal.
// TestPrincipalSpellingMatchesDomain holds the kinds against Go, because
// a page that accepted a kind the server does not would turn a typo into
// a 400 the operator has to read backwards from.
export const ACTOR_KINDS = ['human', 'process'];
export const ANY_PRINCIPAL = '*';

// validateRules normalizes the rows the form holds into the rules the
// wire takes, or throws a sentence naming the row that is wrong. The
// server validates all of this again and stays the only judge — this is
// here because it can say *which row*, and what is in front of the
// operator is a thing with rows.
//
// The error carries the position it is about (`row`, 1-based) as well as
// naming it in the sentence.
export function validateRules(list) {
  const seen = new Set();
  return (list || []).map((r, i) => {
    const n = i + 1;
    if (!r || typeof r !== 'object' || Array.isArray(r)) {
      throw rowError(n, `${n} 番目の付与がオブジェクトではありません。`);
    }
    const raw = r.prefix ?? '';
    if (typeof raw !== 'string') {
      throw rowError(n, `${n} 番目の prefix が文字列ではありません。バンドル全体は "" です。`);
    }
    if (typeof r.principal !== 'string' || !validPrincipal(r.principal.trim())) {
      throw rowError(n, `${n} 番目の principal は human:<名前>・process:<名前>・"*" のいずれかで書いてください。`);
    }
    if (r.may_write !== undefined && typeof r.may_write !== 'boolean') {
      throw rowError(n, `${n} 番目の may_write が true / false ではありません。`);
    }
    if (r.may_admin !== undefined && typeof r.may_admin !== 'boolean') {
      throw rowError(n, `${n} 番目の may_admin が true / false ではありません。`);
    }
    if (r.may_admin === true && trimPrefix(raw) === '') {
      throw rowError(n, `${n} 番目: バンドル全体の「付与」は置けません。ポリシー全体を編集できるのは誰かという答えは、そのポリシーの中には置けないためです(OCHAKAI_ADMINS で名指します)。`);
    }
    const rule = {
      prefix: trimPrefix(raw),
      principal: r.principal.trim(),
      // may_admin implies may_write, the way may_write implies reading.
      may_write: r.may_write === true || r.may_admin === true,
      ...(r.may_admin === true ? { may_admin: true } : {}),
    };
    const key = rule.prefix + '\u0000' + rule.principal;
    if (seen.has(key)) {
      throw rowError(n, `${rule.principal} への ${rule.prefix || '(バンドル全体)'} の付与が二行あります。`
        + '一行にまとめてください(「書込」は「読取」を含みます)。');
    }
    seen.add(key);
    return rule;
  });
}

function rowError(n, message) {
  const e = new Error(message);
  e.row = n;
  return e;
}

// The same trim the server does before it stores the prefix, so the
// duplicate above is caught against the row as it will be kept rather
// than as it was typed: "sales/" and "sales" are one grant.
function trimPrefix(s) {
  return s.trim().replace(/^\/+|\/+$/g, '');
}

// validPrincipal is the wildcard or one of the two actor kinds with a
// name after it. A bare email is refused rather than guessed at, the way
// the server refuses it: "tanaka@example.com" and "human:tanaka@…" would
// be two spellings of one grant and only one of them would ever match.
export function validPrincipal(s) {
  if (s === ANY_PRINCIPAL) return true;
  const i = s.indexOf(':');
  return i > 0 && ACTOR_KINDS.includes(s.slice(0, i)) && s.slice(i + 1) !== '' && !/\s/.test(s);
}

// The two halves the row editor asks for separately. The wire spells a
// principal as one string and the ledger compares it as one (0065 §2),
// so the split lives here beside the spelling rule rather than in the
// view — the kind is a choice with three answers and the name is free
// text, and only one of the two can be typed wrong.
export function splitPrincipal(s) {
  const t = (s || '').trim();
  if (t === ANY_PRINCIPAL) return { kind: ANY_PRINCIPAL, name: '' };
  const i = t.indexOf(':');
  if (i > 0 && ACTOR_KINDS.includes(t.slice(0, i))) {
    return { kind: t.slice(0, i), name: t.slice(i + 1) };
  }
  // Not a spelling the server would have stored. It arrives as the name
  // under the first kind rather than being dropped, so an operator sees
  // what is there — and validateRules still refuses it if they leave it.
  return { kind: ACTOR_KINDS[0], name: t };
}

export function joinPrincipal(kind, name) {
  return kind === ANY_PRINCIPAL ? ANY_PRINCIPAL : `${kind}:${(name || '').trim()}`;
}
