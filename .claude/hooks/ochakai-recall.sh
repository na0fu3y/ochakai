#!/bin/sh
# ochakai-recall: UserPromptSubmit hook — surface pointers to relevant
# knowledge from this repository's own dogfood instance (kb/README.md).
# Adapted from examples/claude-code/hooks/ochakai-recall.sh; the changes
# are the default server (the compose harness, so a fresh clone needs no
# `ochakai use`), a reachability marker the write-back hook reads, and
# the fetch instruction pointing at the MCP tool — on the dev instance
# CLI reads are the anonymous human, while the MCP connection carries
# the agent's process identity
# (kb/bundle/policies/ai-human-identity.md).
#
# What this injects is the search ranking, not the knowledge itself
# (design doc 0108): the fetch is the agent's own move, and the fetched
# concept names what links at it under linked_from. Whatever this prints
# on stdout is added to the context; printing nothing adds nothing.
# Failures are silent by design: a knowledge base being down must never
# block the user's prompt.
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

session=$(printf '%s' "$input" | jq -r '.session_id // empty' 2>/dev/null) || session=""

if ! hits=$(ochakai search "$prompt" --limit "${OCHAKAI_RECALL_LIMIT:-8}" --json 2>/dev/null); then
	# Down. Say so once per session rather than never: for a fortnight
	# this hook's silence read as "nothing relevant" while the instance
	# was simply not running. session-start.sh brings it up when Docker
	# is there; this is the line for when it could not.
	[ -n "$session" ] || exit 0
	marker="${TMPDIR:-/tmp}/ochakai-down-$session"
	[ -e "$marker" ] && exit 0
	: >"$marker" 2>/dev/null || true
	printf 'The dogfood ochakai instance at %s is not answering, so this session has no recall and no write-back (kb/README.md). `docker compose -f deploy/compose.yaml up -d` brings it up; say so to the user rather than working as if there were nothing to recall.\n' "$OCHAKAI_URL"
	exit 0
fi

if [ -n "$session" ]; then
	# The search answered, so the write-back nudge has somewhere to send
	# the agent — even when the answer was empty.
	: >"${TMPDIR:-/tmp}/ochakai-up-$session" 2>/dev/null || true
fi

rows=$(printf '%s' "$hits" | jq -r '.hits[]? |
	"- ochakai://\(.id) [\(.type), \(.trust)]\(if .description and .description != "" then " — " + .description elif .snippet and .snippet != "" then " — " + .snippet else "" end)"' 2>/dev/null) || exit 0
[ -z "$rows" ] && exit 0

if [ -n "$session" ]; then
	{ printf '%s' "$hits" | jq -r '.hits[]?.id | "ochakai://" + .' \
		>>"${TMPDIR:-/tmp}/ochakai-recalled-$session"; } 2>/dev/null || true
fi

printf 'Knowledge in this repository'"'"'s ochakai instance that may bear on this request — pointers, not the knowledge itself. Fetch what you rely on with the ochakai MCP get_concept tool (never the CLI, per the identity policy); a fetched concept lists what links at it under linked_from. Trust verified concepts; judge drafts by created_by:\n\n%s\n' "$rows"
