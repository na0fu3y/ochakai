---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.orders
title: orders
description: 注文1件につき1行のヘッダ。金額の列は無く、金額は order_items にある
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:20:00Z }
status: stable
---

注文のヘッダで、1行が注文1件である。このテーブルについて最初に知って
おくことは、**金額の列が無い**ことである。`orders` だけをどう集計しても
売上は出ない。金額はすべて[注文明細](/tables/order-items.md)の
`sale_price` にあり、素直に書いた SQL が最初につまずくのはここである。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `order_id` | INT64 | 主キー。[明細](/tables/order-items.md)が `order_id` で指す |
| `user_id` | INT64 | 常に入っている。ゲスト購入という状態はこのデータセットに無い |
| `status` | STRING | 5値で、いずれも大文字始まり。[完了した注文](/glossary/completed-order.md)を参照 |
| `num_of_item` | INT64 | 明細の行数。明細側に数量列が無いので、これが個数にあたる |
| `gender` | STRING | [顧客](/tables/users.md)側にもある値の複製。JOIN を省くために置かれている |
| `created_at` | TIMESTAMP | UTC。月次はこの列を UTC の暦月で切る([売上計上ポリシー](/policies/revenue-recognition.md)) |
| `returned_at` / `shipped_at` / `delivered_at` | TIMESTAMP | 起きていなければ null。時系列はここから読める |

## status は現在の状態で、履歴を持たない

`status` は上書きされる。配達済みだった注文が返品されると `Returned` に
なり、その時点で過去の月の売上が変わる。いつ何の状態だったかを、この
テーブルは答えられない。締めた数字を固定したい場合は、締めの時点の
receipt を残す
([thelook_ecommerce データセット](/references/thelook-dataset.md))。

集計は、このテーブルではなく明細側で行う。理由は
[完了した注文](/glossary/completed-order.md)にある。
