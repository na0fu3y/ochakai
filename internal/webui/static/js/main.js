// Boot: the chrome that belongs to no view, then the first render.

import { markPosture } from './api.js';
import { $ } from './dom.js';
import { refreshQueues } from './queues.js';
import { route } from './router.js';
import { refreshTree } from './tree.js';

// The sidebar disclosure mirrors the filter bar: forced open on wide
// screens (its summary is hidden there), collapsed by default on mobile.
export const sideDetails = $('#side-details');
sideDetails.open = matchMedia('(min-width: 801px)').matches;
matchMedia('(min-width: 801px)').addEventListener('change', e => { if (e.matches) sideDetails.open = true; });

// Desktop sidebar toggle — the collapsed choice persists per browser.
export const shell = $('#shell');
if (localStorage.getItem('ochakai.sidebar') === 'collapsed') shell.classList.add('side-collapsed');
$('#side-toggle').addEventListener('click', () => {
  const collapsed = shell.classList.toggle('side-collapsed');
  localStorage.setItem('ochakai.sidebar', collapsed ? 'collapsed' : 'open');
});

refreshTree();
refreshQueues();
markPosture();
route();
