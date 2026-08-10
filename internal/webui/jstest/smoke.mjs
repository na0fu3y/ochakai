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

// Every view renders from a fetch, so there is nothing synchronous to
// wait for — and a fixed sleep is the wrong answer to that. It has to be
// long enough for the slowest machine that will ever run this, which
// makes it both slow everywhere else and still a race: the first version
// of this file slept 1.2s per navigation, passed on a warm laptop, and
// failed in CI on the sidebar, whose entries arrive one round trip after
// the view's. So each check waits for its own condition instead, and
// only gives up at a deadline.
const DEADLINE_MS = 20_000;

async function waitFor(expression) {
  const until = Date.now() + DEADLINE_MS;
  for (;;) {
    const v = await evalJS(expression);
    if (v) return v;
    if (Date.now() > until) return v;
    await sleep(100);
  }
}

// The view is emptied before navigating, so waiting for content cannot
// be satisfied by the previous route's: a hash change re-renders into
// the same element, and without this every wait after the first would
// return immediately on stale markup.
async function go(hash) {
  await evalJS(`(() => { const v = document.querySelector('#view'); if (v) v.innerHTML = ''; })()`);
  await evalJS(`location.href = ${JSON.stringify(BASE + '/' + hash)}`);
}

const json = v => JSON.stringify(v);
const textOf = sel => `(document.querySelector(${json(sel)}) || {}).textContent || ''`;
const countOf = sel => `document.querySelectorAll(${json(sel)}).length`;

const failed = [];

// detail is asked for only on failure — it costs two round trips, and a
// passing run should not pay for a sentence nobody prints.
async function check(name, ok, detail) {
  console.log((ok ? 'ok   ' : 'FAIL ') + name + (ok || !detail ? '' : '\n     ' + await detail()));
  if (!ok) failed.push(name);
}

// What was on screen instead. The run that produced the failure is gone
// by the time anybody reads the log, so it has to say.
async function shown() {
  const body = await evalJS(`(document.querySelector('#view') || {}).textContent || ''`);
  const tree = await evalJS(`(document.querySelector('#tree') || {}).textContent || ''`);
  return `gave up after ${DEADLINE_MS} ms — #view held ${body.length} characters, #tree held ${tree.length}`;
}

// The walk is wrapped because a page broken early makes every later step
// throw on an element that is not there — and the most useful check of
// all is the last one, which names the module that failed to load. A
// stack trace here would replace it with a symptom.
try {
  await go('#/');
  await check('the page boots', await waitFor(`document.title === 'ochakai'`), shown);
  const rendered = await waitFor(`${textOf('#view')}.length > 100`);
  await check('the home page renders', rendered, shown);
  // Nothing below can pass if the first view never drew, and each of
  // them would spend the full deadline discovering that. Stop, so the
  // error tally — which names the module — is what the log ends on.
  if (!rendered) throw new Error('the first view never rendered; the rest of the walk is skipped');
  await check('the sidebar tree loads',
    await waitFor(`${countOf('#tree .tree-entry, #tree .tree-dir')} > 0`), shown);

  await go('#/search');
  // Guarded rather than assumed: on a page that did not finish loading
  // there is no box to type in, and the failure worth printing is the
  // one below, not a TypeError from the test's own typing.
  await waitFor(`!!document.querySelector('#q')`);
  await evalJS(`(() => { const q = document.querySelector('#q'); if (!q) return;
    q.value = 'revenue'; q.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  await check('search returns hits', await waitFor(`${countOf('#results .card')} > 0`), shown);

  await go('#/k/metrics/revenue');
  await check('a concept renders', await waitFor(`${textOf('#view')}.includes('Revenue')`), shown);
  await check('its body renders as markdown', await waitFor(`${countOf('#view .md *')} > 0`), shown);

  await go('#/review');
  await check('the review queue renders', await waitFor(`${textOf('#view')}.length > 100`), shown);

  await go('#/new');
  await check('the editor renders', await waitFor(`!!document.querySelector('#view textarea')`), shown);

  await go('#/dir/metrics');
  await check('a directory page renders', await waitFor(`${textOf('#view')}.includes('metrics')`), shown);
} catch (e) {
  await check('the walk reached the end', false, () => String(e && e.message || e));
}

// One settle, and only here: an error logged by a request still in
// flight when the last check passed belongs in this tally.
await sleep(500);

// A module that failed to load is silent in the view and loud here,
// which is the whole reason this file exists. favicon.ico is the one
// exception: the page has never carried one, and its 404 predates the
// split this test was written for.
const noise = events.filter(e =>
  e.method === 'Runtime.exceptionThrown' ||
  (e.method === 'Runtime.consoleAPICalled' && e.params.type === 'error') ||
  (e.method === 'Log.entryAdded' && e.params.entry.level === 'error' &&
   !String(e.params.entry.url || '').endsWith('/favicon.ico')));
await check('nothing threw and nothing logged an error', noise.length === 0,
  () => noise.map(e => JSON.stringify(e.params).slice(0, 400)).join('\n     '));

chrome.kill();
if (failed.length) {
  console.error(`\nsmoke: ${failed.length} of the checks above failed`);
  process.exit(1);
}
console.log('\nsmoke: the page works');
process.exit(0);
