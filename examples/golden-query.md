---
type: Golden Query
title: 月次の確定売上
description: 確定済み注文の売上を月次で集計する(例)
status: draft
question: 今年の月次売上は?
sql: |
  SELECT
    DATE_TRUNC(o.created_at, MONTH) AS month,
    SUM(o.total_price)              AS revenue
  FROM `myproject.shop.orders` AS o
  WHERE o.status = 'completed'
    AND o.created_at >= DATE_TRUNC(CURRENT_DATE(), YEAR)
  GROUP BY month
  ORDER BY month
tags: [sales, revenue]
---

# 注意点

- **確定売上のみ。** `status = 'completed'` 以外(キャンセル・返品保留)は
  含めない。速報値が欲しい場合は別のクエリを使うこと。
- 金額は税込 JPY。返品は控除していないので、返品率の高い月は上振れする。
- 月境界は注文作成時刻(`created_at`、UTC)基準。会計側の締めとは
  数時間ずれることがある。
