#!/bin/sh
# ochakai-write-back: Stop hook for Claude Code.
#
# The write-back half of the loop. When the agent finishes a session that
# looks like data work, ask it once — before it stops — whether anything
# it learned belongs in the team knowledge base. CLAUDE.md alone relies
# on the agent remembering the habit; this hook makes the question
# unskippable, exactly once per session.
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

jq -n '{
  decision: "block",
  reason: "Before finishing: this session did data work. If a query you wrote proved correct and reusable, or you learned how to read a metric (a baseline, a seasonality, a caveat), write it back to ochakai — search first (including --status rejected) to avoid re-proposing, then `ochakai create` (type: query with attrs.question + attrs.sql, or type: insight). If nothing durable was learned, finish now without creating anything — do not invent knowledge."
}'
