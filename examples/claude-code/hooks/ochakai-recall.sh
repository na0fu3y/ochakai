#!/bin/sh
# ochakai-recall: UserPromptSubmit hook for Claude Code.
#
# Injects relevant team knowledge into the agent's context before it
# starts working — automatic recall, no LLM involved, no agent judgment
# required. Whatever this script prints on stdout is added to the
# context; printing nothing adds nothing.
#
# Requires: jq, and ochakai on PATH with a server selected
# (`ochakai use <url>`). Tune with:
#   OCHAKAI_RECALL_BUDGET     max injected bytes (default 4000)
#   OCHAKAI_RECALL_MIN_SCORE  drop hits below this score (default 0 = off).
#     Scores depend on the server's search mode and your corpus — English
#     trigram matches score far higher than CJK ones — so measure with
#     `ochakai search ... | cut -f1` before picking a floor.
#
# Failures are silent by design: a knowledge base being down must never
# block the user's prompt.
set -eu

prompt=$(jq -r '.prompt // empty' 2>/dev/null) || exit 0
[ -z "$prompt" ] && exit 0
case $prompt in
/*) exit 0 ;; # slash commands are not data questions
esac

pack=$(ochakai context "$prompt" --budget "${OCHAKAI_RECALL_BUDGET:-4000}" \
	--min-score "${OCHAKAI_RECALL_MIN_SCORE:-0}" 2>/dev/null) || exit 0
[ -z "$pack" ] && exit 0

printf 'Team knowledge from ochakai relevant to this request (trust verified entries; judge drafts by created_by):\n\n%s\n' "$pack"
