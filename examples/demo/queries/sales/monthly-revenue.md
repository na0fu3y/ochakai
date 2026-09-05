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

[売上](/metrics/revenue.md)を月ごとに出す、承認された方法である。上の
`question` にそのまま答える。公開データセットなので、どの Google Cloud
プロジェクトからでも実際に実行できる。

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

- `status = 'Complete'` は[完了した注文](/glossary/completed-order.md)の
  定義そのものである。大文字の C で書くこと。小文字で書くと 0 行が返る。
- 月は UTC の暦月で切る。[売上計上ポリシー](/policies/revenue-recognition.md)
  の規則 3 で、`Asia/Tokyo` に直さないこと。[^rev-policy]
- 同じ月でも、走らせる日によって数字が違う。返品が過去の月から売上を
  抜いていくためで、この計算の欠陥ではなく
  [データセット](/references/thelook-dataset.md)の性質である。締めた数字
  が必要なら、締めの日の receipt を残す。
- **末尾の月を読まないこと。** 未完なだけなら低く出るはずだが、この
  データセットでは生成器が直近数日に山を作り、`created_at` が今日より先の
  行まで入っているので、当月が系列の最高値として返ってくる。出力の最後の
  行はこの計算の結果であって、店の状態ではない
  ([売上の読み方](/insights/reading-revenue.md))。

# 実行のしかた

`executor.resource` が指すのは
[BigQuery の計算を実行する](/skills/run-bigquery-query.md)で、実行は宣言
した `receipt` の三つ、`job_id`・`executed_sql`・`row_count` を持って
帰る必要がある。`attester` はその実行が有効かどうかを決めるコードで、
`executed_sql` を上のフェンスと突き合わせ、書き換えられていれば拒否する。
そこで初めて、この数字は「承認された」だけでなく「証明された」ものに
なる。どちらも ochakai の仕事ではない。ochakai は契約を保存するだけで、
実行はしない。

`stale_after` は年度末に置いてある。データセットは更新され続けるので、
年に一度、この形のままでよいかを見直すこと。

[^rev-policy]: 売上計上ポリシー (FY2026)
