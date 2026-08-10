---
type: Skill
title: Docker なしで dogfood インスタンスを立てる
description: compose が使えない環境(リモートセッション等)で、ローカル PostgreSQL + serve + MCP over HTTP により dogfood ループを回す手順
tags: [dogfood, runbook, environment]
status: draft
sources:
  - id: loop
    resource: kb/bundle/skills/run-the-dogfood-loop.md
    title: 正規の手順(compose 版)
  - id: check
    resource: scripts/check
    title: ローカル PostgreSQL から使い捨てクラスタを組む先例
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

[dogfood ループ](/skills/run-the-dogfood-loop.md)の正規の立て方は
docker compose だが、Docker が動かない環境
([リモートセッション](/skills/remote-session-checks.md))でも、
scripts/check --db と同じやり方でインスタンスは立つ:

```sh
BIN=/usr/lib/postgresql/16/bin   # pgvector が隣にあること
mkdir -p /tmp/kbdb && chown postgres /tmp/kbdb
su postgres -s /bin/sh -c "$BIN/initdb -D /tmp/kbdb/data -U t --auth=trust --no-sync"
su postgres -s /bin/sh -c "$BIN/pg_ctl -D /tmp/kbdb/data -o '-p 55434 -c listen_addresses=localhost -F' -l /tmp/kbdb/pg.log start"
$BIN/createdb -h localhost -p 55434 -U t t
go build -o /tmp/kbdb/ochakai ./cmd/ochakai
OCHAKAI_DATABASE_URL='postgres://t:t@localhost:55434/t?sslmode=disable' \
  OCHAKAI_MODE=dev PORT=8080 /tmp/kbdb/ochakai serve &
```

三つの癖:

- **データディレクトリは /tmp 直下に。** scratchpad 配下は postgres
  ユーザーが親ディレクトリを辿れず `Permission denied` になる。
- **AI の書き込みは MCP over HTTP で。** `.mcp.json` が無い文脈(素の
  curl)でも、`POST /mcp` に JSON-RPC を送り
  `Ochakai-On-Behalf-Of: process:claude-code` を毎リクエストに載せれば
  [identity 規律](/policies/ai-human-identity.md)は保てる。initialize の
  応答ヘッダ `Mcp-Session-Id` を以後のリクエストに返すこと。
- **このインスタンスは使い捨てである。** 検証台帳(trust)は pgdata に
  しか住まないという正規手順の注意がそのまま効く — セッションが死ねば
  台帳は消える。書いた draft を残すには export して git に載せるしか
  ない。だから裁定(verify/reject)はここでは打つ意味がなく、レビューは
  export 後の PR diff で人が行う。
