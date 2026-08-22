// How much work is waiting, on the tab that leads to it (design doc
// 0049). Refreshed at boot and after everything that empties a queue.

import { api } from './api.js';
import { $ } from './dom.js';

// How much work the three queues hold, as one call rather than three
// listings (design doc 0049). The web UI is where the reviewing happens,
// so the count belongs on the tab that leads to it: a queue going quiet
// and a queue being empty look identical until somebody opens it.
//
// Refreshed at boot, when the review queue is opened, and after every
// ruling or save — verifying, rejecting and editing are what move a
// count (each queue empties differently: design doc 0049 §1).
export let waiting = null;

export async function refreshQueues() {
  try {
    // The queue depths live on the stats face; the address they had of
    // their own is gone (design doc 0049 §3.1).
    waiting = (await api('/api/v1/stats')).queues;
  } catch {
    waiting = null; // a nudge that cannot load must not become an error banner
  }
  const badge = $('#nav-waiting');
  const total = waiting ? waiting.drafts + waiting.failed + waiting.stale_after : 0;
  badge.textContent = total;
  badge.hidden = !total;
  badge.title = total ? `未処理 ${total} 件(draft ${waiting.drafts}・誤りの報告 ${waiting.failed}・期限切れ ${waiting.stale_after})` : '';
  const strip = $('#queue-strip');
  if (strip) strip.innerHTML = queueStrip();
}

// The same three numbers as links into the feed each one counts. Zero is
// worth printing here (unlike on the tab): on the review page it is the
// answer to "is there anything else", and it is an answer nothing gave
// before.
export function queueStrip() {
  if (!waiting) return '';
  return [
    ['#/review', waiting.drafts, 'draft'],
    ['#/search/reported-wrong', waiting.failed, '誤りの報告'],
    ['#/search/stale', waiting.stale_after, '期限切れ'],
  ].map(([href, n, label]) =>
    `<a class="badge${n ? ' draft' : ''}" href="${href}">${label} ${n}</a>`).join(' ');
}
