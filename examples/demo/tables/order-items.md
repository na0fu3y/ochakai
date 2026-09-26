---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.order_items
title: order_items
description: 商品1点につき1行の注文明細。このバンドルの金額・状態はすべてここから出る
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:25:00Z }
status: stable
---

注文明細である。[売上](/metrics/revenue.md)も[返品率](/metrics/return-rate.md)
も[粗利](/metrics/gross-margin.md)も、このバンドルで金額と呼ぶものは
すべてこのテーブルの `sale_price` から出る。[orders](/tables/orders.md)
には金額の列が無い。

**1行は商品1点で、数量の列は無い。** 同じ商品を2点買った注文は明細が
2行になり、行数がそのまま個数になる。`SUM(quantity * price)` のような式は
書けない。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `id` | INT64 | 主キー |
| `order_id` | INT64 | [orders](/tables/orders.md) の `order_id` へ |
| `user_id` | INT64 | [users](/tables/users.md) へ。注文の客と常に同じ |
| `product_id` | INT64 | [products](/tables/products.md) へ |
| `inventory_item_id` | INT64 | 在庫の個体を指すように見えるが、**売れた個体を指していない**(下の Joins) |
| `status` | STRING | 5 値、大文字始まり。意味は[完了した注文](/glossary/completed-order.md) |
| `sale_price` | FLOAT64 | 実売価格、USD。税・送料・値引きはこのデータのモデルに無い |
| `created_at` | TIMESTAMP | UTC。売上はこの列で月に束ねる |
| `shipped_at` / `delivered_at` / `returned_at` | TIMESTAMP | 状態に応じて入る。出荷日時が受注日時より前の行が 3 割近くある |

# Joins

- [orders](/tables/orders.md) — `oi.order_id = o.order_id`。主キーの名前が
  両側で違う(明細は `id`、注文は `order_id`)。注文の列は明細の数だけ
  複製されるので、件数は `COUNT(DISTINCT oi.order_id)` で数える。
- [products](/tables/products.md) — `p.id = oi.product_id`。カテゴリで
  割るときの JOIN。明細は必ず商品を指し、JOIN で行は減らない。
- [users](/tables/users.md) — `u.id = oi.user_id`。注文を経由しなくてよい。
- [inventory_items](/tables/inventory-items.md) — `inventory_item_id` では
  **ない**。`ii.product_id = oi.product_id AND ii.sold_at = oi.created_at`
  で結ぶ。理由は[inventory_item_id は売れた在庫を指さない](/insights/inventory-item-id.md)。
- [events](/tables/events.md) — キーの列が無い。購入セッションとは客・
  商品・時刻で 1 対 1 に結べる。[セッションと明細のつなぎ方](/insights/sessions-and-order-items.md)。

# Metrics

- [売上](/metrics/revenue.md)と、それを分解する[受注点数](/metrics/booked-items.md)・
  [完了率](/metrics/completion-rate.md)・[明細単価](/metrics/average-item-price.md)
- [返品率](/metrics/return-rate.md)、[粗利](/metrics/gross-margin.md)、
  [リピート購入率](/metrics/repeat-purchase-rate.md)

このテーブルに対する検証済みのクエリは、[月次売上](/computations/monthly-revenue.md)
と[月次売上の要因分解](/computations/revenue-drivers.md)である。
