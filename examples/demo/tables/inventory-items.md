---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.inventory_items
title: inventory_items
description: 在庫の現物1点につき1行。売れた時刻は sold_at にあり、60 日で売れなかった個体はもう売れない
tags: [inventory, products, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-11T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-13T01:00:00Z }
status: stable
---

在庫の現物、つまり倉庫に入った商品1点につき1行である。売れた行と
売れ残った行が同じテーブルに入っていて、売れ残りのほうが多い。

**売れた個体は、どれも入荷から 60 日以内に売れている。** 60 日を過ぎて
残っている個体は、この生成器ではこの先も売れない。在庫の大半はそういう
個体で、[滞留在庫の処分提案](/actions/propose-aged-inventory-clearance.md)
はこの性質の上に立っている。現実の倉庫の回転として読んではいけない。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `id` | INT64 | 主キー |
| `product_id` | INT64 | [products](/tables/products.md) へ |
| `created_at` | TIMESTAMP | 入荷。UTC |
| `sold_at` | TIMESTAMP | 注文が入った時刻。売れていなければ null。**注文がキャンセルされても返品されても null に戻らない** |
| `cost` | FLOAT64 | 原価。商品の `cost` と同じ |
| `product_category` ほか `product_` 付きの 7 列 | 各種 | 商品の属性の写し。元と食い違わない |

`sold_at` は「注文が入った」という意味で、「売上になった」という意味では
ない。`Cancelled` や `Returned` の明細にも売れた個体が 1 つずつ対応して
いるので、売れた個体の数を[売上](/metrics/revenue.md)と突き合わせると
合わない。

# Joins

- [order_items](/tables/order-items.md) — `ii.product_id = oi.product_id
  AND ii.sold_at = oi.created_at`。明細の `inventory_item_id` では結べない。
  [inventory_item_id は売れた在庫を指さない](/insights/inventory-item-id.md)。
- [products](/tables/products.md) — `p.id = ii.product_id`。属性を集計する
  だけなら、写しの列があるので JOIN は要らない。
