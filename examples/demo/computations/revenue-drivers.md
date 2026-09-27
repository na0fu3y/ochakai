---
type: Attested Computation
title: 月次売上の要因分解
description: 月ごとの売上を受注点数・完了率・明細単価に分け、前月からの変化をそれぞれの寄与として出す。変化が揺れの幅を超えたかを z で添える。売上が動いた理由を調べる最初の一手
tags: [sales, revenue, diagnosis, variation, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T03:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-26T02:20:00Z }
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
question: 売上が動いたのは、件数か、完了率か、単価か? それは揺れの範囲か?
---

[売上](/metrics/revenue.md)は[受注点数](/metrics/booked-items.md) ×
[完了率](/metrics/completion-rate.md) × [明細単価](/metrics/average-item-price.md)
にちょうど分かれる。この計算は三つを月ごとに出し、前月からの変化を
対数で返す。対数なので `d_revenue` は残りの三つの `d_` の和に一致し、
どの要因が何ポイント効いたかをそのまま足し引きで読める。

**変化ごとに `z_` も返す。** 同じ店が同じように売っていても、注文は一件
ずつ偶然に入るので、月の値はそれだけで揺れる。`z_` は、その月の変化が
この揺れの何倍かである。揺れの大きさは、その月の注文どうしのばらつき
から出している。**`|z| < 2` の変化には理由を探さない**
([メトリクスが沈んだときの調べ方](/skills/diagnose-a-metric.md))。

完了しなかった明細がどこに流れたかも、`returned_share`(返品)・
`cancelled_share`(キャンセル)・`open_share`(未完了)として一緒に出す。

# Computation

```sql
WITH orders AS (
  -- One row per order and month. Status is shared by every item of an
  -- order, so the order, not the item, is what varies at random.
  SELECT
    DATE_TRUNC(DATE(oi.created_at), MONTH)            AS month,
    COUNT(*)                                          AS items,
    COUNTIF(oi.status = 'Complete')                   AS completed,
    SUM(IF(oi.status = 'Complete', oi.sale_price, 0)) AS revenue,
    COUNTIF(oi.status = 'Returned')                   AS returned,
    COUNTIF(oi.status = 'Cancelled')                  AS cancelled,
    COUNTIF(oi.status IN ('Processing', 'Shipped'))   AS open
  FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
  WHERE oi.created_at >= TIMESTAMP(DATE_SUB(@from_month, INTERVAL 1 MONTH))
    AND oi.created_at <  TIMESTAMP(@to_month)
  GROUP BY oi.order_id, month
), monthly AS (
  SELECT
    month,
    EXTRACT(DAY FROM LAST_DAY(month))       AS days,
    SUM(revenue)                            AS revenue,
    SUM(items)                              AS booked_items,
    SUM(completed) / SUM(items)             AS completion_rate,
    SAFE_DIVIDE(SUM(revenue), SUM(completed)) AS avg_item_price,
    SUM(returned) / SUM(items)              AS returned_share,
    SUM(cancelled) / SUM(items)             AS cancelled_share,
    SUM(open) / SUM(items)                  AS open_share
  FROM orders
  GROUP BY month
), noise AS (
  -- Squared relative standard error of each monthly value, from how much
  -- its orders differ from one another (ratio estimators, orders as the
  -- sampling unit).
  SELECT
    o.month,
    SUM(POW(o.revenue, 2)) / POW(m.revenue, 2)       AS v_revenue,
    SUM(POW(o.items, 2)) / POW(m.booked_items, 2)    AS v_booked_items,
    SUM(POW(o.completed - m.completion_rate * o.items, 2))
      / POW(m.completion_rate * m.booked_items, 2)   AS v_completion_rate,
    SUM(POW(o.revenue - m.avg_item_price * o.completed, 2))
      / POW(m.revenue, 2)                            AS v_avg_item_price
  FROM orders AS o
  JOIN monthly AS m USING (month)
  GROUP BY o.month, m.revenue, m.booked_items, m.completion_rate, m.avg_item_price
), changes AS (
  SELECT
    month,
    days,
    ROUND(revenue, 2)                                         AS revenue,
    booked_items,
    ROUND(completion_rate, 3)                                 AS completion_rate,
    ROUND(avg_item_price, 2)                                  AS avg_item_price,
    ROUND(returned_share, 3)                                  AS returned_share,
    ROUND(cancelled_share, 3)                                 AS cancelled_share,
    ROUND(open_share, 3)                                      AS open_share,
    ROUND(LN(days / LAG(days) OVER w), 3)                     AS d_days,
    ROUND(LN(revenue / LAG(revenue) OVER w), 3)               AS d_revenue,
    ROUND(LN(booked_items / LAG(booked_items) OVER w), 3)     AS d_booked_items,
    ROUND(LN(completion_rate / LAG(completion_rate) OVER w), 3) AS d_completion_rate,
    ROUND(LN(avg_item_price / LAG(avg_item_price) OVER w), 3) AS d_avg_item_price,
    ROUND(LN(revenue / LAG(revenue) OVER w)
      / SQRT(v_revenue + LAG(v_revenue) OVER w), 1)           AS z_revenue,
    ROUND(LN(booked_items / LAG(booked_items) OVER w)
      / SQRT(v_booked_items + LAG(v_booked_items) OVER w), 1) AS z_booked_items,
    ROUND(LN(completion_rate / LAG(completion_rate) OVER w)
      / SQRT(v_completion_rate + LAG(v_completion_rate) OVER w), 1) AS z_completion_rate,
    ROUND(LN(avg_item_price / LAG(avg_item_price) OVER w)
      / SQRT(v_avg_item_price + LAG(v_avg_item_price) OVER w), 1)   AS z_avg_item_price
  FROM monthly
  JOIN noise USING (month)
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
- **先に `z_revenue` を見る。** `|z_revenue| < 2` なら、売上の変化は
  揺れの範囲で、要因を掘っても偶然に名前を付けることになる。このデータ
  では売上の月次の揺れが大きく、前月比で 1〜2 割動いても z は 2 に届か
  ないことが多い。
- `z_` が 2 を超えた要因だけを掘る。ただし、比べる月が並ぶほど偶然
  超える月も出る(40 か月を並べれば 2 つ前後)。一つだけ超えたら、
  **前月のほうが外れていないか**を、もう一つ前の月と比べて確かめる。
  外れていたのが前月なら、当月は元に戻っただけである。
- **`d_days` を引いて読む。** 受注点数は日数に比例する。2 月から 3 月へは
  `d_days` が +0.10 で、受注点数はそれだけで 1 割伸びる。
- `d_` は対数なので、-0.10 はおよそ 1 割減である。
- 当月は含めないこと。未来の日付の行と生成器の山で、三つとも歪む。
- `z_` はこの計算で出る揺れの目安である。どの注文も互いに無関係に
  入る、という仮定の上にある。
