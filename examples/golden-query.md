---
type: Attested Computation
title: 月次の計上売上
description: 完了した注文の月次売上(例)
status: draft
runtime: bigquery
question: 今年度の月次売上は?
tags: [sales, revenue]
---

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  SUM(o.total_price)              AS revenue
FROM `myproject.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= DATE_TRUNC(CURRENT_DATE(), YEAR)
GROUP BY month
ORDER BY month
```

# 注意

- **完了した注文だけ。** `status = 'completed'` 以外 — キャンセル、返品
  待ちで保留のもの — は除いている。当日の速報値が要るなら、別のクエリを
  使うこと。
- 金額は円、税込。返金は引いていないので、返品率の高い月は締めた数字より
  高く出る。
- 月の境目は注文の作成時刻(`created_at`、UTC)であり、経理の締めとは
  数時間ずれることがある。
