---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.order_items
title: order_items
description: 商品1点につき1行の注文明細。このバンドルの金額はすべてここから出る
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:25:00Z }
status: stable
---

注文明細である。[売上](/metrics/revenue.md)も
[返品率](/metrics/return-rate.md)も、このバンドルで金額と呼ぶものは
すべてこのテーブルの `sale_price` から出る。
[注文ヘッダ](/tables/orders.md)には金額の列が無い。

**1行は商品1点で、数量の列は無い。** 同じ商品を2点買った注文は明細が
2行になる。1行が在庫の個体1つ(`inventory_item_id`)に対応しているので、
行数がそのまま個数になる。`SUM(quantity * price)` のような式は書けない。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `sale_price` | FLOAT64 | 実売価格、USD。税と送料はこのデータセットのモデルに存在しない |
| `status` | STRING | 明細ごとの状態。注文の status とは別に持つ([完了した注文](/glossary/completed-order.md)) |
| `order_id` | INT64 | [注文ヘッダ](/tables/orders.md)へ |
| `product_id` | INT64 | [商品](/tables/products.md)へ。定価・原価・カテゴリはあちら |
| `user_id` | INT64 | [顧客](/tables/users.md)へ。ヘッダを経由せずに客へ辿れる |
| `inventory_item_id` | INT64 | 在庫の個体。数量列が無いのはこのためである |
| `created_at` | TIMESTAMP | UTC。売上はこの列で束ねる |
| `returned_at` | TIMESTAMP | 返品されていなければ null |

## status を明細ごとに持つ

そのため、3点のうち1点だけ返った一部返品も、明細1行が `Returned` に
なるだけで正しく数えられる。常に明細側で集計するという規則は、ここから
来ている。注文側だけを読む集計は、その1行を見落とす。

このテーブルに対する検証済みのクエリは
[月次売上](/queries/sales/monthly-revenue.md)である。
