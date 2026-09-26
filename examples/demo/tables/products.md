---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.products
title: products
description: 商品カタログ。価格と原価は商品ごとに固定で、明細単価が動くのは売れた商品の構成が変わったときだけ
tags: [products, catalog, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T04:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-29T01:05:00Z }
status: stable
---

商品カタログである。1行は商品(SKU)1つで、[order_items](/tables/order-items.md)
が `product_id` で指してくる。カテゴリ・ブランド・部門で売上を割るときの
JOIN 先はここである。

**価格は商品ごとに一つしかない。** 明細の `sale_price` は、今のデータでは
全行で指す先の商品の `retail_price` と一致する。値引きも値上げも起きない
ので、[明細単価](/metrics/average-item-price.md)が動いたら、それは価格では
なく、どの商品が売れたかが変わったということである。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `id` | INT64 | 主キー。`sku` も一意だが、ほかのテーブルが指すのは `id` |
| `retail_price` | FLOAT64 | 定価。売上はここから作らず、明細の `sale_price` から作る。今は同じ値だが、取引の時点の値は明細のほう |
| `cost` | FLOAT64 | 原価。常に定価より小さい。[粗利](/metrics/gross-margin.md)の引く側。履歴は無い |
| `category` | STRING | 26 値。売上を割る一段目。カテゴリごとに単価の水準がはっきり違う |
| `brand` / `name` | STRING | どちらも null がありうる |
| `department` | STRING | `Men` / `Women` の2値だけ。**買った客の性別と完全に一致する**(女性客は `Women` の商品しか買わない) |
| `distribution_center_id` | INT64 | 出荷元の拠点 |

`department` と客の性別が一致しているので、「男性が女性向け商品を買う
割合」のような問いにこのデータは答えられない。答えは常に 0 になる。

# Joins

- [order_items](/tables/order-items.md) — `p.id = oi.product_id`。カタログに
  あって一度も売れていない商品が少数あるので、商品を起点に集計するなら
  `LEFT JOIN`。
- [inventory_items](/tables/inventory-items.md) — `ii.product_id = p.id`。
  在庫の行は商品の属性を `product_` 付きの列に写して持っていて、写しは
  元と食い違わない。
- [distribution_centers](/tables/distribution-centers.md) —
  `dc.id = p.distribution_center_id`。商品がいつもどこから出るかは、この
  列だけで決まる。

# Metrics

- [明細単価](/metrics/average-item-price.md)が動いた理由は、
  [カテゴリ別売上](/computations/revenue-by-category.md)で構成を見る。
- [粗利](/metrics/gross-margin.md)と[カテゴリ別粗利](/computations/gross-margin-by-category.md)。
- 商品単位のアクションが二つある。[返品率の高い商品の見直し](/actions/review-high-return-products.md)
  と[滞留在庫の処分提案](/actions/propose-aged-inventory-clearance.md)。
