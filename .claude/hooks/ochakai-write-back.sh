#!/bin/sh
# ochakai-write-back: Stop hook — once per session, before the agent
# stops, ask the two write-side questions of the loop:
#
# - report_outcome: did the concepts ochakai-recall.sh pointed this
#   session at hold up when acted on?
# - write-back: did the session learn something durable about ochakai
#   that belongs in the knowledge base?
#
# Adapted from examples/claude-code/hooks/ochakai-write-back.sh. Two
# changes: the trigger is development work on this tree rather than data
# work, and the instructions point at the ochakai MCP tools rather than
# the CLI — on the dev instance CLI writes are recorded as the anonymous
# human, while the MCP connection carries the agent's process identity
# (kb/bundle/policies/ai-human-identity.md).
#
# Requires: jq. Failures are silent: never block the agent from stopping.
set -eu

command -v jq >/dev/null 2>&1 || exit 0

input=$(cat)
# stop_hook_active is true when we already blocked this stop — let go.
[ "$(printf '%s' "$input" | jq -r '.stop_hook_active // false')" = "true" ] && exit 0

session=$(printf '%s' "$input" | jq -r '.session_id // empty')
transcript=$(printf '%s' "$input" | jq -r '.transcript_path // empty')
[ -n "$session" ] && [ -n "$transcript" ] && [ -r "$transcript" ] || exit 0

# Without this marker the recall hook never reached the knowledge base
# this session, and a nudge would send the agent to write into a wall.
[ -e "${TMPDIR:-/tmp}/ochakai-up-$session" ] || exit 0

# Nudge at most once per session.
marker="${TMPDIR:-/tmp}/ochakai-write-back-$session"
[ -e "$marker" ] && exit 0

# Only sessions that worked on the tree deserve the interruption.
grep -qE 'scripts/check|docs/design|internal/|cmd/ochakai|deploy/|webui' "$transcript" || exit 0
: >"$marker"

recalled_file="${TMPDIR:-/tmp}/ochakai-recalled-$session"
recalled=""
[ -r "$recalled_file" ] && recalled=$(sort -u "$recalled_file" 2>/dev/null | head -20 | tr '\n' ' ')

outcome_reason=""
if [ -n "$recalled" ]; then
	outcome_reason="This session was pointed at these ochakai concepts: $recalled — if you acted on one (followed its procedure, relied on its claim), report whether it held up with the report_outcome MCP tool (worked, or failed with a note saying what went wrong). Skip any you only read but never acted on. "
fi

jq -n --arg outcome "$outcome_reason" '{
  decision: "block",
  reason: ($outcome + "Before finishing: if this session learned something durable about ochakai — how a failure was diagnosed, a constraint of an environment, a convention written nowhere — write it back to the knowledge base as a draft with the put_concept MCP tool. Search first with search_concepts (including rejected=true) to avoid re-proposing what was already turned down. Use the MCP tools, never the ochakai CLI, for writes and reports: the MCP connection carries your process identity, the CLI would record you as the anonymous human (kb/bundle/policies/ai-human-identity.md). If nothing durable was learned, finish now without creating anything — do not invent knowledge.")
}'
