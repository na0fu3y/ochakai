#!/bin/sh
# ochakai-recall: UserPromptSubmit hook for Claude Code.
#
# Injects relevant team knowledge into the agent's context before it
# starts working — automatic recall, no LLM involved, no agent judgment
# required. Whatever this script prints on stdout is added to the
# context; printing nothing adds nothing.
#
# Also records which concepts were recalled this session (one ID per
# line, appended to a per-session file under $TMPDIR) so ochakai-write-back.sh
# can later ask, specifically, whether any of them held up when acted on —
# report_outcome is a loop move too, and this is what gives it a hook.
#
# Requires: jq, and ochakai on PATH with a server selected
# (`ochakai use <url>`). Tune with:
#   OCHAKAI_RECALL_BUDGET     max injected bytes (default 4000)
#
# Failures are silent by design: a knowledge base being down must never
# block the user's prompt.
set -eu

input=$(cat)
prompt=$(printf '%s' "$input" | jq -r '.prompt // empty' 2>/dev/null) || exit 0
[ -z "$prompt" ] && exit 0
case $prompt in
/*) exit 0 ;; # slash commands are not data questions
esac

pack=$(ochakai context "$prompt" --budget "${OCHAKAI_RECALL_BUDGET:-4000}" 2>/dev/null) || exit 0
[ -z "$pack" ] && exit 0

session=$(printf '%s' "$input" | jq -r '.session_id // empty' 2>/dev/null) || session=""
if [ -n "$session" ]; then
	# Only concepts rendered in full ("## ochakai://…" headings) count as
	# recalled — the "Also relevant" one-liners are pointers, not knowledge
	# the agent was actually handed.
	{ printf '%s\n' "$pack" | grep -o '^## ochakai://[^ ]*' | sed 's/^## //' \
		>>"${TMPDIR:-/tmp}/ochakai-recalled-$session"; } 2>/dev/null || true
fi

printf 'Team knowledge from ochakai relevant to this request (trust verified concepts; judge drafts by created_by):\n\n%s\n' "$pack"
