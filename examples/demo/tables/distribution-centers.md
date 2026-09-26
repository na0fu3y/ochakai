---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.distribution_centers
title: distribution_centers
description: 物流拠点の10行だけのテーブル。拠点は商品が決め、配送日数は拠点によらない
tags: [logistics, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-11T02:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-13T01:05:00Z }
status: stable
---

物流拠点のテーブルで、行は 10 しかなく、すべて米国内にある。客は 15 か国
にいても、出荷元はすべて米国である。

# Schema

| 列 | 型 | 注記 |
|---|---|---|
| `id` | INT64 | 主キー、1〜10 |
| `name` | STRING | `Memphis TN` のような都市名と州 |
| `latitude` / `longitude` | FLOAT64 | 座標 |
| `distribution_center_geom` | GEOGRAPHY | 同じ座標の点。`ST_DISTANCE` にそのまま渡せる |

注文にも明細にも拠点の列は無い。どの拠点から出るかは商品が決める。
出荷から配達までの日数はどの拠点でも 0〜5 日に均等に散っていて、距離とは
関係しない。「近い拠点から出せば早く届く」という前提の分析は、このデータ
では成り立たない。

# Joins

- [products](/tables/products.md) — `dc.id = p.distribution_center_id`。
- [inventory_items](/tables/inventory-items.md) の
  `product_distribution_center_id` はこの値の写しで、拠点ごとに在庫を
  数えるだけならここまで JOIN しなくてよい。
