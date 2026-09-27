#!/bin/sh
# Restore a sandbox deployment to the bundle it ships with. This is the
# entrypoint of the image build.sh makes, run by a Cloud Run job on a
# Cloud Scheduler trigger; demo.ochak.ai runs it every six hours.
#
# A sandbox is anonymous and writable (design doc 0087), so anyone can
# write, rule on, and destroy what is in it. Putting it back is the
# operator's job and not the product's (0087 §2): ochakai has no reset
# endpoint and is not getting one, because a knowledge store that can
# erase itself on request is a different product.
#
# Two passes, in this order:
#
#   1. Purge every concept the base currently holds — chiefly the ones a
#      visitor added that the seed does not contain, since those are what
#      an import would leave behind. Delete is reversible and purge is
#      not (0031), so both are needed to free an id completely.
#
#      **The second enumeration is gone, and so is the bug it worked
#      around.** Until 0.28.0 a rejection was a human ruling against a
#      live concept: the id was absent from every ordinary listing and
#      import refused to land on it, so a visitor who tried the reject
#      half of the loop on a seed concept removed it from the demo
#      permanently — which is what happened to
#      metrics/repeat-purchase-rate on 2026-08-27, and `ochakai list
#      --rejected` was the listing that saw them. Design doc 0135 made a
#      rejection a deletion whose reason is a log entry rather than a
#      wall: there is no rejected category left to enumerate, the flag is
#      gone from the CLI, and the tombstone a visitor leaves is revived
#      in place by the import below. The seed comes back on its own.
#   2. Import the seed, which is the bundle build.sh copied into the image
#      (examples/demo from the release the image was built from, unless
#      told otherwise).
#
# Neither pass is allowed to fail the run on a single id: a visitor
# racing the job is expected, not exceptional, and the next run fixes
# whatever this one could not. What must not happen is the job dying
# half way and leaving the base empty, which is why the import is last
# and unconditional.
set -eu

: "${OCHAKAI_URL:?OCHAKAI_URL must name the sandbox to reset}"

echo "reset: target ${OCHAKAI_URL}"

# A listing pages; 1000 is the server's own maximum and far past what a
# demo should ever hold. If a page is full, say so — that is the signal
# that this loop needs a --cursor and nobody noticed.
ids=$(ochakai list usage --json --limit 1000 | jq -r '.hits[].id')
n=$(printf '%s' "$ids" | grep -c . || true)
if [ "$n" -ge 1000 ]; then
	echo "reset: WARNING page is full at 1000 — this loop does not follow cursors" >&2
fi

echo "reset: purging ${n} concept(s)"

for id in $ids; do
	# Already-deleted and already-gone are both fine: the goal is the
	# absence of the id, not the success of the call.
	ochakai delete "$id" >/dev/null 2>&1 || true
	ochakai purge "$id" >/dev/null 2>&1 || true
done

echo "reset: importing the seed"
ochakai import /seed

echo "reset: done"
