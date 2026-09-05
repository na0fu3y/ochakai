#!/bin/sh
# ochakai-recall: UserPromptSubmit hook for Claude Code.
#
# Surfaces pointers to relevant team knowledge before the agent starts
# working — automatic recall, no LLM involved, no agent judgment
# required. What this injects is the search ranking, not the knowledge
# itself (design doc 0108): rows the agent follows with `ochakai get`,
# so a fetch is a choice the agent makes — and the concept it fetches
# names what links at it under linked_from, which is where the caveats
# live.
#
# Also records which concepts were surfaced this session (one ID per
# line, appended to a per-session file under $TMPDIR) so
# ochakai-write-back.sh can later ask, specifically, whether any of them
# held up when acted on — report_outcome is a loop move too, and this is
# what gives it a hook.
#
# Requires: jq, and ochakai on PATH with a server selected
# (`ochakai use <url>`). Tune with:
#   OCHAKAI_RECALL_LIMIT      max pointer rows (default 8)
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

hits=$(ochakai search "$prompt" --limit "${OCHAKAI_RECALL_LIMIT:-8}" --json 2>/dev/null) || exit 0
# A row says whether the writer's re-check date has passed, because the
# ranking does not: an expired concept is due for re-checking, not wrong,
# so it is neither hidden nor demoted (design doc 0069 §7) — the agent is
# told, and chooses. Compared at date granularity so both spellings of an
# OKF instant work (0139), and today counts as passed: the date names the
# UTC midnight that opens it (0133).
today=$(date -u +%Y-%m-%d)
rows=$(printf '%s' "$hits" | jq -r --arg today "$today" '.hits[]? |
	"- ochakai://\(.id) [\(.type), \(.trust)\(if (.stale_after // "") == "" then "" elif (.stale_after[:10] <= $today) then ", past stale_after" else "" end)]\(if .description and .description != "" then " — " + .description elif .snippet and .snippet != "" then " — " + .snippet else "" end)"' 2>/dev/null) || exit 0
[ -z "$rows" ] && exit 0

session=$(printf '%s' "$input" | jq -r '.session_id // empty' 2>/dev/null) || session=""
if [ -n "$session" ]; then
	{ printf '%s' "$hits" | jq -r '.hits[]?.id | "ochakai://" + .' \
		>>"${TMPDIR:-/tmp}/ochakai-recalled-$session"; } 2>/dev/null || true
fi

printf 'Team knowledge in ochakai that may bear on this request — pointers, not the knowledge itself. Fetch what you rely on before answering (`ochakai get <id>`); a fetched concept lists the concepts that link at it under linked_from, which is where the caveat that says how to read a number lives. Trust verified concepts; `ochakai get` prints who wrote and who confirmed one on stderr, which is how a draft is judged. A row marked `past stale_after` is due for re-checking, not wrong — prefer a fresh concept when both answer, and say which you used. Cite what you use: name the concept id and whether a human confirmed it, so the reader can check the answer and knows which concept to fix:\n\n%s\n' "$rows"
