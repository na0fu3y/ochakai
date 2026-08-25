// Which view the hash means. It imports every view, so nothing else
// should import it — the one exception is the editor, which re-routes
// after a save, and does it from inside a handler long after both
// modules have finished evaluating.

import { $, view } from './dom.js';
import { markTreeSelection } from './tree.js';
import { viewAccess } from './views/access.js';
import { viewDetail } from './views/detail.js';
import { viewDir, viewHome } from './views/dir.js';
import { viewEditor } from './views/editor.js';
import { explore, viewExplore } from './views/explore.js';
import { viewReview } from './views/review.js';

// Set by the editor while it holds unsaved changes: route() asks before
// discarding them on hash navigation, beforeunload warns on real navigation.
export let unsaved = null; // () => boolean
window.addEventListener('beforeunload', e => { if (unsaved && unsaved()) e.preventDefault(); });

export function route() {
  // Every route this page has begins "#/". A hash that does not is an
  // anchor within the document being shown — a footnote marker jumping
  // to the foot of a body, and back — so it is the browser's to handle
  // and not a navigation at all. Without this the reader who clicked a
  // footnote landed on the home page, which is worse than the marker
  // not being a link.
  const hash = location.hash;
  if (hash && !hash.startsWith('#/')) {
    // Except at boot: an anchor is all the URL holds then — the route it
    // jumped within is gone — and returning without rendering left the
    // whole page blank. The home page is the honest answer to an address
    // that names a place inside a document it does not name.
    if (!route._rendered) {
      route._rendered = true;
      viewHome();
    }
    return;
  }
  if (unsaved && unsaved() && !confirm('保存していない変更を破棄しますか？')) {
    // Put the URL back; replaceState does not re-fire hashchange.
    history.replaceState(null, '', route._current || '#/');
    return;
  }
  unsaved = null;
  route._current = hash || '#/';
  const [head, ...rest] = hash.replace(/^#\/?/, '').split('/');
  document.querySelectorAll('#topnav a').forEach(a => {
    a.classList.remove('active');
    a.removeAttribute('aria-current');
  });
  // aria-current beside the class: the colour says "you are here" to a
  // reader who can see it, and nothing said it to one who cannot.
  const mark = r => {
    const a = document.querySelector(`#topnav a[data-route="${r}"]`);
    if (a) { a.classList.add('active'); a.setAttribute('aria-current', 'page'); }
  };
  if (head === 'k' && rest.length >= 1) {
    viewDetail(rest.map(decodeURIComponent).join('/'));
  } else if (head === 'dir') {
    // A directory's index page — the web rendering of the index.md the
    // OKF export generates at every level. #/dir/ (the root) shows the
    // same listing the home page carries.
    viewDir(rest.map(decodeURIComponent).join('/'));
  } else if (head === 'edit' && rest.length >= 1) {
    viewEditor(rest.map(decodeURIComponent).join('/'));
  } else if (head === 'new') {
    // New entry lives in the sidebar's “＋” (global and per-directory)
    // and the search view, not a top-level tab — creating knowledge is
    // part of exploring it. #/new/<prefix>/ prefills the ID with the
    // directory the ＋ was clicked on.
    viewEditor(null, rest.map(decodeURIComponent).join('/'));
  } else if (head === 'browse') {
    // Legacy route from when the tree was a tab; it is the sidebar now.
    location.replace('#/');
    return;
  } else if (head === 'review') {
    mark('review');
    viewReview();
  } else if (head === 'access') {
    // Reachable by typing it even where the tab is hidden, which is what
    // a route is: the view says who may read the policy, and the server
    // says it again on every call.
    mark('access');
    viewAccess();
  } else if (head === 'search') {
    mark('search');
    // #/search/reported-wrong and #/search/verification-age open a feed
    // directly. Both are entrances to the review loop, and the filter bar
    // was the only way in — a queue nobody can find stops being read as
    // surely as one nobody can empty (design doc 0025 §6).
    if (rest[0] === 'reported-wrong' || rest[0] === 'verification-age' || rest[0] === 'stale') {
      explore.failedFeed = rest[0] === 'reported-wrong';
      explore.ageFeed = rest[0] === 'verification-age';
      explore.expiredFeed = rest[0] === 'stale';
      explore.source = '';
    } else if (rest[0] === 'in') {
      // Search scoped to a directory — where the tree's "Search here"
      // lands. A route rather than a click handler so it is a real link:
      // openable in a new tab, and shareable as "search our team's space"
      // (design doc 0041 §2.8). The scope is a filter, so it leaves the
      // query and the other filters alone.
      explore.prefix = rest.slice(1).map(decodeURIComponent).join('/').replace(/^\/+|\/+$/g, '');
    } else if (rest[0] === 'cites') {
      // The reverse lookup: which entries cite one resource. The resource
      // is a URI, so it travels as one encoded segment.
      explore.source = decodeURIComponent(rest.slice(1).join('/'));
      explore.q = '';
      explore.failedFeed = explore.ageFeed = explore.expiredFeed = false;
    } else if (rest.length === 0) {
      explore.source = '';
    }
    viewExplore();
  } else {
    viewHome();
  }
  markTreeSelection();
  // On a narrow screen the sidebar is a disclosure that sits above the
  // view in the document. Left open across a navigation, it kept the new
  // content a screenful below the tap that asked for it — so a tap on a
  // tree entry looked like it did nothing. Navigating is the moment the
  // tree has served its purpose; close it, like a drawer.
  if (matchMedia('(max-width: 800px)').matches) $('#side-details').open = false;
  // A navigation replaced the main region in place. Focus follows it, so
  // the next Tab continues inside the new view and a screen reader reads
  // it out — but not on the first render, where the browser has just
  // loaded the document and moving focus would fight it.
  if (route._rendered) view.focus({ preventScroll: true });
  route._rendered = true;
}
window.addEventListener('hashchange', route);

// Result cards navigate on a click anywhere in them, not just the title.
// A click on a control (link, button, form field) keeps its own behavior,
// a click that selected text (copying the SQL or the ochakai:// URI) is
// not a navigation, and the title stays a real link for keyboard tabbing
// and open-in-new-tab.
view.addEventListener('click', e => {
  const card = e.target.closest('.card[data-href]');
  if (!card || e.target.closest('a, button, input, textarea, select, label, summary')) return;
  if (getSelection().toString()) return;
  location.hash = card.dataset.href;
});
