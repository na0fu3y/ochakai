#!/bin/sh
# session-start: SessionStart hook — make the store tests runnable in the
# Claude Code cloud environment, and say what still cannot be checked there.
#
# That environment ships the docker CLI without a daemon, and starting
# dockerd does not help: its egress policy answers 403 for Docker Hub's
# blob CDN, so pgvector/pgvector:pg17 can never be pulled and the store
# integration tests would silently skip for the whole session.
# `scripts/check --db` already falls back to a cluster built from the
# machine's own PostgreSQL; what that fallback needs and a stock image
# lacks is pgvector beside it. One package, installed here rather than
# per session, because the container image is cached once this completes.
#
# Only the cloud environment is touched (CLAUDE_CODE_REMOTE) — a hook that
# installed system packages on a contributor's laptop would be a bug. It
# is idempotent, and it fails silently: a hook must never be the reason a
# session refuses to start.
#
# shellcheck disable=SC2016 # the notes below are prose: the backticks are
# Markdown, and $HTTPS_PROXY is a command for the agent to run, not for
# this script to expand.
set -eu

[ "${CLAUDE_CODE_REMOTE:-}" = "true" ] || exit 0
command -v jq >/dev/null 2>&1 || exit 0

# Everything below is written to a log, never to stdout: stdout is the
# hook's JSON reply.
log=${TMPDIR:-/tmp}/ochakai-session-start.log

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
