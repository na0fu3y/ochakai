---
type: Attested Computation
title: カテゴリ別粗利
description: 期間を指定して、商品カテゴリごとの売上・粗利・粗利率を出す
tags: [sales, margin, products, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-22T04:30:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-26T01:35:00Z }
status: stable
stale_after: "2026-12-31T00:00:00Z"
runtime: bigquery
parameters:
  - { name: from_date, type: DATE, required: true }
  - { name: to_date, type: DATE, required: true }
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: カテゴリごとの粗利は?
---

[粗利](/metrics/gross-margin.md)を[商品](/tables/products.md)のカテゴリで
割って出す、承認された方法である。

# Computation

```sql
SELECT
  p.category,
  COUNT(*)                                                   AS items,
  ROUND(SUM(oi.sale_price), 2)                               AS revenue,
  ROUND(SUM(oi.sale_price - p.cost), 2)                      AS gross_profit,
  ROUND(SUM(oi.sale_price - p.cost) / SUM(oi.sale_price), 3) AS gross_margin
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
JOIN `bigquery-public-data.thelook_ecommerce.products` AS p
  ON p.id = oi.product_id
WHERE oi.status = 'Complete'
  AND oi.created_at >= TIMESTAMP(@from_date)
  AND oi.created_at <  TIMESTAMP(@to_date)
GROUP BY p.category
ORDER BY gross_profit DESC
```

# 注意

- 期間は `[from_date, to_date)` で、`to_date` の日は含まない。UTC で切る。
- 母数の小さいカテゴリがある(`Clothing Sets` や `Jumpsuits & Rompers`
  は月に数点しか売れない月がある)。そういう行の粗利率は荒いので、`items`
  を一緒に読むこと。
- 当月を含めないこと([売上の読み方](/insights/reading-revenue.md))。
