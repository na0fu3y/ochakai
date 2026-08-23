// Where the tree starts, when the levels above hold nothing to choose
// between.
//
// A caller granted one directory (design doc 0109) opens the page on a
// root holding one directory holding one directory, and two clicks stand
// between them and their own knowledge every time. This walks that
// corridor once, at load, so the sidebar and the home listing begin where
// the reader would have arrived anyway.
//
// **It hides nothing, and that is the whole of why it is safe**: a step
// is taken only when the level has exactly one subdirectory, no concepts
// and no files, so what is skipped is a corridor rather than a room. The
// prefixes stay whole — a walked path is shown with the way back, and an
// id is spelled the way it is stored, because a second spelling of an
// address is what this must not buy (design doc 0075 §2).
//
// Kept apart from tree.js so it can be tested as what it is: a function
// over levels. load(prefix) answers the level at a prefix, and a load
// that throws ends the walk where it stands — a corridor that cannot be
// read is as far as this caller gets, and the page still renders.

// MAX_DESCENT bounds the walk. A corridor this long is not a shape
// anybody navigates by hand, and the bound keeps a boot from becoming one
// request per segment of some pathological tree.
export const MAX_DESCENT = 8;

// isCorridor reports whether a level holds one directory and nothing
// else — the only shape it is safe to walk past.
export function isCorridor(level) {
  return (level.dirs || []).length === 1
    && !(level.concepts || []).length
    && !(level.files || []).length;
}

export async function descendSingleRoad(level, prefix, load) {
  for (let i = 0; i < MAX_DESCENT && isCorridor(level); i++) {
    const next = prefix + level.dirs[0].name + '/';
    let deeper;
    try {
      deeper = await load(next);
    } catch {
      break;
    }
    level = deeper;
    prefix = next;
  }
  return { level, prefix };
}
