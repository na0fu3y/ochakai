---
type: Attested Computation
title: 月次受注額
description: キャンセルを除く全明細の受注ベース合計(deprecated。FY2026 から売上は配達完了ベース)
tags: [sales, bookings, gmv, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-26T07:00:00Z }
status: deprecated
status_note: FY2026 の売上計上ポリシーで売上は配達完了ベースになった。FY2025 のレポートはこの数字で作られているので、読み直すために残してある。
runtime: bigquery
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
question: 受注ベースの月次合計は?
---

`Cancelled` 以外のすべての[明細](/tables/order-items.md)を、受注した月で
合計したものである。世間で GMV と呼ばれる形の数字で、FY2025 まで月次
レポートの一番上にあったのはこれだった。

# Computation

```sql
SELECT
  DATE_TRUNC(DATE(oi.created_at), MONTH) AS month,
  ROUND(SUM(oi.sale_price), 2)           AS bookings
FROM `bigquery-public-data.thelook_ecommerce.order_items` AS oi
WHERE oi.status != 'Cancelled'
GROUP BY month
ORDER BY month
```

# deprecated であって、削除ではない

FY2026 の[売上計上ポリシー](/policies/revenue-recognition.md)は売上を
配達完了ベースに決めたので、正は
[月次売上](/queries/sales/monthly-revenue.md)である。この計算は
`Returned` を含み、まだ届いていない `Processing` と `Shipped` も含むので、
[売上](/metrics/revenue.md)より常に大きい。

それでも消していないのは、FY2025 のレポートがこの数字で書かれているから
である。古い資料の「月商」を今の売上と突き合わせて合わないと言う前に、
これを一度走らせて、読んでいた数字がどちらだったのかを確かめること。
`attester` は付けていない。新しく引く数字ではないためである。
