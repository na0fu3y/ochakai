#!/bin/sh
# ochakai-write-back: Stop hook for Claude Code.
#
# Two of the loop's write-side moves, both nudged once per session, before
# the agent stops:
#
# - write-back: whether anything the agent learned belongs in the team
#   knowledge base.
# - report_outcome: whether any concept ochakai-recall.sh pointed this
#   session at (recorded to a per-session file under $TMPDIR) held up
#   when acted on. `failed` reports are what moves a verified concept into the
#   re-verification feed (design doc 0069 §2.1) — without them that feed stays
#   at zero regardless of how stale the knowledge actually is.
#
# CLAUDE.md alone relies on the agent remembering both habits; this hook
# makes the question unskippable. Neither move is something the hook can
# judge for itself (no LLM here, per 0001) — it can only ask, using the
# structural fact of which concepts this session actually read.
#
# Requires: jq. Failures are silent: never block the agent from stopping.
set -eu

input=$(cat)
# stop_hook_active is true when we already blocked this stop — let go.
[ "$(printf '%s' "$input" | jq -r '.stop_hook_active // false')" = "true" ] && exit 0

session=$(printf '%s' "$input" | jq -r '.session_id // empty')
transcript=$(printf '%s' "$input" | jq -r '.transcript_path // empty')
[ -n "$session" ] && [ -n "$transcript" ] && [ -r "$transcript" ] || exit 0

# Nudge at most once per session.
marker="${TMPDIR:-/tmp}/ochakai-write-back-$session"
[ -e "$marker" ] && exit 0

# Only sessions that look like data work deserve the interruption.
grep -qiE 'SELECT|ochakai|bigquery|warehouse' "$transcript" || exit 0
: >"$marker"

recalled_file="${TMPDIR:-/tmp}/ochakai-recalled-$session"
recalled=""
[ -r "$recalled_file" ] && recalled=$(sort -u "$recalled_file" 2>/dev/null | head -20 | tr '\n' ' ')

outcome_reason=""
if [ -n "$recalled" ]; then
	outcome_reason="This session was pointed at these ochakai concepts: $recalled — if you fetched one and acted on it (ran its SQL, followed its definition), report whether the result held up: \`ochakai report <id> worked\` or \`ochakai report <id> failed --note \"what went wrong\"\`. Skip any you only read but never acted on. Always report failed when a verified concept led you to a wrong number. "
fi

jq -n --arg outcome "$outcome_reason" '{
  decision: "block",
  reason: ($outcome + "Before finishing: this session did data work. If a query you wrote proved correct and reusable, or you learned how to read a metric (a baseline, a seasonality, a caveat), write it back to ochakai — search first (including --rejected) to avoid re-proposing, then `ochakai put <path>` (type \"Attested Computation\" with runtime, the SQL in a # Computation fence, and a top-level question key; or type \"Insight\"). If nothing durable was learned, finish now without creating anything — do not invent knowledge.")
}'
