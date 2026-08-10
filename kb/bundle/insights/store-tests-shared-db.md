---
type: Insight
title: store 統合テストは共有 DB 前提で書く
description: id は internal/testdb.Unique から取る — 自前の名前空間はコーパスを太らせ、無関係な検索テストを壊す
tags: [dogfood, testing]
status: draft
sources:
  - id: contributing
    resource: CONTRIBUTING.md
    title: Tests and checks の節
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

store 統合テストの DB は共有される: CI は 1 コンテナを 1 run で使い回し、
手元では 1 つを立てっぱなしにする(port 55433、`OCHAKAI_TEST_DATABASE_URL`)。
だから **テストの id は `internal/testdb.Unique` から取る** — run ごとに
一意な番号が付き、その下に書いた全行を掃除する sweep も登録される。

自前のトークンで名前空間を切ると行が残り、コーパスが黙って太る。壊れる
のは自分のテストではなく、**どこか別の、件数を絞った ranking の assertion**
である — 自分の concept が上位から押し出された変更が「検索のバグ」として
落ち始める。この故障の読みにくさが、`TestNoTestRollsItsOwnNamespace` が
自前の名前空間を機械的に落とす理由である。
