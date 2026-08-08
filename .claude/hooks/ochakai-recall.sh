#!/bin/sh
# ochakai-recall: UserPromptSubmit hook — inject relevant knowledge from
# this repository's own dogfood instance (kb/README.md) into the agent's
# context. Adapted from examples/claude-code/hooks/ochakai-recall.sh; the
# changes are the default server (the compose harness, so a fresh clone
# needs no `ochakai use`) and a reachability marker the write-back hook
# reads, so it never nudges a session whose knowledge base was down.
#
# Whatever this prints on stdout is added to the context; printing
# nothing adds nothing. Failures are silent by design: a knowledge base
# being down must never block the user's prompt.
set -eu

command -v ochakai >/dev/null 2>&1 || exit 0
command -v jq >/dev/null 2>&1 || exit 0
: "${OCHAKAI_URL:=http://localhost:8080}"
export OCHAKAI_URL

input=$(cat)
prompt=$(printf '%s' "$input" | jq -r '.prompt // empty' 2>/dev/null) || exit 0
[ -z "$prompt" ] && exit 0
case $prompt in
/*) exit 0 ;; # slash commands are not knowledge questions
esac

pack=$(ochakai context "$prompt" --budget "${OCHAKAI_RECALL_BUDGET:-4000}" 2>/dev/null) || exit 0

session=$(printf '%s' "$input" | jq -r '.session_id // empty' 2>/dev/null) || session=""
if [ -n "$session" ]; then
	# The context call answered, so the write-back nudge has somewhere
	# to send the agent — even when the answer was empty.
	: >"${TMPDIR:-/tmp}/ochakai-up-$session" 2>/dev/null || true
fi
[ -z "$pack" ] && exit 0

if [ -n "$session" ]; then
	# Only concepts rendered in full ("## ochakai://…" headings) count as
	# recalled — the "Also relevant" one-liners are pointers, not knowledge
	# the agent was actually handed.
	{ printf '%s\n' "$pack" | grep -o '^## ochakai://[^ ]*' | sed 's/^## //' \
		>>"${TMPDIR:-/tmp}/ochakai-recalled-$session"; } 2>/dev/null || true
fi

printf 'Team knowledge from ochakai relevant to this request (trust verified concepts; judge drafts by created_by):\n\n%s\n' "$pack"
