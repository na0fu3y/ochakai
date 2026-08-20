---
type: Skill
title: アクションを実行する
description: actions/ の concept を動かす手順。検証、実行、決定草案の書き戻し、そして人の裁定で止まること
tags: [action, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-08-03T05:30:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-05T01:20:00Z }
status: stable
---

`actions/` の concept が `executor` として名指す手順である。いま名指して
いるのは[返品率の高い商品の見直し](/actions/review-high-return-products.md)
ひとつ。計算だけの concept との違いは、実行の出口が決定草案の書き戻しに
なることで、Palantir の語彙でこれが何にあたるかは
[オントロジー](/glossary/ontology.md)にある。

## 手順

1. **検証。** action の concept の「検証」節にあるパラメータの範囲を確かめ
   る。範囲の外なら実行せず、パラメータと理由を添えて人へ返す。検証を実行
   のあとに回さないこと。
2. **実行。** `# Computation` を
   [BigQuery の計算を実行する](/skills/run-bigquery-query.md)の規律その
   ままで実行する。SQL は書き換えず、receipt は全部持ち帰る。
3. **0 行なら終わり。** 候補が無いのも結果である。receipt を返して止まり、
   草案は書かない。空の決定に裁定を求めない。
4. **決定草案を書く。** 候補の表、束縛したパラメータ、根拠の `job_id` を
   本文に入れて、`decisions/` の下に draft で書く(id は
   `decisions/YYYY-MM-<何の件か>`)。draft のままにすること。状態を上げるの
   はエージェントの仕事ではない。
5. **receipt を返す。** 宣言どおりの `job_id`・`executed_sql`・`row_count`
   に加えて、書いた草案の `draft_id` を返す。
6. **結果を報告する。** 裁定が出て実際に動いたあと、その顛末を
   `report_outcome`(CLI なら `ochakai report`)で草案に記録する。worked か
   failed かが、次に同じアクションを回す者への前例になる。

## 止まる場所

草案が verified になるまで、世界の側では何も動かない。商品の取り扱いを
変える、値を下げる、仕入れを止めるといった副作用はすべて、人が草案に裁定
を下したあとに、その持ち場のチームが行う。エージェントがやってよいのは、
ここまでの 6 手だけである。

ochakai はこの手順を保存しているだけで、実行も裁定もしない。実行者はこの
バンドルの想定では Claude Code であり、裁定は必ず人である。
