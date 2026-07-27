#!/bin/sh
# gofmt: PostToolUse hook — format Go files the agent just wrote.
#
# `gofmt -l .` is the first thing CI runs and the cheapest failure to
# avoid, so this closes the loop at the edit rather than at the push.
# It reformats in place and says so, because the agent's copy of the
# file is now stale and it needs to re-read before editing again.
#
# Requires jq (the hook payload is JSON). Failures are silent: a hook
# must never be the reason an edit appears to fail.
set -eu

command -v jq >/dev/null 2>&1 || exit 0
command -v gofmt >/dev/null 2>&1 || exit 0

payload=$(cat)
file=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty') || exit 0

case $file in
*.go) ;;
*) exit 0 ;;
esac
[ -f "$file" ] || exit 0

# Nothing to say when the file was already formatted, which is the
# common case — keep the transcript quiet.
[ -n "$(gofmt -l "$file" 2>/dev/null)" ] || exit 0

gofmt -w "$file" 2>/dev/null || exit 0

jq -nc --arg f "$file" '{
	hookSpecificOutput: {
		hookEventName: "PostToolUse",
		additionalContext: ("gofmt reformatted \($f) in place. Re-read it before editing again — your copy is stale."),
	},
}'
