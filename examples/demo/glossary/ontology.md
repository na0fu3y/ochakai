---
type: Glossary Term
title: オントロジー
description: Palantir Foundry が Ontology と呼ぶものを、このバンドルはどこで持っているか — 意味層はconceptとリンク、動力層は契約と実行者の分離
tags: [ontology, glossary, meta]
sources:
  - id: foundry
    resource: https://www.palantir.com/docs/foundry/ontology/core-concepts
    title: "Palantir Foundry: Ontology core concepts"
generated: { by: human:sato@example.co.jp, at: 2026-07-30T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-01T01:00:00Z }
status: stable
---

オントロジーとは、データと判断と行動を一枚につなぐ層のことである —
Palantir Foundry はこれを製品の中心に置き、**意味層**(object・
property・link)と**動力層**(action・function)に分けて説明する。
[^foundry] このバンドルは同じものを、プラットフォームではなく
**ナレッジベースと実行エージェントの分業**として持つ。対応はこうなる:

| Foundry の概念 | このバンドルでの居場所 |
|---|---|
| Object type | テーブルの concept([注文明細](/tables/order-items.md)など)と、その意味を決める用語([完了した注文](/glossary/completed-order.md)) |
| Property | 列の注記と、列から導いた判断を持つ [Metric](/metrics/revenue.md) |
| Link type | concept 同士の markdown リンク。宣言せず、書けばグラフになる |
| Shared property | 同じ語が複数のオブジェクトで同じ意味を持つこと — `status` は注文にも明細にもあり、意味は用語集が一箇所で決める |
| Function | [Attested Computation](/queries/sales/monthly-revenue.md) — 承認された計算。実行は外 |
| Action type | [actions/ の concept](/actions/review-high-return-products.md) — パラメータ・検証・副作用の契約 |
| Object view | `ochakai get` の一枚。`linked_from` が「これを読む前に知るべきこと」を運んでくる |
| Roles | trust tier — draft は書いたエージェントの名で残り、verified は人の裁定である |

## どこが違うか

Foundry はオントロジーをプラットフォームの中で実行する。ochakai は
**実行しない** — 中に LLM も SQL 実行系も無く、持つのは意味と契約と
裁定だけである。実行するのは手元のエージェント(このバンドルの想定は
Claude Code)で、[計算の実行](/skills/run-bigquery-query.md)も
[アクションの実行](/skills/run-an-action.md)も、エージェントが receipt
を持って帰ってくる契約として書かれている。

だから動力層はこう分解される: **何をしてよいか**は action の concept
が持ち、**どうやるか**は skill が持ち、**やってよかったか**は人の
verify と `report_outcome` が持つ。書き戻しは draft で入り、裁定は
必ず人に残る — Foundry が権限モデルでやることを、このバンドルは
provenance と trust tier でやる。

一周して見たい人へ: [thelook_ecommerce](/references/thelook-dataset.md)
が世界、[売上](/metrics/revenue.md)が property、
[月次売上](/queries/sales/monthly-revenue.md)が function、
[返品率の高い商品の見直し](/actions/review-high-return-products.md)が
action である。読む順もそれでよい。

[^foundry]: Palantir Foundry: Ontology core concepts
