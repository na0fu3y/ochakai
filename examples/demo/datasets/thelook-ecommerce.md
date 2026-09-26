---
type: BigQuery Dataset
resource: bigquery://bigquery-public-data.thelook_ecommerce
title: thelook_ecommerce
description: このバンドルの数字がすべて出てくる公開データセット。誰でも読めるが、履歴ごと毎日作り直されるので、同じクエリが昨日と同じ数字を返さない
tags: [bigquery, dataset, thelook]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:10:00Z }
status: stable
---

Google が公開している合成 EC データセットで、架空の衣料品店 "The Look" の
注文・商品・顧客・在庫・行動ログを Looker のチームが生成したものである。
読むのに許可は要らず、クエリの課金は実行した側のプロジェクトに付く。
ロケーションは `US`。

# Tables

| テーブル | 1 行は | 読むときに最初に知ること |
|---|---|---|
| [order_items](/tables/order-items.md) | 注文の中の商品 1 点 | 金額はすべてここにある |
| [orders](/tables/orders.md) | 注文 1 件 | 金額の列が無い |
| [products](/tables/products.md) | 商品(SKU)1 つ | 価格は商品ごとに固定 |
| [users](/tables/users.md) | 会員 1 人 | 2 割は一度も買っていない |
| [inventory_items](/tables/inventory-items.md) | 在庫の現物 1 点 | 明細からの外部キーが売れた個体を指さない |
| [distribution_centers](/tables/distribution-centers.md) | 物流拠点 1 か所 | 10 行だけ |
| [events](/tables/events.md) | ページ閲覧 1 回 | 一番大きい。セッションはここから作る |

# 履歴ごと、毎日作り直される

このデータセットは毎日再生成され、そのたびに**過去の行も作り直される**。
行が足されるだけではない。2026 年 3 月の明細の数は、同じ月について
6 日前・3 日前・前日・当日で四つとも違う値だった。先週見た月次の数字と
今日の数字が合わないのは、この性質による。返品や状態の変化で過去が動く
のではない([完了した注文](/glossary/completed-order.md))。

そのため、このバンドルは数字を concept に書かない。数字は
[月次売上](/computations/monthly-revenue.md)のような承認された計算が、
実行のたびに receipt 付きで出す。receipt の実行時刻があれば、7 日以内
なら BigQuery のタイムトラベルで同じスナップショットを読み直せる
([BigQuery の計算を実行する](/skills/run-bigquery-query.md))。それを
過ぎると、その数字は receipt の中にしか残らない。

# 未来の日付の行がある

`created_at` が今日より数日先の行が、毎回千行以上ある。直近の数日には
生成器が山を作ってもいる。当月を読んではいけない理由はこの二つで、
[売上の読み方](/insights/reading-revenue.md)にある。

# Common query patterns

データの鮮度を確かめる。`max_created_at` が今日より先を指していれば、
未来の日付の行が入っている。

```sql
SELECT
  COUNT(*)        AS order_items,
  MIN(created_at) AS min_created_at,
  MAX(created_at) AS max_created_at
FROM `bigquery-public-data.thelook_ecommerce.order_items`
```

このデータで答えられない問いの一覧は[不足しているデータ](/insights/missing-data.md)
にある。
