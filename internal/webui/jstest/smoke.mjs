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
const PORT = process.env.OCHAKAI_TEST_SMOKE_CDP_PORT || '9222';
// A missing browser or a server that never came up is a skip on a laptop
// and a failure in CI, where a silent skip is a green run that tested
// nothing. CI sets this; nothing else does.
//
// Both names sit under OCHAKAI_TEST_, the half of the namespace this
// repository's harness keeps (design doc 0112 §4): a start refuses every
// other OCHAKAI_ variable it does not read, and the smoke's own two would
// have stopped the server this file exists to drive the moment somebody
// exported them a step earlier.
const REQUIRED = !!process.env.OCHAKAI_TEST_SMOKE_REQUIRED;

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
// stderr is kept, not discarded. It is where chromium says why it will
// not start — a missing shared library, a sandbox it cannot use, a
// profile directory it cannot write — and it is also where it announces
// "DevTools listening on ws://…". Throwing it away is what made a
// failed launch unactionable: the harness could say only that a port
// never opened, and every such failure cost a re-run to learn nothing.
// Bounded, because a browser that is unhappy repeats itself.
const chrome = spawn(browser, [
  '--headless=new', '--remote-debugging-port=' + PORT, '--no-sandbox',
  '--disable-gpu', '--user-data-dir=' + profile, 'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] });

let said = '';
chrome.stderr.setEncoding('utf8');
chrome.stderr.on('data', d => { said = (said + d).slice(-4000); });

// Whether the process is still trying. A launch that fails and a launch
// that is slow look identical from the port alone, and they are not the
// same failure: one is this machine, the other is this minute.
let exit = null;
chrome.on('exit', (code, signal) => { exit = { code, signal }; });

// 60 seconds, from 20. The evidence for the raise is what CI leaves
// behind when this fails: the chrome process is still alive at job
// cleanup, so it had not crashed — it had not finished starting. A cold
// first launch on a loaded runner is slower than the old budget, and the
// job that pays for it is the one that just built the Go binaries.
// Nothing waits the full minute on a healthy run: the loop ends the
// moment the port answers.
const launchBudgetMs = 60_000;

async function debuggerURL() {
  const step = 250;
  for (let waited = 0; waited < launchBudgetMs; waited += step) {
    try {
      return (await (await fetch(`http://127.0.0.1:${PORT}/json/version`)).json()).webSocketDebuggerUrl;
    } catch { /* not up yet */ }
    if (exit) {
      throw new Error(`chromium exited before opening its debugging port ` +
        `(${exit.signal ? 'signal ' + exit.signal : 'exit code ' + exit.code})` + saidBy(said));
    }
    await sleep(step);
  }
  throw new Error(`chromium never opened its debugging port within ${launchBudgetMs / 1000}s ` +
    `(it is still running, so it was starting rather than broken)` + saidBy(said));
}

// What chromium said, indented under the error, or nothing when it said
// nothing — an empty "chromium said:" reads like the output was lost.
function saidBy(text) {
  text = text.trim();
  return text ? '\nchromium said:\n  ' + text.replace(/\n/g, '\n  ') : '';
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

// A desktop viewport, stated rather than inherited: headless opens
// 800x600, which is exactly the width at which the page switches to its
// stacked narrow layout — so the layout every reader on a laptop sees
// would be the one this walk never loaded.
await send('Emulation.setDeviceMetricsOverride',
  { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);

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
  // A directory is `details.node` and a concept is `.tree-entry`; a
  // corpus may legitimately have none of the second, so asking for both
  // is what "the tree drew something" means. Asking only for
  // `.tree-entry` passed on a development database with concepts left at
  // the root and could never have passed on a fresh import of
  // examples/demo, where every concept lives in a directory.
  await check('the sidebar tree loads',
    await waitFor(`${countOf('#tree details.node, #tree .tree-entry')} > 0`), shown);
  // And it is the tree, not the banner it draws when its own fetch fails
  // — which is caught, so it reaches no console and no other check.
  // Asked once rather than waited for: the wait above already settled,
  // and a banner is a state the tree stays in.
  await check('the tree is a tree, not an error banner',
    (await evalJS(countOf('#tree .error-banner'))) === 0,
    () => evalJS(`(document.querySelector('#tree') || {}).textContent || ''`));

  // A deployment with authentication off says so on the page, and one
  // that does not says nothing. Asserted against what the API answers
  // rather than against CI's own posture, so the check means the same
  // thing wherever this is pointed: the banner is up exactly when the
  // deployment admits it is insecure. Both directions matter — a banner
  // that is always on is a banner nobody reads.
  const insecure = (await (await fetch(BASE + '/api/v1/stats')).json()).insecure_dev === true;
  await check(`the dev banner is ${insecure ? 'up' : 'absent'}, as /stats says`,
    await waitFor(`(() => {
      const n = document.querySelector('.dev-note');
      return !!n && (getComputedStyle(n).display !== 'none') === ${insecure};
    })()`), shown);

  // And it costs the page nothing. The shell is a two-column grid, and a
  // banner shown as a sibling of <main> was a third grid item: it took
  // the content cell, the view was placed in the sidebar's column one
  // row down, and a sticky sidebar covered it — a dev or sandbox
  // deployment served an empty-looking page on every desktop screen.
  // Every check around this one passed throughout, because all of it was
  // still in the DOM. Geometry is the only thing that says otherwise, so
  // this asks where the view actually is.
  await check('the view is beside the sidebar, not under it',
    await waitFor(`(() => {
      const v = document.querySelector('#view').getBoundingClientRect();
      const side = document.querySelector('.sidebar').getBoundingClientRect();
      if (v.width < 400) return false;
      // The narrow layout stacks them, which is its own correct answer.
      if (getComputedStyle(document.querySelector('.shell')).display !== 'grid') {
        return v.top >= side.bottom - 1;
      }
      return v.left >= side.right - 1;
    })()`), () => evalJS(`JSON.stringify({
      view: document.querySelector('#view').getBoundingClientRect(),
      sidebar: document.querySelector('.sidebar').getBoundingClientRect(),
    })`));

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

  // examples/demo's metrics/revenue cites a source with a footnote, which
  // is the notation `sources[].id` exists for. Two things have to be
  // true: it is drawn as a footnote, and following the marker stays on
  // the concept — the router owns "#/" routes, and an in-page anchor is
  // not one.
  await check('a footnote is drawn as a footnote',
    await waitFor(`${countOf('#view .md:not(.desc) .fnref a')} > 0`), shown);
  await evalJS(`document.querySelector('#view .md:not(.desc) .fnref a').click()`);
  // Asserted on something only the detail view draws. "the text still
  // says Revenue" is not that: the home page lists the concept too, so
  // the first version of this check passed against a build with the
  // router guard removed.
  await check('following a footnote marker stays on the concept',
    await waitFor(`location.hash.startsWith('#fn-')
      && !!document.querySelector('#view .md:not(.desc) .footnotes')`), shown);

  await go('#/review');
  await check('the review queue renders', await waitFor(`${textOf('#view')}.length > 100`), shown);
  // Only a route the top nav names has a current item — a directory page
  // is under neither tab, and marking one would be a lie.
  // Eight weeks of review as one shape (design doc 0095). Drawn as
  // inline SVG with a text label, so the trend is available to a reader
  // who cannot see the bars at all.
  await check('the review trend is drawn, and is readable without seeing it',
    await waitFor(`(() => {
      const s = document.querySelector('#view .spark');
      return !!s && s.querySelectorAll('rect').length === 8
        && (s.querySelector('svg').getAttribute('aria-label') || '').length > 10;
    })()`), shown);
  await check('the current tab says so to a reader who cannot see it',
    await waitFor(`!!document.querySelector('#topnav a[data-route="review"][aria-current="page"]')`), shown);

  // The window and the scope are the stats call's own two parameters, so
  // these two checks are the round trip: the page asks, the server
  // answers about that period and that path, and the band redraws. A
  // control that changed nothing on screen would pass every check above
  // this one.
  await waitFor(`!!document.querySelector('#loop-days')`);
  await evalJS(`(() => { const d = document.querySelector('#loop-days'); if (!d) return;
    d.value = '7'; d.dispatchEvent(new Event('change', { bubbles: true })); })()`);
  // The band names the window the server answered about (window_days),
  // not the one the control asked for — a page that drew its own request
  // back would say 7 whatever came back.
  await check('the flow numbers can be asked about another period',
    await waitFor(`${textOf('#loop-stats')}.includes('直近 7 日に起きたこと')`), shown);

  // examples/demo keeps its metrics under metrics/, and a scope the base
  // does not have would draw the same note over nothing at all.
  await evalJS(`(() => { const p = document.querySelector('#loop-prefix'); if (!p) return;
    p.value = 'metrics'; p.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  // Scoped, the misses beside the other numbers are still the whole
  // instance's and cannot be anything else (design doc 0069 §5.1) — so
  // the reading says which of its numbers did not narrow.
  await check('a scoped reading says what it could not scope',
    await waitFor(`(() => { const t = ${textOf('#loop-stats')};
      return t.includes('該当なしの検索だけは絞れません') && t.includes('/metrics'); })()`), shown);

  // The access policy (design doc 0109 §5). The tab is absent until the
  // server answers for it, so this is also the check that the probe ran
  // at all: CI's deployment has no policy and names no administrators,
  // which leaves every caller unscoped and the tab visible.
  await check('the access tab appears once the server answers for it',
    await waitFor(`(() => { const a = document.querySelector('#nav-access');
      return !!a && !a.hidden && getComputedStyle(a).display !== 'none'; })()`), shown);
  await go('#/access');
  await check('the access policy renders', await waitFor(`${textOf('#view')}.includes('境界はまだありません')`), shown);
  // The editor is the document the CLI's -f takes, seeded from what the
  // server just sent — closed in a disclosure, which is why this asks
  // the textarea's value rather than what is on screen.
  await check('the policy is offered as the document `ochakai access -f` takes',
    await waitFor(`(() => { const t = document.querySelector('#access-doc');
      return !!t && t.value.includes('"rules"'); })()`), shown);

  await go('#/new');
  await check('the editor renders', await waitFor(`!!document.querySelector('#view textarea')`), shown);

  await go('#/dir/metrics');
  await check('a directory page renders', await waitFor(`${textOf('#view')}.includes('metrics')`), shown);

  // A navigation replaces the main region in place, so focus has to
  // follow it — otherwise the next Tab continues wherever the last click
  // left it, in content that is no longer on screen.
  await check('focus follows the navigation',
    await waitFor(`document.activeElement && document.activeElement.id === 'view'`), shown);
  await check('what the page announces is announced to everyone',
    await waitFor(`(document.querySelector('#toast') || {}).getAttribute
      && document.querySelector('#toast').getAttribute('aria-live') === 'polite'`), shown);
} catch (e) {
  await check('the walk reached the end', false, () => String(e && e.message || e));
}

// The page renders somebody else's text, so it is served under a policy
// the browser enforces. Checked explicitly because its absence is
// silent: a page with no policy logs no violations, so the tally below
// would pass a build that had dropped the header entirely.
const csp = (await fetch(BASE + '/')).headers.get('content-security-policy') || '';
await check('the page is served under a content security policy',
  csp.includes("script-src 'self'") && csp.includes("default-src 'none'"),
  () => csp || '(no Content-Security-Policy header)');

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
