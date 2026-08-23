// Who may read and write under which directory (design doc 0109), on
// the surface the person who decides it already uses.
//
// One table and one document. The table is the policy as a set, because
// that is how the question arrives — "who can see personnel/" is
// answered by reading all of it — and the document behind the
// disclosure is the same one `ochakai access -f` takes, so what an
// operator reviews is what is sent. Rows with their own add and remove
// buttons were the other shape and it costs a surface per verb while
// still needing a way to review the whole (§5).

import { api, toast } from '../api.js';
import { $, view } from '../dom.js';
import { esc } from '../escape.js';
import { fmtDate } from '../format.js';
import { parsePolicyDocument, policyDocument } from '../policy.js';

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

// open keeps the editor open across the re-render a save causes, so the
// operator reads back the document they just sent — which is the whole
// of the advice `ochakai access` gives at the terminal.
// version is what the editor sends back as If-Match: the page saves the
// document it read, and refuses rather than dropping the rules somebody
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
      <summary style="cursor:pointer">文書として編集</summary>
      <div class="caution">
        <strong>保存が誰として記録されるかは、この UI の配り方で決まります。</strong>
        <code>ochakai ui</code> なら、あなた自身(<code>human:…</code>)です。
        <code>ochakai serve-ui</code> を IAP 無しで置いた場合は、全員が同じサービスアカウント
        (<code>process:…</code>)に畳まれるため、<strong>この URL に届く全員がこのポリシーを編集できます</strong>。ポリシーに履歴はありません。残るのは、いまの表と、それを設定した人だけです。
      </div>
      <p style="color:var(--muted);max-width:48rem;font-size:.9rem">文書を丸ごと置き換えます。
      <code>ochakai access --json</code> が出力するもの、<code>-f</code> が受け取るものと同じ形式です。
      <code>prefix</code> の <code>""</code> はバンドル全体、<code>principal</code> は
      <code>human:&lt;名前&gt;</code>・<code>process:&lt;名前&gt;</code>・<code>*</code> です。</p>
      <textarea id="access-doc" rows="16" class="mono" spellcheck="false"
        aria-label="アクセスポリシーの文書">${esc(policyDocument(rules))}</textarea>
      <div id="access-error"></div>
      <div class="toolbar" style="justify-content:flex-end">
        <button class="btn" id="access-reset" type="button">サーバーの内容に戻す</button>
        <button class="btn primary" id="access-save" type="button">保存</button>
      </div>
    </details>`;

  $('#access-reset')?.addEventListener('click', () => {
    $('#access-doc').value = policyDocument(rules);
    $('#access-error').innerHTML = '';
  });
  $('#access-save')?.addEventListener('click', () => saveAccess(rules, version));
}

async function saveAccess(rules, version) {
  const errBox = $('#access-error');
  errBox.innerHTML = '';
  let next;
  try {
    next = parsePolicyDocument($('#access-doc').value);
  } catch (e) {
    errBox.innerHTML = `<div class="error-banner" role="alert">${esc(e.message)}</div>`;
    return;
  }
  // The two crossings that change what the deployment *is*, rather than
  // which side of an existing boundary somebody stands on. Both are one
  // click from an empty textarea, and neither is undoable by reading the
  // page again.
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
