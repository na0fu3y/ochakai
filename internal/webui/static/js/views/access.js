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
  let rules;
  try {
    rules = (await api('/api/v1/access')).rules || [];
  } catch (e) {
    view.innerHTML = `<div class="section-title">アクセス</div>` + (e.code === 'forbidden'
      ? `<div class="empty">アクセスポリシーを読めるのは、<code>OCHAKAI_ADMINS</code> が名指す管理者だけです。</div>`
      : `<div class="error-banner" role="alert">アクセスポリシーを読み込めませんでした: ${esc(e.message)}</div>`);
    return;
  }
  renderAccess(rules);
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
    <td style="color:var(--muted);font-size:.86rem">${by}</td>
  </tr>`;
}

// open keeps the editor open across the re-render a save causes, so the
// operator reads back the document they just sent — which is the whole
// of the advice `ochakai access` gives at the terminal.
export function renderAccess(rules, open = false) {
  const table = rules.length ? `
    <div class="scroll-x">
      <table class="rules">
        <thead><tr><th>ディレクトリ</th><th>誰が</th><th class="flag">読む</th><th class="flag">書く</th>
          <th>置いた人</th></tr></thead>
        <tbody>${rules.map(ruleRow).join('')}</tbody>
      </table>
    </div>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">付与だけの表です — 拒否の行はありません。
    <strong>書ける は 読める を含みます。</strong>ディレクトリはセグメント境界で照合するので、
    <code>sales</code> は <code>sales/orders</code> に当たり <code>sales-legacy/orders</code> には当たりません。
    深い行は浅い行を打ち消さない足し算なので、<code>sales</code> を書ける人は
    <code>sales/sample</code> も書けます。表に無いところは、その人には <strong>404</strong> —
    見えないのではなく、無いものとして答えます。</p>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">バンドル全体を取る操作
    (統計・エクスポート・<code>move</code>・再埋め込み・このポリシー自身)は管理者のものです。
    塞げないものが一つあります: 読める concept の本文が隠した id を名指していれば、その id は本文の中で読めます。
    この表が約束するのは、一覧・検索・取得・エクスポート・履歴から<em>出てこない</em>ことまでです。</p>`
    : `<div class="empty" style="padding:1.5rem 0">境界はまだありません。</div>
    <p style="color:var(--muted);max-width:48rem;font-size:.9rem">付与が一行も無いデプロイでは、
    ここに届いた人は全部を読み書きします。<strong>最初の一行が、全員に対して同時に境界を入れます</strong> —
    段階的に効かせる仕組みはありません。部門ごとに秘匿しながら全社の用語集は検証済みのまま共有する、
    といった、デプロイを分けても買えないものが要るときの機構です。</p>`;

  view.innerHTML = `
    <div class="section-title">アクセス</div>
    ${table}
    <div class="read-only-note">この ochakai は read-only なので、ポリシーは表示だけです。
    凍結されているのが知識の側だけで境界は動かせる、という状態を作らないためです。</div>
    <details class="write-only" id="access-edit"${open ? ' open' : ''}>
      <summary style="cursor:pointer">文書として編集</summary>
      <div class="caution">
        <strong>保存が誰として記録されるかは、この UI の配り方で決まります。</strong>
        <code>ochakai ui</code> なら、あなた自身(<code>human:…</code>)です。
        <code>ochakai serve-ui</code> を IAP 無しで置いた場合は、全員が同じサービスアカウント
        (<code>process:…</code>)に畳まれるので、<strong>この URL に届く全員がこのポリシーを編集できます</strong>。
        ポリシーに履歴はありません — 残るのは、いまの表と、それを置いた人だけです。
      </div>
      <p style="color:var(--muted);max-width:48rem;font-size:.9rem">文書を丸ごと置き換えます。
      <code>ochakai access --json</code> が出すもの、<code>-f</code> が受け取るものと同じ形です。
      <code>prefix</code> の <code>""</code> はバンドル全体、<code>principal</code> は
      <code>human:&lt;名前&gt;</code>・<code>process:&lt;名前&gt;</code>・<code>*</code>。</p>
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
  $('#access-save')?.addEventListener('click', () => saveAccess(rules));
}

async function saveAccess(rules) {
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
    'これが最初の一行です。保存すると、いま全部を読み書きできている全員に、同時に境界が入ります。続けますか?')) return;
  if (rules.length && !next.length && !confirm(
    '付与を全部消します。境界そのものが無くなり、ここに届く人はまた全部を読み書きできます。続けますか?')) return;
  try {
    const saved = await api('/api/v1/access', { method: 'PUT', body: { rules: next } });
    toast('アクセスポリシーを保存しました。');
    renderAccess(saved.rules || [], true);
  } catch (e) {
    errBox.innerHTML = `<div class="error-banner" role="alert">保存できませんでした: ${esc(e.message)}</div>`;
  }
}
