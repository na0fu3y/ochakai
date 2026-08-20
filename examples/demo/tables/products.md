---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.products
title: products
description: 商品カタログ。定価と原価はここ、実売価格はここではない
tags: [products, catalog, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T04:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-29T01:05:00Z }
status: stable
---

商品カタログ。1行は商品(SKU)1つで、
[注文明細](/tables/order-items.md)が `product_id` で指してくる。
カテゴリ・ブランド・部門で売上を割るときの JOIN 先はここである。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `retail_price` | FLOAT64 | **定価**。売上をここから作らないこと — 実売は明細の `sale_price` が正である |
| `cost` | FLOAT64 | 原価。粗利をこの世界で出すなら `sale_price - cost` であって、`retail_price - cost` ではない |
| `category` | STRING | 26 前後の品目分類。売上を割る一段目はたいていこれ |
| `brand` | STRING | ブランド。カテゴリより粒が細かく、null がありうる |
| `department` | STRING | `Men` / `Women` の**2値だけ**。3つ目を期待した集計は静かに嘘をつく |
| `sku` | STRING | 倉庫側の識別子 |
| `distribution_center_id` | INT64 | 出荷元。`distribution_centers` はカタログ外([データセット](/references/thelook-dataset.md)) |

定価と実売を混ぜるのが、このテーブルまわりで一番よくある間違いで
ある。粗利にはまだ metric が無い — 書くなら
[売上](/metrics/revenue.md)と同じく明細側から書くこと。

返品率が高い商品を割り出してこのカタログの見直しにかけるのは、
[返品率の高い商品の見直し](/actions/review-high-return-products.md)の
仕事である。
