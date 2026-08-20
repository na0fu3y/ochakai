---
type: Reference
resource: bigquery://bigquery-public-data.thelook_ecommerce
title: thelook_ecommerce データセット
description: このバンドルのすべての数字が出てくる公開データセット — 誰でも読めて、毎日動き、過去も不変ではない
tags: [bigquery, dataset, thelook]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:10:00Z }
status: stable
---

このバンドルが扱う世界は、Google が公開している合成 EC データセット
`bigquery-public-data.thelook_ecommerce` である。架空の衣料品店
"The Look" の注文・商品・顧客・行動ログを Looker のチームが生成して
おり、**実在のデータセットなので、このバンドルのクエリはどれも本当に
走る**。読むのに許可は要らないが、クエリの課金は実行する側のプロジェクト
に付く。ロケーションは `US`。

テーブルは 7 つあり、カタログ化してあるのは判断の載っている 4 つである:

| テーブル | ここでの concept |
|---|---|
| `orders` | [注文](/tables/orders.md) — ヘッダ。**金額の列は無い** |
| `order_items` | [注文明細](/tables/order-items.md) — 金はすべてここ |
| `products` | [商品](/tables/products.md) — カタログ。定価と原価 |
| `users` | [顧客](/tables/users.md) — 獲得チャネルはここ |
| `inventory_items` | 在庫の個体。まだ判断が無いのでカタログ外 |
| `distribution_centers` | 出荷元 10 拠点。同上 |
| `events` | 行動ログ。`traffic_source` の罠が[顧客](/tables/users.md)にある |

## 数字がここに書かれていない理由

このデータセットは毎日更新される。しかも `status` は現在の状態で
上書きされるので([完了した注文](/glossary/completed-order.md))、
新しい行が増えるだけでなく、**過去の月の集計も動く**。合成データの
再生成で歴史そのものが書き換わることもあり、同じクエリを二度走らせて
同じ数字が返る保証は、このデータセットのどこにも無い。

だからこのバンドルは方針として、数字を concept に書かない。書くのは
定義と判断だけで、数は[月次売上](/queries/sales/monthly-revenue.md)の
ような承認された計算が、実行のたびに receipt 付きで出す。「先週見た
数字と合わない」は、このデータセットではバグ報告ではなく仕様である —
先週の数字を握っていたければ、先週の receipt を残しておくことである。

このバンドルを Palantir 風の語彙で読みたい人は、
[オントロジー](/glossary/ontology.md)から入るとよい。
