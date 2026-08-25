// Which of a body's links point at something that is there. A wiki
// draws the ones that do not in red, and the reason is not decoration:
// a concept that names a neighbour which was deleted or moved is a
// concept that is quietly wrong, and only the reader following the link
// ever finds out.
//
// The probe is injected rather than imported, so the walk can be tested
// without a network — and so the caller decides what a read costs.

// checkTargets asks about each id at most once, a few at a time, and
// answers with the ones that are not there.
//
// Only a not_found is dead. A network failure, a 500, a read the policy
// refuses — none of those say the concept is missing, and marking a link
// red because the server hiccuped would put a permanent-looking verdict
// on a temporary condition (the page keeps no record either way, so the
// next render asks again).
export async function checkTargets(ids, probe, limit = 4) {
  const queue = [...new Set(ids)].filter(Boolean);
  const dead = new Set();
  let at = 0;
  const worker = async () => {
    while (at < queue.length) {
      const id = queue[at++];
      try {
        await probe(id);
      } catch (e) {
        if (e && e.code === 'not_found') dead.add(id);
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, queue.length) }, worker));
  return dead;
}
