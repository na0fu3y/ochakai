---
type: Attested Computation
title: 月次売上
description: 直近24か月の売上を暦月ごとに出す
tags: [sales, revenue, bigquery]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: 売上計上ポリシー (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-07-24T00:00:00Z"
usage_window: { from: "2026-07-01T00:00:00Z", to: "2026-07-31T00:00:00Z" }
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-26T06:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-30T02:30:00Z }
status: stable
stale_after: "2026-12-31T00:00:00Z"
runtime: bigquery
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: 月ごとの売上は?
---

[売上](/metrics/revenue.md)を月ごとに出す、承認された方法である。
公開データセットなので、どの Google Cloud プロジェクトからでも実際に
実行できる。

# Computation

```sql
SELECT
  DATE_TRUNC(DATE(oi.created_at), MONTH) AS month,
  ROUND(SUM(oi.sale_price), 2)           AS revenue,
  COUNT(DISTINCT oi.order_id)            AS orders
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
WHERE oi.status = 'Complete'
  AND oi.created_at >= TIMESTAMP(
        DATE_SUB(DATE_TRUNC(CURRENT_DATE(), MONTH), INTERVAL 24 MONTH))
GROUP BY month
ORDER BY month
```

# 注意

- `status = 'Complete'` は大文字の C で書くこと。小文字で書くと、
  エラーにならずに 0 行が返る([完了した注文](/glossary/completed-order.md))。
- 月は UTC の暦月で切る。`Asia/Tokyo` に直さないこと。[^rev-policy]
- **末尾の月を読まないこと。** 当月は系列の最高値として返ってくるが、
  それは生成器の山と未来の日付の行のせいである([売上の読み方](/insights/reading-revenue.md))。
- 同じ月でも、走らせる日によって数字が違う。データセットが履歴ごと毎日
  作り直されるからで([thelook_ecommerce](/datasets/thelook-ecommerce.md))、
  締めた数字が必要なら receipt を残す。

売上が動いた理由を知りたいときは、この計算ではなく
[月次売上の要因分解](/computations/revenue-drivers.md)を走らせる。

`stale_after` は年度末に置いてある。年に一度、この形のままでよいかを
見直すこと。

[^rev-policy]: 売上計上ポリシー (FY2026)
