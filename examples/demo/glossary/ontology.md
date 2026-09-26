---
type: Glossary Term
title: オントロジー
description: Palantir Foundry が Ontology と呼ぶものを、この OKF バンドルはどこで持っているか。専用の型は作らず、テーブル・リンク・知見・計算・アクションの分業で持つ
tags: [ontology, glossary, meta]
sources:
  - id: foundry
    resource: https://www.palantir.com/docs/foundry/ontology/core-concepts
    title: "Palantir Foundry: Ontology core concepts"
generated: { by: human:sato@example.co.jp, at: 2026-07-30T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-22T01:30:00Z }
status: stable
---

オントロジーとは、データと判断と行動を一枚につなぐ層のことである。
Palantir Foundry はこれを製品の中心に置き、意味層(object・property・
link)と動力層(action・function)に分けて説明している。[^foundry]
このバンドルは同じものを、専用の型を増やさず、OKF の普通の concept と
その間のリンクで持つ。

| Foundry の概念 | このバンドルでの居場所 |
|---|---|
| Object type | テーブルの concept([order_items](/tables/order-items.md) など)。OKF のサンプルと同じく、1 行が何かと `# Schema` を持つ |
| Property | `# Schema` の列の注記と、列から導いた [Metric](/metrics/revenue.md) |
| Link type | テーブルの `# Joins` 節からのリンクと、自明でない結合についての Insight([inventory_item_id は売れた在庫を指さない](/insights/inventory-item-id.md)) |
| Function | [Attested Computation](/computations/revenue-drivers.md)。承認された計算で、実行は外で行う |
| Action type | [actions/ の concept](/actions/review-high-return-products.md)。パラメータ・検証・副作用の契約 |
| Object view | `ochakai get` の一枚。`linked_from` が、そのテーブルや指標を指すすべての concept を運んでくる |
| Roles | trust tier。draft は書いたエージェントの名で残り、verified は人の裁定である |

## 型を増やさなかった理由

OKF の SPEC にもサンプルのバンドルにも、object type にあたる型は無い。
サンプルでは `BigQuery Table` の concept が業務上の「もの」の名前と
`# Schema`・`# Joins` を持ち、結合は concept 間のリンクで表されている。
このバンドルもそれに従った。関係の種類はリンクの周りの文が言う(SPEC §6.1)。

Foundry のオントロジーが持つ関係は「この二つはこう結べる」である。
データ分析で本当に要るのは、その先の「結ぶとこうなる、だからこう読む」
であり、それは Insight の形のほうがよく持てる。売上とその要因の関係は
[売上が落ちるときの因果](/insights/why-revenue-falls.md)が、明細と訪問の
関係は[セッションと明細のつなぎ方](/insights/sessions-and-order-items.md)
が、それぞれ文章で持っている。

## Foundry との違い

Foundry はオントロジーをプラットフォームの中で実行し、インスタンスも
持つ。ochakai は実行せず、インスタンスも持たない。持つのは意味と契約と
裁定だけで、個々の注文は BigQuery にある。実行するのは手元のエージェント
で、[計算の実行](/skills/run-bigquery-query.md)も
[アクションの実行](/skills/run-an-action.md)も、エージェントが receipt を
持って帰ってくる契約として書いてある。何をしてよいかは action の concept、
どうやるかは skill、やってよかったかは人の verify と `report_outcome` が
持つ。

[^foundry]: Palantir Foundry: Ontology core concepts
