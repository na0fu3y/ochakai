---
type: Skill
title: アクションを実行する
description: actions/ の concept を動かす手順 — 検証、実行、決定草案の書き戻し、そして人の裁定で止まること
tags: [action, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-08-03T05:30:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-05T01:20:00Z }
status: stable
---

`actions/` の concept が `executor` として名指す手順である。いま名指して
いるのは[返品率の高い商品の見直し](/actions/review-high-return-products.md)
ひとつ。計算だけの concept との違いは、実行の出口が**決定草案の書き
戻し**であることで、Palantir の語彙でこれが何にあたるかは
[オントロジー](/glossary/ontology.md)にある。

## 手順

1. **検証。** action の concept の「検証」節にあるパラメータの範囲を
   確かめる。範囲の外なら実行しない — パラメータと理由を添えて人へ
   返す。検証を実行のあとに回さないこと。
2. **実行。** `# Computation` を
   [BigQuery の計算を実行する](/skills/run-bigquery-query.md)の規律
   そのままで実行する。SQL は書き換えない、receipt は全部持ち帰る。
3. **0 行なら終わり。** 候補が無いのも結果である。receipt を返して
   止まる。草案は書かない — 空の決定に裁定を求めない。
4. **決定草案を書く。** 候補の表、束縛したパラメータ、根拠の `job_id`
   を本文に入れて、`decisions/` の下に **draft** で書く(id は
   `decisions/YYYY-MM-<何の件か>`)。draft のままにする — 状態を上げる
   のはエージェントの仕事ではない。
5. **receipt を返す。** 宣言どおり `job_id`・`executed_sql`・
   `row_count` に加えて、書いた草案の `draft_id`。
6. **結果を報告する。** 裁定が出て実際に動いたあと、その顛末を
   `report_outcome`(CLI なら `ochakai report`)で草案に記録する。
   worked か failed かが、次に同じアクションを回す者への前例になる。

## 止まる場所

草案が verified になるまで、世界の側では何も動かない。商品の取り扱い
を変える・値を下げる・仕入れを止める — そういう副作用はすべて、人が
草案に裁定を下した**あと**、その持ち場のチームが行う。エージェントが
やってよいのは、ここまでの 6 手だけである。

ochakai はこの手順を保存しているだけで、実行も裁定もしない。実行者は
このバンドルの想定では Claude Code であり、裁定は必ず人である。
