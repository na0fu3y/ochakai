---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.order_items
title: order_items
description: 商品1点につき1行の注文明細。このバンドルの金はすべてここから出る
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:25:00Z }
status: stable
---

注文明細。[売上](/metrics/revenue.md)も[返品率](/metrics/return-rate.md)も、
このバンドルで金額と呼ばれるものはすべてこのテーブルの `sale_price` から
出る。[注文ヘッダ](/tables/orders.md)には金額の列が無い。

**1行は商品1点である。数量の列は無い。** 同じ商品を2点買った注文は
明細が2行になる。行がそのまま個数なので、`SUM(quantity * price)` を
書きたくなったら、このテーブルには quantity が無いことを思い出すこと。
1行が在庫の個体1つ(`inventory_item_id`)に対応しているからこうなって
いる。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `sale_price` | FLOAT64 | 実売価格、**USD**。税と送料はこのデータセットのモデルに存在しない |
| `status` | STRING | **明細ごと**の状態。注文の status とは別に持つ([完了した注文](/glossary/completed-order.md)) |
| `order_id` | INT64 | [注文ヘッダ](/tables/orders.md)へ |
| `product_id` | INT64 | [商品](/tables/products.md)へ。定価・原価・カテゴリはあちら |
| `user_id` | INT64 | [顧客](/tables/users.md)へ。ヘッダを経由せずに客へ飛べる |
| `inventory_item_id` | INT64 | 在庫の個体。数量列が無い理由がこれである |
| `created_at` | TIMESTAMP | UTC。売上はこの列で束ねる |
| `returned_at` | TIMESTAMP | 返品されていなければ null |

status が明細ごとにあるので、**一部返品は特別扱いなしで正しく数え
られる** — 3点のうち1点が返った注文は、明細1行だけが `Returned` に
なる。集計を常に明細側で行うというバンドルの規則は、ここから来ている。

このテーブルに対する検証済みのクエリは
[月次売上](/queries/sales/monthly-revenue.md)である。
