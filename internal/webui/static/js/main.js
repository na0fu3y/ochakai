// Boot: the chrome that belongs to no view, then the first render.

import { markPosture } from './api.js';
import { initCopyButtons } from './clipboard.js';
import { $ } from './dom.js';
import { initPalette } from './palette.js';
import { refreshQueues } from './queues.js';
import { route } from './router.js';
import { refreshTree } from './tree.js';
import { markAccessTab } from './views/access.js';

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

// The divider drags: the raw pointer x is the width, and app.css clamps
// it where it is used, so nothing here needs to know the bounds. The
// width persists per browser like the collapsed choice; double-click
// forgets it, which is the default width again.
const sideResize = $('#side-resize');
const storedW = localStorage.getItem('ochakai.sidebar-width');
if (storedW) shell.style.setProperty('--side-w', storedW);
sideResize.addEventListener('pointerdown', e => {
  e.preventDefault(); // a drag, not a text selection
  sideResize.setPointerCapture(e.pointerId);
  sideResize.classList.add('dragging');
});
sideResize.addEventListener('pointermove', e => {
  if (!sideResize.hasPointerCapture(e.pointerId)) return;
  shell.style.setProperty('--side-w', `${e.clientX}px`);
});
// lostpointercapture covers both the release and a cancelled drag.
sideResize.addEventListener('lostpointercapture', () => {
  sideResize.classList.remove('dragging');
  const w = shell.style.getPropertyValue('--side-w');
  if (w) localStorage.setItem('ochakai.sidebar-width', w);
});
sideResize.addEventListener('dblclick', () => {
  shell.style.removeProperty('--side-w');
  localStorage.removeItem('ochakai.sidebar-width');
});

refreshTree();
refreshQueues();
markPosture();
markAccessTab();
initPalette();
initCopyButtons();
route();
