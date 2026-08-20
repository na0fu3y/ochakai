---
type: Reference
resource: bigquery://bigquery-public-data.thelook_ecommerce
title: thelook_ecommerce データセット
description: このバンドルの数字がすべて出てくる公開データセット。誰でも読めるが、毎日更新され、過去の集計も動く
tags: [bigquery, dataset, thelook]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:10:00Z }
status: stable
---

このバンドルが扱うのは、Google が公開している合成 EC データセット
`bigquery-public-data.thelook_ecommerce` である。架空の衣料品店
"The Look" の注文・商品・顧客・行動ログを Looker のチームが生成した
もので、実在のデータセットなので、このバンドルのクエリはどれも実際に
走る。読むのに許可は要らない。ただしクエリの課金は実行する側の
プロジェクトに付く。ロケーションは `US` である。

テーブルは 7 つある。カタログ化してあるのは、判断の載っている 4 つで
ある。

| テーブル | ここでの concept |
|---|---|
| `orders` | [注文](/tables/orders.md)。ヘッダで、金額の列は無い |
| `order_items` | [注文明細](/tables/order-items.md)。金額はすべてここ |
| `products` | [商品](/tables/products.md)。定価と原価 |
| `users` | [顧客](/tables/users.md)。獲得チャネルはここ |
| `inventory_items` | 在庫の個体。まだ判断が無いのでカタログ外 |
| `distribution_centers` | 出荷元 10 拠点。同上 |
| `events` | 行動ログ。`traffic_source` の注意点は[顧客](/tables/users.md)にある |

## 数字を concept に書かない理由

このデータセットは毎日更新される。行が増えるだけではない。`status` は
現在の状態で上書きされるので([完了した注文](/glossary/completed-order.md))、
過去の月の集計まで動く。合成データの再生成で古い行ごと変わることもあり、
同じクエリを二度走らせて同じ数字が返る保証はない。

そのため、このバンドルは定義と判断だけを持ち、数字を書かない。数字は
[月次売上](/queries/sales/monthly-revenue.md)のような承認された計算が、
実行のたびに receipt 付きで出す。「先週見た数字と合わない」は、この
データセットでは正常な状態である。先週の数字が必要なら、先週の receipt
を残しておく。

## 未来の日付の行がある

生成器は今日より先の日付の行を書く。`created_at` の最大値は数日先を
指しているのが普通で、当月の集計にはその分が積まれる。当月を読んでは
いけない理由の一つがこれで、もう一つは生成器が直近数日に山を作ること
である。どちらも[売上の読み方](/insights/reading-revenue.md)にある。

このバンドルを Palantir 風の語彙で読みたい場合は、
[オントロジー](/glossary/ontology.md)から入るとよい。
