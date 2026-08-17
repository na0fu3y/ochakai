---
type: Attested Computation
title: チャネル別売上
description: 注文が入ってきたチャネルで月次売上を割ったもの(draft — チャネルコードが移行中)
tags: [sales, revenue, marketing, bigquery]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-06-12T09:45:00Z }
status: draft
stale_after: "2026-06-30"
runtime: bigquery
parameters:
  - { name: from_date, type: DATE, required: true }
  - { name: to_date, type: DATE, required: true }
executor:
  resource: /skills/run-bigquery-query.md
  receipt: [job_id, executed_sql, row_count]
question: 売上が伸びているのはどのチャネルか?
---

[月次売上](/queries/sales/monthly-revenue.md)と同じ[売上](/metrics/revenue.md)
を、`orders.channel_code` で割ったもの。キャンペーンの振り返りのときに
エージェントが書き、draft のまま置いてある。割り方はコードの質より良くは
ならないからである。

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  o.channel_code,
  SUM(o.total_price)              AS revenue
FROM `demo-shop.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= @from_date
  AND o.created_at <  @to_date
GROUP BY month, o.channel_code
ORDER BY month, revenue DESC
```

# 検証されていない理由

- 注文のおよそ 4%、主に古いものは `channel_code` が null で、その行は黙って
  自分たちだけのグループを作る。合計は
  [月次売上](/queries/sales/monthly-revenue.md)と一致するので、25 分の1が
  未帰属であることは、割った結果からはわからない。
- コードそのものが差し替え中である。この出力を読むための一覧
  [注文チャネルコード](/references/order-channel-codes.md)は deprecated で、
  店舗側は一部の注文に新しい分類を書き始めている。backfill が終わるまで、
  `web` と `web_direct` は名前が二つある同じチャネルである。
- `stale_after` を移行期間の終わりに置いたので、それはもう過ぎている。
  つまりこの concept は stale のフィードに出てくる。それが狙いである —
  誰も戻ってきていないことが、フィードでわかる。
- `attester` がない。`executor` はあるので
  [実行はできる](/skills/run-bigquery-query.md)が、その実行が*この* SQL を
  使ったことを確かめるものが何もない。ここから引いた数字は、信用で受け取る
  数字である。[月次売上](/queries/sales/monthly-revenue.md)には attester が
  ある。二つの concept の違いで、いちばん効くのはそこである。
