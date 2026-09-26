---
type: Attested Computation
title: 流入元別の受注
description: 受注点数と売上を、その明細を生んだ訪問の流入元で割ったもの(draft。セッションと明細のつなぎ方が裁定されていない)
tags: [sales, demand, marketing, attribution, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-27T09:45:00Z }
status: draft
stale_after: "2026-08-31T00:00:00Z"
runtime: bigquery
parameters:
  - { name: from_month, type: DATE, required: true }
  - { name: to_month, type: DATE, required: true }
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
question: 受注はどの流入元の訪問から来たか?
---

[受注点数](/metrics/booked-items.md)が落ちたとき、つまり需要が落ちた
ときに、どの経路の訪問が減ったのかを見る計算である。明細と訪問には
キーの列が無いので、[セッションと明細のつなぎ方](/insights/sessions-and-order-items.md)
の導出をそのまま使っている。

流入元は訪問ごとの `events.traffic_source` で、客の獲得チャネル
(`users.traffic_source`)ではない。二つは語彙から違う([users](/tables/users.md))。

# Computation

```sql
WITH purchase_sessions AS (
  SELECT
    e.session_id,
    ANY_VALUE(e.user_id)        AS user_id,
    ANY_VALUE(e.traffic_source) AS traffic_source,
    SAFE_CAST(ANY_VALUE(IF(e.event_type = 'product',
      REGEXP_EXTRACT(e.uri, r'^/product/(\d+)$'), NULL)) AS INT64) AS product_id,
    MAX(IF(e.event_type = 'purchase', e.created_at, NULL))           AS purchased_at
  FROM `bigquery-public-data.thelook_ecommerce.events` AS e
  WHERE e.created_at >= TIMESTAMP_SUB(TIMESTAMP(@from_month), INTERVAL 5 DAY)
    AND e.created_at <  TIMESTAMP_ADD(TIMESTAMP(@to_month), INTERVAL 1 DAY)
  GROUP BY e.session_id
  HAVING COUNTIF(e.event_type = 'purchase') = 1
)
SELECT
  DATE_TRUNC(DATE(oi.created_at), MONTH)                      AS month,
  s.traffic_source,
  COUNT(*)                                                    AS booked_items,
  ROUND(SUM(IF(oi.status = 'Complete', oi.sale_price, 0)), 2) AS revenue
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
LEFT JOIN purchase_sessions AS s
  ON  s.user_id    = oi.user_id
  AND s.product_id = oi.product_id
  AND ABS(TIMESTAMP_DIFF(oi.created_at, s.purchased_at, SECOND)) <= 600
WHERE oi.created_at >= TIMESTAMP(@from_month)
  AND oi.created_at <  TIMESTAMP(@to_month)
GROUP BY month, s.traffic_source
ORDER BY month, booked_items DESC
```

`LEFT JOIN` なので、訪問につながらなかった明細は `traffic_source` が
null の行として残る。その行が出たら、つなぎ方の前提が崩れている。

# 検証されていない理由

- **つなぎ方が draft である。** 10 分という許容幅を誰も裁定していない。
  それが verify されるまで、この計算も verify しない。
- **`attester` が無い。** `executor` はあるので実行はできるが、その実行が
  *この* SQL を使ったことを確かめるものが無い。ここから引いた数字は、
  信用で受け取る数字である。
- **流入元が分かっても、理由までは分からない。** 広告費やキャンペーンの
  記録が無いので、ある経路の訪問が減った理由をこのデータの中で探すことは
  できない([不足しているデータ](/insights/missing-data.md))。
- `stale_after` はキャンペーン振り返りの締切に置いたので、もう過ぎている。
  誰も戻ってきていないことが stale のフィードに出る。
- `events` を読むので、ほかの売上の計算より一桁以上多くスキャンする。
