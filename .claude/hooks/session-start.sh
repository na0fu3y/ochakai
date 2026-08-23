#!/bin/sh
# session-start: SessionStart hook. Two environments, two jobs:
#
# - On a contributor's machine, bring the dogfood instance up
#   (kb/README.md). The loop's other two hooks are silent by design when
#   nothing answers on :8080, and a Docker restart leaves the compose
#   stack down — so for most of its first fortnight the loop was
#   installed and not running, and nobody could tell from inside a
#   session. `docker compose up -d` is idempotent and the stack binds
#   loopback only, so starting it is safe to repeat; a machine with no
#   Docker daemon is left alone, and the note says so.
# - In the Claude Code cloud environment, make the store tests runnable,
#   and say what still cannot be checked there.
#
# The cloud half: that environment ships the docker CLI without a daemon,
# and starting dockerd does not help: its egress policy answers 403 for
# Docker Hub's blob CDN, so pgvector/pgvector:pg17 can never be pulled and
# the store integration tests would silently skip for the whole session.
# `scripts/check --db` already falls back to a cluster built from the
# machine's own PostgreSQL; what that fallback needs and a stock image
# lacks is pgvector beside it. One package, installed here rather than
# per session, because the container image is cached once this completes.
#
# Only the cloud environment has packages installed (CLAUDE_CODE_REMOTE) —
# a hook that installed system packages on a contributor's laptop would be
# a bug. Both halves are idempotent, and both fail silently: a hook must
# never be the reason a session refuses to start.
#
# shellcheck disable=SC2016 # the notes below are prose: the backticks are
# Markdown, and $HTTPS_PROXY is a command for the agent to run, not for
# this script to expand.
set -eu

command -v jq >/dev/null 2>&1 || exit 0

# say prints one note for the agent and exits. Everything else this
# script writes goes to a log, never to stdout: stdout is the hook's JSON
# reply.
say() {
	jq -nc --arg t "$1" '{
		hookSpecificOutput: {
			hookEventName: "SessionStart",
			additionalContext: $t,
		},
	}'
	exit 0
}

# Everything below is written to a log, never to stdout: stdout is the
# hook's JSON reply.
log=${TMPDIR:-/tmp}/ochakai-session-start.log

# --- A contributor's machine: the dogfood instance -------------------

: "${OCHAKAI_URL:=http://localhost:8080}"

# concepts prints how many concepts the instance holds, or nothing if it
# is not answering.
concepts() {
	curl -sf -m 3 "$OCHAKAI_URL/api/v1/stats" 2>/dev/null | jq -r '.concepts.total // empty' 2>/dev/null
}

dogfood_local() {
	root=$(cd "$(dirname "$0")/../.." && pwd)
	compose="$root/deploy/compose.yaml"
	[ -f "$compose" ] || exit 0
	verb="is up"
	if ! n=$(concepts) || [ -z "$n" ]; then
		# Not answering. Only Docker can bring it up, and only when the
		# port is free: something else on :8080 is not ours to replace.
		command -v docker >/dev/null 2>&1 || exit 0
		docker info >/dev/null 2>&1 || say "The dogfood ochakai instance is down and Docker is not running, so this session has no recall and no write-back (kb/README.md). Start Docker and run \`docker compose -f deploy/compose.yaml up -d\` to have both."
		if nc -z localhost 8080 >/dev/null 2>&1; then
			say "Something answers on :8080 that is not ochakai (no /api/v1/stats), so the dogfood instance was not started; this session has no recall and no write-back."
		fi
		{
			echo "=== $(date -u +%FT%TZ) docker compose up -d ($compose)"
			docker compose -f "$compose" up -d
		} >>"$log" 2>&1 || say "\`docker compose -f deploy/compose.yaml up -d\` failed (see $log), so this session has no recall and no write-back (kb/README.md)."
		i=0
		until n=$(concepts) && [ -n "$n" ]; do
			i=$((i + 1))
			[ "$i" -lt 25 ] || say "The dogfood instance is starting in the background (docker compose, see $log) and was not answering yet; recall begins with the first prompt it answers."
			sleep 1
		done
		verb="was started"
	fi
	# Seed an empty instance from the bundle in git — but only a bundle
	# that carries no verified key. Import re-records a document's
	# verification as the importer's own (design doc 0043 §3.2), which is
	# why kb/README.md leaves import to a person; a bundle of drafts
	# verifies nothing, so loading it rules on nothing.
	if [ "$n" = 0 ]; then
		if grep -rqE '^verified(_at)?:' "$root/kb/bundle"; then
			say "The dogfood ochakai instance $verb at $OCHAKAI_URL and is empty, and kb/bundle carries verified keys — loading it is a ruling, so a person runs \`ochakai import kb/bundle\` (kb/README.md). Until then recall has nothing to find."
		fi
		command -v ochakai >/dev/null 2>&1 || say "The dogfood ochakai instance $verb at $OCHAKAI_URL and is empty, and there is no ochakai CLI on PATH to load kb/bundle with (\`go install ./cmd/ochakai\`, then \`ochakai import kb/bundle\`)."
		{
			echo "=== $(date -u +%FT%TZ) ochakai import kb/bundle"
			OCHAKAI_URL=$OCHAKAI_URL ochakai import "$root/kb/bundle"
		} >>"$log" 2>&1 || say "The dogfood ochakai instance $verb at $OCHAKAI_URL and is empty; loading kb/bundle failed (see $log)."
		n=$(concepts) || n=0
		verb="$verb and was loaded from kb/bundle as drafts (nothing verified, so nothing ruled on)"
	fi
	say "The dogfood ochakai instance $verb at $OCHAKAI_URL: $n concepts (kb/README.md). Recall runs on each prompt; fetch with the ochakai MCP get_concept tool, never the CLI."
}

[ "${CLAUDE_CODE_REMOTE:-}" = "true" ] || dogfood_local

# --- The cloud environment: the store tests --------------------------

docker_note='Two things this environment cannot check, both because of its
egress policy rather than anything in the tree:

- Docker cannot run a container. The CLI is installed but no daemon
  answers, and starting dockerd does not help: the policy replies 403 for
  Docker Hub'"'"'s blob CDN, so no image is pullable. Do not spend a turn on
  it. What that costs is the Dockerfile and deploy/compose.yaml — leave
  those to CI.
- `scripts/check vuln` may fail with `Forbidden` fetching vuln.go.dev.
  That is the proxy refusing the host, not a vulnerability. Confirm with
  `curl -sS "$HTTPS_PROXY/__agentproxy/status"`, then report it as
  unverifiable here — never as a failing check, and never work around it.

So `scripts/check core` and `scripts/check lint` are the ones to run, and
a green run of those is what you can honestly claim.'

note() {
	jq -nc --arg t "$docker_note

$1" '{
		hookSpecificOutput: {
			hookEventName: "SessionStart",
			additionalContext: $t,
		},
	}'
	exit 0
}

# pgvector, beside any PostgreSQL this machine has.
have_pgvector() {
	for d in /usr/lib/postgresql/*/bin; do
		[ -x "$d/pg_config" ] || continue
		[ -f "$("$d/pg_config" --sharedir)/extension/vector.control" ] || continue
		return 0
	done
	return 1
}

# The newest PostgreSQL major version installed, if any.
pg_major() {
	v=""
	for d in /usr/lib/postgresql/*/bin; do
		if [ -x "$d/pg_config" ]; then v=$(basename "$(dirname "$d")"); fi
	done
	[ -n "$v" ] || return 1
	echo "$v"
}

works='The store integration tests do run here. `scripts/check --db` starts
a throwaway cluster from the PostgreSQL installed on this machine instead
of the container CI uses; its closing line names which database it ran
against, because that one is not pgvector/pgvector:pg17.'

if have_pgvector; then
	note "$works"
fi

command -v apt-get >/dev/null 2>&1 || note 'The store integration tests
cannot run here: Docker cannot pull a database image, and this machine has
no PostgreSQL with pgvector for `scripts/check --db` to fall back on. Say
so rather than reporting a `scripts/check` run as the run CI will do.'

install() {
	DEBIAN_FRONTEND=noninteractive apt-get install -y -q "$@" >>"$log" 2>&1
}

{
	echo "=== $(date -u +%FT%TZ) ensuring PostgreSQL with pgvector"
} >>"$log" 2>&1

# A first install without updating the package lists: on an image whose
# lists are still current that is the whole job, and it is much the faster
# half of the two.
if ! v=$(pg_major); then
	install postgresql || {
		apt-get update -q >>"$log" 2>&1 || :
		install postgresql || :
	}
	v=$(pg_major) || note 'The store integration tests cannot run here:
Docker cannot pull a database image, and installing PostgreSQL locally for
`scripts/check --db` to fall back on failed too. Say so rather than
reporting a `scripts/check` run as the run CI will do.'
fi

install "postgresql-$v-pgvector" || {
	apt-get update -q >>"$log" 2>&1 || :
	install "postgresql-$v-pgvector" || :
}

if have_pgvector; then
	note "$works"
fi

note 'The store integration tests cannot run here: Docker cannot pull a
database image, and pgvector could not be installed beside the local
PostgreSQL for `scripts/check --db` to fall back on (see '"$log"'). Say so
rather than reporting a `scripts/check` run as the run CI will do.'
