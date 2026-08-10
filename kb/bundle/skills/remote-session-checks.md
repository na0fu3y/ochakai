---
type: Skill
title: リモートセッションで正直に主張できるチェック
description: Claude Code のクラウド環境で scripts/check の何が本物で、何が CI に任せるものかの線引き
tags: [dogfood, runbook, environment]
status: draft
sources:
  - id: hook
    resource: .claude/hooks/session-start.sh
    title: この線引きをセッション開始時に注入するフック
  - id: contributing
    resource: CONTRIBUTING.md
    title: Working with Claude Code の節
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

Claude Code のクラウドセッション(`CLAUDE_CODE_REMOTE=true`)は egress
ポリシーの都合で、リポジトリのチェックの一部しか本物として走らせられ
ない。緑を報告してよいものとそうでないものの線引き:

- **`scripts/check core` と `scripts/check lint` は本物。** この二つが
  緑なら、そう主張してよい。
- **store 統合テストも走る**が、CI と同じ DB ではない。`scripts/check
  --db` は Docker が使えないとき、マシンにインストール済みの PostgreSQL
  から使い捨てクラスタを組む。締めの行が実際に使った DB を名乗るのは、
  それが pgvector/pgvector:pg17(CI の DB)ではないからである。共有 DB
  前提の書き方は [store テストの規約](/insights/store-tests-shared-db.md)。
- **Docker は動かない。** CLI はあるがデーモンが無く、dockerd を起こして
  も Docker Hub の blob CDN に 403 が返るのでイメージは引けない。
  Dockerfile と deploy/compose.yaml の検証は CI に任せ、セッションでは
  時間を使わない。
- **`scripts/check vuln` の `Forbidden` は脆弱性ではない。** プロキシが
  vuln.go.dev を拒んでいるだけである。`curl -sS
  "$HTTPS_PROXY/__agentproxy/status"` で確かめた上で「ここでは検証不能」
  と報告する — 「チェックが落ちた」とは決して報告しない。

この線引き自体はセッション開始フックが注入するが、フックが切れて
いる・別の環境にいるときにも同じ判断ができるよう、ここに置く。
