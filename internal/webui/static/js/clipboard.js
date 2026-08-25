// Copying what a reader came for. The knowledge here is written to be
// used somewhere else — a verified query pasted into a console, an id
// handed to an agent — and a page that renders it without a way to take
// it makes every reader select it by hand.
//
// The decoration is one observer rather than a call in every view: the
// page redraws whole regions by innerHTML (a view, a tab body, a result
// list), so a rendered <pre> appears in places that share no code, and
// asking each of them to remember is asking one of them to forget. The
// click is one delegated listener for the same reason — an injected
// button carries no listener of its own and can be thrown away with the
// markup around it.

import { toast } from './api.js';
import { view } from './dom.js';

// copyText puts one string on the clipboard and says so. The API needs a
// secure context, which localhost (`ochakai ui`) and https (`serve-ui`)
// both are; anywhere else it is refused, and a button that did nothing
// silently would read as a broken page rather than an unavailable one.
export async function copyText(text, doneMsg) {
  try {
    await navigator.clipboard.writeText(text);
    toast(doneMsg || 'コピーしました。');
    return true;
  } catch {
    toast('コピーできませんでした。お使いのブラウザで選択してコピーしてください。');
    return false;
  }
}

const BUTTON = 'copy-btn';

// decorate puts a button in every code block that has none. It is
// idempotent because it must be: appending the button is itself a
// mutation, so the observer sees its own work and comes straight back.
function decorate(root) {
  for (const pre of root.querySelectorAll('pre:not([data-copy-wired])')) {
    pre.dataset.copyWired = '1';
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = BUTTON;
    btn.title = 'コピー';
    btn.setAttribute('aria-label', 'コピー');
    btn.textContent = '⧉';
    pre.appendChild(btn);
  }
}

export function initCopyButtons() {
  decorate(view);
  new MutationObserver(records => {
    for (const r of records) {
      for (const node of r.addedNodes) {
        if (node.nodeType !== 1) continue;
        if (node.matches && node.matches('pre')) decorate(node.parentNode || view);
        else if (node.querySelector) decorate(node);
      }
    }
  }).observe(view, { childList: true, subtree: true });

  // One listener for every button there will ever be, including the ones
  // written into a view's own markup with a data-copy of their own.
  document.addEventListener('click', e => {
    const btn = e.target.closest && e.target.closest('.' + BUTTON);
    if (!btn) return;
    e.preventDefault();
    if (btn.dataset.copy !== undefined) {
      copyText(btn.dataset.copy, btn.dataset.copyDone);
      return;
    }
    const pre = btn.closest('pre');
    if (!pre) return;
    // The block's text without the button's own glyph: a copy of the
    // element, minus what this module put in it.
    const clone = pre.cloneNode(true);
    clone.querySelectorAll('.' + BUTTON).forEach(b => b.remove());
    copyText(clone.textContent, 'コードをコピーしました。');
  });
}
