// Which of a body's links point at something that is there. A wiki
// draws the ones that do not in red, and the reason is not decoration:
// a concept that names a neighbour which was deleted or moved is a
// concept that is quietly wrong, and only the reader following the link
// ever finds out.
//
// The probe is injected rather than imported, so the walk can be tested
// without a network — and so the caller decides what a read costs.

// checkTargets asks about each id at most once, a few at a time, and
// answers with what it learned: which ids are there, and which are not.
//
// **An id can come back in neither.** Only a read that says the concept
// is missing makes it dead, and only a read that succeeds makes it
// found; a network failure, a 500, a read the policy refuses are none of
// those, and the caller must be able to tell "not there" from "did not
// find out". Returning the dead set alone cannot say that — every
// answer that was not a 404 would read as "present", and a caller
// remembering that would stop asking about a concept it never saw.
export async function checkTargets(ids, probe, limit = 4) {
  const queue = [...new Set(ids)].filter(Boolean);
  const found = new Set();
  const dead = new Set();
  let at = 0;
  const worker = async () => {
    while (at < queue.length) {
      const id = queue[at++];
      try {
        await probe(id);
        found.add(id);
      } catch (e) {
        if (e && e.code === 'not_found') dead.add(id);
      }
    }
  };
  // At least one worker, however the caller spelled the cap: a pool of
  // none finishes instantly having asked nothing, which is the one
  // outcome that looks like a clean answer while carrying no answer.
  const workers = Math.min(Math.max(1, limit | 0), queue.length);
  await Promise.all(Array.from({ length: workers }, worker));
  return { found, dead };
}
