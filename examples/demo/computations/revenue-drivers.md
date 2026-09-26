---
type: Attested Computation
title: 月次売上の要因分解
description: 月ごとの売上を受注点数・完了率・明細単価に分け、前月からの変化をそれぞれの寄与として出す。売上が動いた理由を調べる最初の一手
tags: [sales, revenue, diagnosis, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T03:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-20T01:20:00Z }
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
question: 売上が動いたのは、件数か、完了率か、単価か?
---

[売上](/metrics/revenue.md)は[受注点数](/metrics/booked-items.md) ×
[完了率](/metrics/completion-rate.md) × [明細単価](/metrics/average-item-price.md)
にちょうど分かれる。この計算は三つを月ごとに出し、前月からの変化を
対数で返す。対数なので `d_revenue` は残りの三つの `d_` の和に一致し、
どの要因が何ポイント効いたかをそのまま足し引きで読める。

完了しなかった明細がどこに流れたかも、`returned_share`(返品)・
`cancelled_share`(キャンセル)・`open_share`(未完了)として一緒に出す。

# Computation

```sql
WITH monthly AS (
  SELECT
    DATE_TRUNC(DATE(oi.created_at), MONTH)            AS month,
    COUNT(*)                                          AS booked_items,
    COUNTIF(oi.status = 'Complete')                   AS completed_items,
    SUM(IF(oi.status = 'Complete', oi.sale_price, 0)) AS revenue,
    COUNTIF(oi.status = 'Returned')                   AS returned_items,
    COUNTIF(oi.status = 'Cancelled')                  AS cancelled_items,
    COUNTIF(oi.status IN ('Processing', 'Shipped'))   AS open_items
  FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
  WHERE oi.created_at >= TIMESTAMP(DATE_SUB(@from_month, INTERVAL 1 MONTH))
    AND oi.created_at <  TIMESTAMP(@to_month)
  GROUP BY month
), ratios AS (
  SELECT
    month,
    revenue,
    booked_items,
    completed_items / booked_items AS completion_rate,
    revenue / completed_items      AS avg_item_price,
    returned_items / booked_items  AS returned_share,
    cancelled_items / booked_items AS cancelled_share,
    open_items / booked_items      AS open_share
  FROM monthly
), changes AS (
  SELECT
    month,
    ROUND(revenue, 2)                                           AS revenue,
    booked_items,
    ROUND(completion_rate, 3)                                   AS completion_rate,
    ROUND(avg_item_price, 2)                                    AS avg_item_price,
    ROUND(returned_share, 3)                                    AS returned_share,
    ROUND(cancelled_share, 3)                                   AS cancelled_share,
    ROUND(open_share, 3)                                        AS open_share,
    ROUND(LN(revenue / LAG(revenue) OVER w), 3)                 AS d_revenue,
    ROUND(LN(booked_items / LAG(booked_items) OVER w), 3)       AS d_booked_items,
    ROUND(LN(completion_rate / LAG(completion_rate) OVER w), 3) AS d_completion_rate,
    ROUND(LN(avg_item_price / LAG(avg_item_price) OVER w), 3)   AS d_avg_item_price
  FROM ratios
  WINDOW w AS (ORDER BY month)
)
SELECT *
FROM changes
WHERE month >= @from_month
ORDER BY month
```

# 読み方

- `from_month` と `to_month` は月初の日付で、`to_month` の月は含まない。
  前月との比較のために、計算は `from_month` の前月も読む。
- `d_revenue` が負の月について、三つの `d_` のどれが一番負かを見る。
  次に見る先は要因ごとに決まっている([売上が落ちるときの因果](/insights/why-revenue-falls.md))。
- `d_` は対数なので、-0.10 はおよそ 1 割減である。
- 当月は含めないこと。未来の日付の行と生成器の山で、三つとも歪む。
