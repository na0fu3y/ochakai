---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.orders
title: orders
description: 注文1件につき1行のヘッダ。金額の列は無い — 金は order_items にある
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T03:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:20:00Z }
status: stable
---

注文のヘッダ。1行は注文1件である。このテーブルについて最初に知るべき
ことは、**金額の列が無い**ことである。`orders` だけをどう集計しても
売上は出ない — 金はすべて[注文明細](/tables/order-items.md)の
`sale_price` にあり、素直に書いた SQL が最初に踏む罠がこれである。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `order_id` | INT64 | 主キー。[明細](/tables/order-items.md)が `order_id` で指す |
| `user_id` | INT64 | **常に入っている**。ゲスト購入という状態がこの世界には無い |
| `status` | STRING | 5値、**大文字始まり**。[完了した注文](/glossary/completed-order.md)を見ること |
| `num_of_item` | INT64 | 明細の行数。明細側に数量列が無いのでこれが個数である |
| `gender` | STRING | [顧客](/tables/users.md)側にもある値の複製。JOIN を省くために置かれている |
| `created_at` | TIMESTAMP | **UTC**。月次はこの列を UTC の暦月で切る([売上計上ポリシー](/policies/revenue-recognition.md)) |
| `returned_at` / `shipped_at` / `delivered_at` | TIMESTAMP | 起きていなければ null。時系列はここから読める |

## 既知の罠: status は歴史を持たない

`status` は現在の状態であり、上書きされる。配達済みだった注文が返品
されると `Returned` になり、**過去の月の売上がその瞬間に変わる**。
「いつ何の状態だったか」にこのテーブルは答えられない — 締めた数字を
固定したければ、締めの時点の receipt を残すこと
([thelook_ecommerce データセット](/references/thelook-dataset.md))。

集計はこのテーブルではなく明細側で行うのが、このバンドルの規則である。
理由は[完了した注文](/glossary/completed-order.md)にある。
