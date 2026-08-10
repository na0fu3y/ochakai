// A browser smoke over the served page: every route renders, and nothing
// throws on the way. This is the test the module split was made for — a
// module that fails to load leaves a blank panel and a console error,
// and until there was a browser in the loop neither was visible to CI.
//
// Driven over the Chrome DevTools Protocol with Node's own WebSocket, so
// the whole harness is this file: no package.json, no lockfile, no
// node_modules in a Go repository. What it needs is a browser and a
// running `ochakai ui`, and it says which one is missing.
//
//   node internal/webui/jstest/smoke.mjs http://127.0.0.1:8098
//
// The concepts it looks for are examples/demo's, which is what CI
// imports before running this.

import { spawn } from 'node:child_process';
import { accessSync, mkdtempSync, readdirSync, constants } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = (process.argv[2] || 'http://127.0.0.1:8098').replace(/\/+$/, '');
const PORT = process.env.OCHAKAI_SMOKE_CDP_PORT || '9222';
// A missing browser or a server that never came up is a skip on a laptop
// and a failure in CI, where a silent skip is a green run that tested
// nothing. CI sets this; nothing else does.
const REQUIRED = !!process.env.OCHAKAI_SMOKE_REQUIRED;

function unavailable(why) {
  console.error('smoke: ' + why + (REQUIRED ? '' : '; skipping'));
  process.exit(REQUIRED ? 1 : 0);
}

// $CHROME first, so a machine with an unusual one says so once instead
// of being guessed at. The rest are the spellings the common images use;
// PLAYWRIGHT_BROWSERS_PATH covers a devcontainer that has one installed
// for other reasons.
function findBrowser() {
  const pw = process.env.PLAYWRIGHT_BROWSERS_PATH;
  const candidates = [
    process.env.CHROME,
    'chromium', 'chromium-browser', 'google-chrome', 'google-chrome-stable',
    '/usr/bin/chromium', '/usr/bin/google-chrome',
  ].filter(Boolean);
  for (const c of candidates) {
    if (!c.includes('/')) {
      for (const dir of (process.env.PATH || '').split(':')) {
        try { accessSync(join(dir, c), constants.X_OK); return join(dir, c); } catch { /* next */ }
      }
      continue;
    }
    try { accessSync(c, constants.X_OK); return c; } catch { /* next */ }
  }
  // A scan is the last resort: Playwright names its directory after the
  // build it downloaded, which no caller should have to know.
  if (pw) {
    let entries = [];
    try { entries = readdirSync(pw); } catch { /* no such directory */ }
    for (const d of entries) {
      const p = join(pw, d, 'chrome-linux', 'chrome');
      try { accessSync(p, constants.X_OK); return p; } catch { /* next */ }
    }
  }
  return null;
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function reachable(url) {
  try { return (await fetch(url, { redirect: 'manual' })).status < 500; } catch { return false; }
}

const browser = findBrowser();
if (!browser) unavailable('no chromium found (set $CHROME)');
if (!await reachable(BASE + '/')) {
  unavailable(`nothing serving the UI at ${BASE} (start \`ochakai ui\`)`);
}

const profile = mkdtempSync(join(tmpdir(), 'ochakai-smoke-'));
const chrome = spawn(browser, [
  '--headless=new', '--remote-debugging-port=' + PORT, '--no-sandbox',
  '--disable-gpu', '--user-data-dir=' + profile, 'about:blank',
], { stdio: ['ignore', 'ignore', 'ignore'] });

async function debuggerURL() {
  for (let i = 0; i < 80; i++) {
    try {
      return (await (await fetch(`http://127.0.0.1:${PORT}/json/version`)).json()).webSocketDebuggerUrl;
    } catch { await sleep(250); }
  }
  throw new Error('chromium never opened its debugging port');
}

const ws = new WebSocket(await debuggerURL());
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });
let seq = 0;
const pending = new Map();
const events = [];
ws.onmessage = e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  else if (m.method) events.push(m);
};
const send = (method, params = {}, sessionId) =>
  new Promise(res => {
    const id = ++seq;
    pending.set(id, res);
    ws.send(JSON.stringify({ id, method, params, sessionId }));
  });

const { result: { targetId } } = await send('Target.createTarget', { url: 'about:blank' });
const { result: { sessionId } } = await send('Target.attachToTarget', { targetId, flatten: true });
for (const domain of ['Runtime', 'Log', 'Page']) await send(domain + '.enable', {}, sessionId);

async function evalJS(expression) {
  const r = await send('Runtime.evaluate',
    { expression, awaitPromise: true, returnByValue: true }, sessionId);
  const bad = r.result?.exceptionDetails;
  if (bad) throw new Error(expression + ' → ' + (bad.exception?.description ?? JSON.stringify(bad)));
  return r.result.result.value;
}

// One settle per navigation: the views render from a fetch, so there is
// nothing synchronous to await. Generous rather than tight — a slow
// answer must read as slow, never as a missing module.
const go = async hash => { await evalJS(`location.href = ${JSON.stringify(BASE + '/' + hash)}`); await sleep(1200); };
const text = sel => evalJS(`(document.querySelector(${JSON.stringify(sel)})||{}).textContent || ''`);
const count = sel => evalJS(`document.querySelectorAll(${JSON.stringify(sel)}).length`);

const failed = [];
function check(name, ok, detail = '') {
  console.log((ok ? 'ok   ' : 'FAIL ') + name + (ok || !detail ? '' : '\n     ' + detail));
  if (!ok) failed.push(name);
}

await go('#/');
check('the page boots', await evalJS('document.title') === 'ochakai');
check('the home page renders', (await text('#view')).length > 100);
check('the sidebar tree loads', (await count('#tree .tree-entry, #tree .tree-dir')) > 0);

await go('#/search');
await evalJS(`(() => { const q = document.querySelector('#q'); q.value = 'revenue';
  q.dispatchEvent(new Event('input', { bubbles: true })); })()`);
await sleep(1400);
check('search returns hits', (await count('#results .card')) > 0);

await go('#/k/metrics/revenue');
check('a concept renders', (await text('#view')).includes('Revenue'));
check('its body renders as markdown', (await count('#view .md *')) > 0);

await go('#/review');
check('the review queue renders', (await text('#view')).length > 100);

await go('#/new');
check('the editor renders', await evalJS('!!document.querySelector("#view textarea")'));

await go('#/dir/metrics');
check('a directory page renders', (await text('#view')).includes('metrics'));

// A module that failed to load is silent in the view and loud here,
// which is the whole reason this file exists. favicon.ico is the one
// exception: the page has never carried one, and its 404 predates the
// split this test was written for.
const noise = events.filter(e =>
  e.method === 'Runtime.exceptionThrown' ||
  (e.method === 'Runtime.consoleAPICalled' && e.params.type === 'error') ||
  (e.method === 'Log.entryAdded' && e.params.entry.level === 'error' &&
   !String(e.params.entry.url || '').endsWith('/favicon.ico')));
check('nothing threw and nothing logged an error', noise.length === 0,
  noise.map(e => JSON.stringify(e.params).slice(0, 400)).join('\n     '));

chrome.kill();
if (failed.length) {
  console.error(`\nsmoke: ${failed.length} of the checks above failed`);
  process.exit(1);
}
console.log('\nsmoke: the page works');
process.exit(0);
