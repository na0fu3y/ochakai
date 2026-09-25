// Running a query the agent proposed, as the person reading it (design
// doc 0142 §4). The server runs nothing and never sees the token: the
// page signs the person in with Google, keeps the token in memory, and
// calls BigQuery itself. What the person may read is their own
// permission; what one query may cost is the cap below.

// read-only: a proposal is a SELECT, and a person granting this page
// more than reading would be granting it more than it uses.
const SCOPE = 'https://www.googleapis.com/auth/bigquery.readonly';

// What one query may bill. A proposal the person did not expect to be
// expensive fails with BigQuery's own message instead of running.
export const MAX_BYTES_BILLED = 10 * 1024 ** 3;

// What travels back into the conversation. The agent needs the shape of
// the answer, not a dump: more than this is cut, and the message says so.
const MAX_ROWS = 50;
const MAX_CHARS = 8000;

let token = null; // { value, expires }

// signIn opens Google's consent screen in a popup and resolves with a
// token when oauth.html, on this origin, hands one back. No script from
// Google is loaded, so the page's script-src stays 'self' (design doc
// 0094 §1).
export function signIn(clientId) {
  if (token && token.expires > Date.now() + 60_000) return Promise.resolve(token.value);
  const state = crypto.randomUUID();
  const url = 'https://accounts.google.com/o/oauth2/v2/auth?' + new URLSearchParams({
    client_id: clientId,
    redirect_uri: location.origin + '/oauth.html',
    response_type: 'token',
    scope: SCOPE,
    include_granted_scopes: 'true',
    state,
  });
  const popup = window.open(url, 'ochakai-oauth', 'width=480,height=640');
  if (!popup) return Promise.reject(new Error('ポップアップが止められました。このサイトのポップアップを許可してください'));
  return new Promise((resolve, reject) => {
    const done = (fn, v) => { clearInterval(watch); window.removeEventListener('message', onMessage); fn(v); };
    const onMessage = e => {
      if (e.origin !== location.origin || e.data?.kind !== 'ochakai-oauth' || e.data.state !== state) return;
      if (e.data.error || !e.data.token) return done(reject, new Error('サインインできませんでした: ' + (e.data.error || 'トークンが無い')));
      token = { value: e.data.token, expires: Date.now() + e.data.expiresIn * 1000 };
      done(resolve, token.value);
    };
    window.addEventListener('message', onMessage);
    // A popup that closes without a token is either the person closing it
    // or Google refusing this origin (redirect_uri_mismatch) — the page
    // cannot tell which, so the message names what an operator would fix.
    const watch = setInterval(() => {
      if (popup.closed && !token) done(reject, new Error(`サインインが閉じられました。Google の画面にエラー(redirect_uri_mismatch)が出ていたなら、このページのアドレス ${location.origin} が OAuth クライアントに登録されていません — 運用者に伝えてください`));
    }, 500);
  });
}

// hasToken says whether a query can run without asking Google again. A
// sign-in opens a popup, and a browser lets a popup open only from a
// click: a query the page runs by itself runs only while this holds.
export function hasToken() {
  return !!token && token.expires > Date.now() + 60_000;
}

// run executes one query in the named billing project and waits for it
// (up to about a minute) — jobs.query answers at once for a small query
// and hands back a job to poll for a slow one. With no token it goes to
// the same paths on this origin, where `ochakai ui` runs it as the person
// and adds the credential itself.
export async function run(tok, project, query) {
  const base = `${tok ? 'https://bigquery.googleapis.com' : ''}/bigquery/v2/projects/${encodeURIComponent(project)}`;
  const call = async (path, init) => {
    const headers = { 'Content-Type': 'application/json' };
    if (tok) headers.Authorization = 'Bearer ' + tok;
    const res = await fetch(base + path, { ...init, headers });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error?.message || `BigQuery ${res.status}`);
    return body;
  };
  let r = await call('/queries', {
    method: 'POST',
    body: JSON.stringify({ query, useLegacySql: false, maximumBytesBilled: String(MAX_BYTES_BILLED), timeoutMs: 20000, maxResults: MAX_ROWS }),
  });
  for (let i = 0; !r.jobComplete && i < 6; i++) {
    const job = r.jobReference;
    r = await call(`/queries/${encodeURIComponent(job.jobId)}?` + new URLSearchParams({
      location: job.location || '', timeoutMs: '10000', maxResults: String(MAX_ROWS),
    }), { method: 'GET' });
  }
  if (!r.jobComplete) throw new Error('クエリが一分以内に終わりませんでした');
  const schema = r.schema?.fields || [];
  const fields = schema.map(f => f.name);
  const types = schema.map(f => (f.mode === 'REPEATED' ? 'REPEATED' : f.type));
  const rows = (r.rows || []).map(row => row.f.map((c, i) => cell(c.v, types[i])));
  return { fields, types, rows, total: Number(r.totalRows || rows.length), bytes: Number(r.totalBytesProcessed || 0) };
}

function cell(v, type) {
  if (v === null || v === undefined) return 'NULL';
  // BigQuery hands a TIMESTAMP back as seconds since the epoch, which
  // neither a person nor the agent reads as a time.
  if (type === 'TIMESTAMP' && /^-?[\d.]+(E\d+)?$/i.test(v)) {
    return new Date(Number(v) * 1000).toISOString().replace('T', ' ').replace(/\.000Z$|Z$/, ' UTC');
  }
  if (typeof v === 'object') return JSON.stringify(v);
  return String(v).replace(/[\t\n]/g, ' ');
}

// asMessage is the result as the person's next message: what ran and
// the rows as tab-separated text the agent can read. The project is not
// said — it bills the query and changes nothing the agent reads from it.
export function asMessage(query, res) {
  let table = [res.fields.join('\t'), ...res.rows.map(r => r.join('\t'))].join('\n');
  let cut = res.rows.length < res.total;
  if (table.length > MAX_CHARS) { table = table.slice(0, MAX_CHARS); cut = true; }
  return `実行 SQL(${fmtBytes(res.bytes)} 読み取り)\n\n`
    + '```sql\n' + query + '\n```\n\n'
    + `結果(全 ${res.total} 行${cut ? '、先頭だけ' : ''}):\n\n` + '```\n' + table + '\n```';
}

// asFailure is a query that did not run, as the person's next message:
// the SQL and what BigQuery said, so the agent can correct it.
export function asFailure(query, err) {
  return '実行できませんでした\n\n```sql\n' + query + '\n```\n\nエラー: ' + err.message;
}

// The server refuses a conversation carrying more text than this
// (internal/agent). A conversation that runs a few queries reaches it on
// the results it carries back rather than on anything a person wrote, so
// fold drops the oldest results first — keeping the SQL that produced
// each, and the newest result whole — until the conversation fits. What
// the agent concluded from a folded result is still in its own answer.
export const MAX_CONVERSATION_BYTES = 64 << 10;
const FOLDED = '結果: 会話の長さの上限のため省きました。この SQL は実行済みで、そのあとの答えは結果を読んで書かれています。';

// fold takes the conversation as it will be sent — {role, text, result}
// where result marks a message asMessage wrote — and returns it with as
// many of the older results folded as it takes to fit in max bytes.
export function fold(msgs, max = MAX_CONVERSATION_BYTES) {
  const enc = new TextEncoder();
  const out = msgs.map(m => ({ ...m }));
  let size = out.reduce((n, m) => n + enc.encode(m.text).length, 0);
  const results = out.flatMap((m, i) => (m.result ? [i] : [])).slice(0, -1);
  for (const i of results) {
    if (size <= max) break;
    const folded = out[i].text.replace(/\n\n結果\(全 [\s\S]*$/, '\n\n' + FOLDED);
    size += enc.encode(folded).length - enc.encode(out[i].text).length;
    out[i].text = folded;
  }
  return out;
}

export function fmtBytes(n) {
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
}
