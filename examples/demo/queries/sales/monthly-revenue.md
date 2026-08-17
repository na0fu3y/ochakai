---
type: Attested Computation
title: 月次売上
description: 今年度の売上を暦月ごとに出す
tags: [sales, revenue, bigquery]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: 売上計上ポリシー (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-04-01"
usage_window: { from: "2026-06-01", to: "2026-06-30" }
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-18T06:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:30:00Z }
status: stable
stale_after: "2026-12-31"
runtime: bigquery
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
attester:
  resource: https://git.example.co.jp/analytics/attesters/sql_equality.py
question: 今年度の月次売上は?
---

[売上](/metrics/revenue.md)を月ごとに出す、承認された方法。上の
`question` にそのまま答える。ダッシュボードから月次売上を読む人は、これを
再現できるはずである。

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  SUM(o.total_price)              AS revenue,
  COUNT(*)                        AS orders
FROM `demo-shop.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= DATE_TRUNC(CURRENT_DATE('Asia/Tokyo'), YEAR)
GROUP BY month
ORDER BY month
```

# 注意

- `status = 'completed'` は[完了した注文](/glossary/completed-order.md)の
  定義そのもので、このクエリを「全注文」に簡略化できない理由である。
- `created_at` はテーブル上は UTC だが、切り捨ては `Asia/Tokyo` で行って
  いる。1日 08:30 JST の注文が正しい月に落ちるのはそのためである。列が
  UTC である理由は [shop.orders](/tables/shop-orders.md) にある。
- 返金は引かない。[売上計上ポリシー](/policies/revenue-recognition.md)の
  とおりである。
- 出力は月ごとに1行で、注文のなかった月は行ごと空く — カレンダーとの
  結合はしていない。今まで必要になっていない。

# 実行のしかた

`executor.resource` が指すのは
[BigQuery の計算を実行する](/skills/run-bigquery-query.md)で、実行は上に
宣言した `receipt` の三つ — `job_id`・`executed_sql`・`row_count` — を
持って帰ってこなければならない。`attester` はその実行が有効かどうかを
決めるコードで、`executed_sql` を上のフェンスと突き合わせ、書き換えられて
いれば拒否する。この数字が「承認された」だけでなく「証明された」ものに
なるのはそこである。どちらも ochakai の仕事ではない。契約を保存するだけで、
実行はしない。

結果を良し悪しで語る前に、[売上の読み方](/insights/reading-revenue.md)を
読むこと。`stale_after` を年度末にしてあるのは、そこで `shop.orders` の
パーティションが変わるからである。1月には、信じる前に見直すこと。
