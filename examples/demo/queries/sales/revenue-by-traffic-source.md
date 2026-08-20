---
type: Attested Computation
title: 獲得チャネル別売上
description: 売上を顧客の獲得チャネルで割ったもの(draft — セッション側で割るべきかが決まっていない)
tags: [sales, revenue, marketing, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-27T09:45:00Z }
status: draft
stale_after: "2026-08-01"
runtime: bigquery
parameters:
  - { name: from_date, type: DATE, required: true }
  - { name: to_date, type: DATE, required: true }
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
question: 売上が伸びているのはどのチャネルか?
---

[月次売上](/queries/sales/monthly-revenue.md)と同じ
[売上](/metrics/revenue.md)を、[顧客](/tables/users.md)の
`traffic_source` で割ったもの。キャンペーンの振り返りのときに
エージェントが書き、draft のまま置いてある。

# Computation

```sql
SELECT
  DATE_TRUNC(DATE(oi.created_at), MONTH) AS month,
  u.traffic_source,
  ROUND(SUM(oi.sale_price), 2)           AS revenue
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
JOIN `bigquery-public-data.thelook_ecommerce.users` AS u
  ON u.id = oi.user_id
WHERE oi.status = 'Complete'
  AND oi.created_at >= TIMESTAMP(@from_date)
  AND oi.created_at <  TIMESTAMP(@to_date)
GROUP BY month, u.traffic_source
ORDER BY month, revenue DESC
```

# 検証されていない理由

- これが割っているのは**獲得時**のチャネルである。5年前に Search で
  獲得された客の今日の注文も Search に積まれる。「今月の売上はどの
  チャネルから来たか」という問いなら、割るべきは `events` 側の
  セッションであって、この列ではない — 同名の列が二つあって別物で
  ある件は[顧客](/tables/users.md)にある。どちらの問いに答えたいのか、
  誰も決めていない。
- `events` 側で割るには注文とセッションの紐付けを決める必要があり、
  その紐付け自体がもう一つの決めごとである。
- `stale_after` はキャンペーン振り返りの締切に置いたので、もう過ぎて
  いる。つまりこの concept は stale のフィードに出てくる。それが狙い
  である — 誰も戻ってきていないことが、フィードでわかる。
- `attester` が無い。`executor` はあるので
  [実行はできる](/skills/run-bigquery-query.md)が、その実行が*この*
  SQL を使ったことを確かめるものが何も無い。ここから引いた数字は、
  信用で受け取る数字である。
  [月次売上](/queries/sales/monthly-revenue.md)には attester がある。
  二つの concept の違いで、いちばん効くのはそこである。
