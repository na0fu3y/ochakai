// Who may read and write under which directory (design doc 0109), on
// the surface the person who decides it already uses.
//
// One table to read and one table to edit, and behind both the same
// document. The policy is read as a set, because that is how the
// question arrives — "who can see personnel/" is answered by reading all
// of it — and it is sent as a set too: the save replaces the whole
// document the way `ochakai access -f` does, which is what §5 decided
// and what keeps the wire at one verb instead of three.
//
// What the rows are is the *editing* of that document. Spelling
// `human:` and a pair of booleans into JSON by hand is not what deciding
// a boundary is, and a JSON syntax error is the worst thing to meet
// while doing it. The document stays one disclosure away, because it is
// the form the CLI takes and the form that goes into git.

import { api, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { fmtDate } from '../format.js';
import {
  ACTOR_KINDS, ANY_PRINCIPAL, joinPrincipal, parsePolicyDocument, policyDocument,
  splitPrincipal, validateRules,
} from '../policy.js';
import { knownDirs } from '../tree.js';

// The tab is absent until the deployment answers for it: reading the
// policy is an administrator's operation, so a 403 here is the page
// learning this caller is not one. Same shape as the read-only banner —
// what the page offers comes from the server's own answer and never from
// anything configured in the browser, which is what keeps one page
// servable two ways (design doc 0072 §1).
export async function markAccessTab() {
  try {
    await api('/api/v1/access');
    $('#nav-access').hidden = false;
  } catch (e) { /* not an administrator: the tab stays away */ }
}

export async function viewAccess() {
  view.innerHTML = `<div class="section-title">アクセス</div><div class="empty">…</div>`;
  let rules, version;
  try {
    const got = await api('/api/v1/access');
    rules = got.rules || [];
    version = got.version;
  } catch (e) {
    view.innerHTML = `<div class="section-title">アクセス</div>` + (e.code === 'forbidden'
      ? `<div class="empty">アクセスポリシーを読めるのは、<code>OCHAKAI_ADMINS</code> が名指す管理者だけです。</div>`
      : `<div class="error-banner" role="alert">アクセスポリシーを読み込めませんでした: ${esc(e.message)}</div>`);
    return;
  }
  renderAccess(rules, false, version);
}

function ruleRow(r) {
  const where = r.prefix
    ? `<code class="mono">${esc(r.prefix)}/</code>`
    : `<span style="color:var(--muted)">バンドル全体</span>`;
  const who = r.principal === '*'
    ? `<code class="mono">*</code> <span style="color:var(--muted)">認証された全員</span>`
    : `<code class="mono">${esc(r.principal)}</code>`;
  const by = [r.granted_by ? esc(r.granted_by) : '', fmtDate(r.granted_at)].filter(Boolean).join(' · ');
  return `<tr>
    <td>${where}</td>
    <td>${who}</td>
    <td class="flag">✓</td>
    <td class="flag">${r.may_write ? '✓' : '<span style="color:var(--muted)">—</span>'}</td>
    <td class="flag">${r.may_admin ? '✓' : '<span style="color:var(--muted)">—</span>'}</td>
    <td style="color:var(--muted);font-size:.86rem">${by}</td>
  </tr>`;
}

// What a grant lets its principal do, as one choice instead of two
// booleans an operator has to know the implications between. The order
// is the containment: 書込 includes 読取, 付与 includes 書込.
const PERMS = [
  ['read', '読取のみ'],
  ['write', '読取・書込'],
  ['admin', '読取・書込・付与'],
];

// The actor kinds the ledger spells (design doc 0065 §2), labelled. The
// list comes from policy.js so that a kind Go grows and the page does
// not is the failure TestPrincipalSpellingMatchesDomain already catches,
// rather than an option quietly missing here.
const KIND_LABELS = { human: '人 (human:)', process: 'プロセス (process:)' };

function permOf(r) {
  return r.may_admin === true ? 'admin' : (r.may_write === true ? 'write' : 'read');
}

// One grant as a row of controls. Every cell is the thing the wire
// carries, spelled the way the person deciding it says it out loud: a
// directory, somebody, and what they may do there.
function editRow(r) {
  const prefix = r.prefix || '';
  const { kind, name } = splitPrincipal(r.principal);
  const perm = permOf(r);
  const kinds = ACTOR_KINDS
    .map(k => `<option value="${esc(k)}"${k === kind ? ' selected' : ''}>${esc(KIND_LABELS[k] || k + ':')}</option>`)
    .join('');
  const perms = PERMS
    .map(([v, label]) => `<option value="${v}"${v === perm ? ' selected' : ''}`
      + `${v === 'admin' && !prefix.trim() ? ' disabled' : ''}>${label}</option>`)
    .join('');
  return `<tr>
    <td><input type="text" class="mono" data-f="prefix" list="access-dirs" value="${esc(prefix)}"
          placeholder="バンドル全体" aria-label="ディレクトリ"></td>
    <td class="who">
      <select data-f="kind" aria-label="対象の種類">${kinds}<option value="${ANY_PRINCIPAL}"${kind === ANY_PRINCIPAL ? ' selected' : ''}>認証された全員 (*)</option></select>
      <input type="text" class="mono" data-f="name" value="${esc(name)}" placeholder="tanaka@example.co.jp"
             aria-label="対象の名前"${kind === ANY_PRINCIPAL ? ' hidden' : ''}>
    </td>
    <td><select data-f="perm" aria-label="できること">${perms}</select></td>
    <td><button type="button" class="btn small danger" data-del title="この行を消します">×</button></td>
  </tr>`;
}

// open keeps the editor open across the re-render a save causes, so the
// operator reads back the policy they just sent — which is the whole of
// the advice `ochakai access` gives at the terminal.
// version is what the editor sends back as If-Match: the page saves the
// policy it read, and refuses rather than dropping the rules somebody
// else added in between (design doc 0120).
export function renderAccess(rules, open = false, version = '') {
  const table = rules.length ? `
    <div class="scroll-x">
      <table class="rules">
        <thead><tr><th>ディレクトリ</th><th>対象</th><th class="flag">読取</th><th class="flag">書込</th><th class="flag">付与</th>
          <th>設定者</th></tr></thead>
        <tbody>${rules.map(ruleRow).join('')}</tbody>
      </table>
    </div>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">付与だけの表です。拒否の行はありません。<strong>「書込」は「読取」を、「付与」は「書込」を含みます。</strong>「付与」はそのディレクトリ以下の<em>規則そのもの</em>を編集できるという意味で、その相手には他のディレクトリの規則は見えず、保存しても消えません。バンドル全体への「付与」は置けません — ポリシー全体を誰が編集できるかは <code>OCHAKAI_ADMINS</code> が答えます。ディレクトリはセグメント境界で照合するため、
    <code>sales</code> は <code>sales/orders</code> に当たり、<code>sales-legacy/orders</code> には当たりません。深い行が浅い行を打ち消すことはなく足し算になるので、<code>sales</code> を書ける人は
    <code>sales/sample</code> も書けます。表に無いところは、その人には <strong>404</strong> です。見えないのではなく、無いものとして答えます。</p>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">バンドル全体にまたがる操作
    (統計・エクスポート・<code>move</code>・再埋め込み・このポリシー自身)は管理者のものです。塞げないものが一つあります。読めるナレッジの本文が、隠した id を名指していれば、その id は本文の中で読めてしまいます。この表が約束するのは、一覧・検索・取得・エクスポート・履歴から<em>出てこない</em>ことまでです。</p>`
    : `<div class="empty" style="padding:1.5rem 0">境界はまだありません。</div>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">付与が一行も無いデプロイでは、ここに届いた人が全部を読み書きします。<strong>最初の一行が、全員に対して同時に境界を入れます。</strong>段階的に効かせる仕組みはありません。部門ごとに秘匿しながら全社の用語集は検証済みのまま共有する、といった、デプロイを分けるだけでは実現できない要件のための機構です。</p>`;

  view.innerHTML = `
    <div class="section-title">アクセス</div>
    ${table}
    <div class="read-only-note">この ochakai は read-only のため、ポリシーは表示のみです。知識の側だけが凍結されて境界は動かせる、という状態を作らないためです。</div>
    <details class="write-only" id="access-edit"${open ? ' open' : ''}>
      <summary style="cursor:pointer">編集する</summary>
      <div class="caution">
        <strong>保存が誰として記録されるかは、この UI の配り方で決まります。</strong>
        <code>ochakai ui</code> なら、あなた自身(<code>human:…</code>)です。
        <code>ochakai serve-ui</code> を IAP 無しで置いた場合は、全員が同じサービスアカウント
        (<code>process:…</code>)に畳まれるため、<strong>この URL に届く全員がこのポリシーを編集できます</strong>。ポリシーに履歴はありません。残るのは、いまの表と、それを設定した人だけです。
      </div>
      <p style="color:var(--muted);max-width:48rem;font-size:.9rem">この表が<strong>ポリシーの全部</strong>です。保存すると、いまサーバーにあるものが、ここに並んでいる行で丸ごと置き換わります。行を消して保存すれば、その付与は無くなります。「ディレクトリ」を空にするとバンドル全体で、入力欄には読み込み済みのディレクトリが候補として出ます。</p>
      <div class="scroll-x">
        <table class="rules edit" id="access-rows">
          <thead><tr><th>ディレクトリ</th><th>対象</th><th>できること</th><th></th></tr></thead>
          <tbody>${rules.map(editRow).join('')}</tbody>
        </table>
      </div>
      <datalist id="access-dirs">${knownDirs().map(p => `<option value="${esc(p)}">`).join('')}</datalist>
      <div id="access-empty" class="empty" style="padding:.8rem 0"${rules.length ? ' hidden' : ''}>付与はまだ一行もありません。</div>
      <div class="toolbar"><button class="btn small" id="access-add" type="button">＋ 行を追加</button></div>
      <div id="access-error"></div>
      <details id="access-json">
        <summary style="cursor:pointer;color:var(--muted);font-size:.9rem">JSON として扱う</summary>
        <p style="color:var(--muted);max-width:48rem;font-size:.9rem"><code>ochakai access --json</code> が出力するもの、<code>-f</code> が受け取るものと同じ形式です。上の表をそのまま写しているので、git に入れる文書はここからコピーします。貼り付けたものを表に入れるには「行に取り込む」を押してください — 保存が送るのは、いつでも上の表です。</p>
        <textarea id="access-doc" rows="12" class="mono" spellcheck="false"
          aria-label="アクセスポリシーの文書">${esc(policyDocument(rules))}</textarea>
        <div id="access-json-error"></div>
        <div class="toolbar" style="justify-content:flex-end">
          <button class="btn small" id="access-import" type="button">行に取り込む</button>
        </div>
      </details>
      <div class="toolbar" style="justify-content:flex-end">
        <button class="btn" id="access-reset" type="button">サーバーの内容に戻す</button>
        <button class="btn primary" id="access-save" type="button">保存</button>
      </div>
    </details>`;

  const rowsBody = $('#access-rows tbody');
  if (!rowsBody) return; // read-only: the editor is not on the page

  // The row's own controls, delegated so that a row added after this
  // render behaves like one that came from the server.
  rowsBody.addEventListener('click', e => {
    const del = e.target.closest('[data-del]');
    if (!del) return;
    del.closest('tr').remove();
    afterRowsChanged();
  });
  rowsBody.addEventListener('change', e => {
    const kind = e.target.closest('[data-f="kind"]');
    if (!kind) return;
    // "everybody" has no name to type, so the box goes away rather than
    // sitting there being ignored.
    kind.closest('tr').querySelector('[data-f="name"]').hidden = kind.value === ANY_PRINCIPAL;
  });
  rowsBody.addEventListener('input', e => {
    const prefix = e.target.closest('[data-f="prefix"]');
    if (!prefix) return;
    // Who may edit the whole policy is the one answer the policy cannot
    // carry (design doc 0124), so the option greys out as soon as the
    // row becomes the bundle-wide one. A selection already made is left
    // alone — silently downgrading what somebody chose is worse than the
    // sentence the save gives them.
    const admin = prefix.closest('tr').querySelector('[data-f="perm"] option[value="admin"]');
    admin.disabled = !prefix.value.trim();
    admin.title = admin.disabled ? 'バンドル全体の「付与」は置けません(OCHAKAI_ADMINS が答えます)' : '';
  });

  $('#access-add').addEventListener('click', () => {
    rowsBody.insertAdjacentHTML('beforeend', editRow({ prefix: '', principal: '', may_write: false }));
    afterRowsChanged();
    rowsBody.lastElementChild.querySelector('[data-f="prefix"]').focus();
  });
  $('#access-reset').addEventListener('click', () => {
    rowsBody.innerHTML = rules.map(editRow).join('');
    $('#access-error').innerHTML = '';
    afterRowsChanged();
  });
  $('#access-save').addEventListener('click', () => saveAccess(rules, version));
  // The document is written from the rows every time it is opened, so
  // what is copied out of it is what would be saved — never a stale
  // snapshot of the rows as they were rendered.
  $('#access-json').addEventListener('toggle', e => {
    if (e.target.open) syncDocument();
  });
  $('#access-import').addEventListener('click', () => importDocument());
}

// afterRowsChanged keeps the two things that describe the rows honest:
// the "nothing here yet" line and the document behind the disclosure.
function afterRowsChanged() {
  $('#access-empty').hidden = !!$('#access-rows tbody').children.length;
  if ($('#access-json').open) syncDocument();
}

function syncDocument() {
  $('#access-doc').value = policyDocument(currentRules());
}

function importDocument() {
  const errBox = $('#access-json-error');
  errBox.innerHTML = '';
  let next;
  try {
    next = parsePolicyDocument($('#access-doc').value);
  } catch (e) {
    errBox.innerHTML = `<div class="error-banner" role="alert">${esc(e.message)}</div>`;
    return;
  }
  $('#access-rows tbody').innerHTML = next.map(editRow).join('');
  $('#access-error').innerHTML = '';
  afterRowsChanged();
  toast(`${next.length} 行を表に取り込みました。まだ保存していません。`);
}

// currentRules reads the rows as they stand, without judging them: the
// document view renders half-typed rows too, and the save is where a bad
// one is refused.
function currentRules() {
  return [...$('#access-rows tbody').children].map(tr => {
    const val = f => tr.querySelector(`[data-f="${f}"]`).value;
    const perm = val('perm');
    return {
      prefix: val('prefix'),
      principal: joinPrincipal(val('kind'), val('name')),
      may_write: perm !== 'read',
      ...(perm === 'admin' ? { may_admin: true } : {}),
    };
  });
}

async function saveAccess(rules, version) {
  const errBox = $('#access-error');
  errBox.innerHTML = '';
  [...$('#access-rows tbody').children].forEach(tr => tr.classList.remove('invalid'));
  let next;
  try {
    next = validateRules(currentRules());
  } catch (e) {
    // Which row is wrong is the whole reason this check is in the page:
    // the server validates it again and stays the judge, but only what
    // is in front of the operator can point at the row.
    const tr = e.row ? $('#access-rows tbody').children[e.row - 1] : null;
    if (tr) {
      tr.classList.add('invalid');
      tr.scrollIntoView({ block: 'nearest' });
    }
    errBox.innerHTML = `<div class="error-banner" role="alert">${esc(e.message)}</div>`;
    return;
  }
  // The two crossings that change what the deployment *is*, rather than
  // which side of an existing boundary somebody stands on. Both are one
  // click away, and neither is undoable by reading the page again.
  if (!rules.length && next.length && !confirm(
    'これが最初の一行です。保存すると、いま全部を読み書きできている全員に、同時に境界が入ります。続けますか？')) return;
  if (rules.length && !next.length && !confirm(
    '付与を全部消します。境界そのものが無くなり、ここに届く人はまた全部を読み書きできます。続けますか？')) return;
  try {
    const saved = await api('/api/v1/access',
      { method: 'PUT', body: { rules: next }, ifMatch: version ? `"${version}"` : undefined });
    toast('アクセスポリシーを保存しました。');
    renderAccess(saved.rules || [], true, saved.version);
  } catch (e) {
    // The precondition failed: somebody replaced the policy while this
    // page held it. Saying so is the whole point — the alternative was
    // this save quietly removing their rules.
    //
    // The forbidden branch shows only on a deployment with no grants:
    // reading the policy is open while there are no rules, so this page
    // is here for everybody, and only the save learns that placing the
    // first rule is an administrator's operation (design doc 0122).
    // Nothing was written, and saying so is the difference from what
    // this used to do.
    let message;
    if (e.code === 'precondition_failed') {
      message = 'このページを開いたあとに、別の誰かがポリシーを保存しました。上書きせずに止めています。書いたものは残っているので、ページを読み込み直して編集をやり直してください。';
    } else if (e.code === 'forbidden') {
      message = 'アクセスポリシーを編集できるのは <code>OCHAKAI_ADMINS</code> が名指す管理者だけです。付与が一つも無いあいだは誰でもこのページを読めますが、最初の一行を置けるのは管理者だけで、何も書き込んでいません。';
    } else {
      message = `保存できませんでした: ${esc(e.message)}`;
    }
    errBox.innerHTML = `<div class="error-banner" role="alert">${message}</div>`;
  }
}
