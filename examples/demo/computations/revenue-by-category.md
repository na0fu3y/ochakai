---
type: Attested Computation
title: カテゴリ別売上
description: 月ごと・カテゴリごとの完了明細数、売上、平均単価、点数の比率。明細単価が動いた理由(構成の変化)を見る
tags: [sales, revenue, products, diagnosis, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T03:30:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-20T01:25:00Z }
status: stable
runtime: bigquery
parameters:
  - { name: from_month, type: DATE, required: true }
  - { name: to_month, type: DATE, required: true }
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: どのカテゴリが売上を動かしたか?
---

[明細単価](/metrics/average-item-price.md)が動いたときに走らせる。価格は
商品ごとに固定なので、単価の変化はすべて構成の変化であり、この計算は
その構成をカテゴリの単位で出す。

# Computation

```sql
SELECT
  DATE_TRUNC(DATE(oi.created_at), MONTH)                   AS month,
  p.category,
  COUNT(*)                                                 AS completed_items,
  ROUND(SUM(oi.sale_price), 2)                             AS revenue,
  ROUND(AVG(oi.sale_price), 2)                             AS avg_item_price,
  ROUND(COUNT(*) / SUM(COUNT(*)) OVER (PARTITION BY
    DATE_TRUNC(DATE(ANY_VALUE(oi.created_at)), MONTH)), 3) AS item_share
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
JOIN `bigquery-public-data.thelook_ecommerce.products` AS p
  ON p.id = oi.product_id
WHERE oi.status = 'Complete'
  AND oi.created_at >= TIMESTAMP(@from_month)
  AND oi.created_at <  TIMESTAMP(@to_month)
GROUP BY month, p.category
ORDER BY month, revenue DESC
```

# 読み方

比べたい 2 か月を `[from_month, to_month)` に入れ、カテゴリごとに
`revenue` の差を並べる。見るのは二つである。

- **比率が動いたか。** 単価の高いカテゴリ(Outerwear & Coats、Suits &
  Sport Coats、Jeans など)の `item_share` が下がり、安いカテゴリの比率が
  上がっていれば、明細単価はそれで落ちている。
- **カテゴリの中の単価が動いたか。** `avg_item_price` がカテゴリ内で大きく
  動くことがある。同じ Dresses でも、どの商品が売れたかで平均は倍半分に
  なる。母数が数十件のカテゴリでは、特に荒い。

ここで分かるのは「何が」売れたかまでである。「なぜ」安い方に寄ったのか
(値引き、露出、在庫切れ)の記録はこのデータに無い
([不足しているデータ](/insights/missing-data.md))。

当月を含めないこと([売上の読み方](/insights/reading-revenue.md))。
